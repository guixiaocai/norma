/* SPDX-License-Identifier: (MIT OR GPL-2.0)
 * Derived from MIT ORCD nfstop's aggregation idea and the 2026 VFS adaptation.
 * No embedded kernel structure offsets, module BTF, fentry, or ring-buffer maps.
 */
#include <linux/types.h>
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>

/* Stable x86_64 Linux kprobe register ABI. Other architectures need an adapter. */
struct pt_regs {
    __u64 r15,r14,r13,r12,bp,bx,r11,r10,r9,r8,ax,cx,dx,si,di;
    __u64 orig_ax,ip,cs,flags,sp,ss;
};
struct key_t {
    __u64 ino;
    __u32 uid,gid,dev,family;
    __u8 client[16],server[16];
};
struct val_t { __u64 r_bytes,w_bytes,r_ops,w_ops; };
struct name_t { __u8 parent[64],name[64]; };
struct name_key { __u64 ino; __u32 dev,pad; };
struct hook_t { __u32 rq_arg,file_arg,count_arg,count_bytes,file_wrapper; };
struct config_t {
    __u32 transport,names;
    __u32 rq_addr,rq_daddr,rq_vers,rq_prot,rq_uid,rq_gid;
    __u32 file_inode,file_dentry,inode_ino,inode_sb,sb_dev,dentry_parent,dentry_name;
    struct hook_t hooks[3];
};
struct health_t { __u64 bad_reads,file_full,client_full,inflight_full,overwritten,failed_ops,name_drops; };
struct flight_key { __u64 tid; __u32 slot,pad; };
struct flight_t { struct key_t key; __u64 count,file; __u32 write,count_bytes; };

struct {
    __uint(type,BPF_MAP_TYPE_PERCPU_HASH);
    __uint(max_entries,16384);
    __uint(map_flags,BPF_F_NO_PREALLOC);
    __type(key,struct key_t); __type(value,struct val_t);
} counts SEC(".maps");
struct {
    __uint(type,BPF_MAP_TYPE_PERCPU_HASH);
    __uint(max_entries,4096);
    __uint(map_flags,BPF_F_NO_PREALLOC);
    __type(key,struct key_t); __type(value,struct val_t);
} clients SEC(".maps");
struct {
    __uint(type,BPF_MAP_TYPE_HASH); __uint(max_entries,8192);
    __type(key,struct flight_key); __type(value,struct flight_t);
} inflight SEC(".maps");
struct {
    __uint(type,BPF_MAP_TYPE_ARRAY); __uint(max_entries,1);
    __type(key,__u32); __type(value,struct config_t);
} config SEC(".maps");
struct {
    __uint(type,BPF_MAP_TYPE_PERCPU_ARRAY); __uint(max_entries,1);
    __type(key,__u32); __type(value,struct health_t);
} health SEC(".maps");
struct {
    __uint(type,BPF_MAP_TYPE_HASH); __uint(max_entries,16384);
    __type(key,struct name_key); __type(value,struct name_t);
} names SEC(".maps");

static __always_inline struct health_t *healthp(void) {
    __u32 z=0;return bpf_map_lookup_elem(&health,&z);
}
static __always_inline __u64 argument(struct pt_regs *ctx,__u32 n) {
    switch(n) {
    case 1:return ctx->di;case 2:return ctx->si;case 3:return ctx->dx;
    case 4:return ctx->cx;case 5:return ctx->r8;case 6:return ctx->r9;
    case 7:case 8: {
        __u64 x=0;bpf_probe_read(&x,8,(void *)(ctx->sp+(n-6)*8));return x;
    }
    }return 0;
}
#define GET(out,ptr,off) bpf_probe_read(&(out),sizeof(out),(void *)((__u64)(ptr)+(__u32)(off)))
static __always_inline void hint(__u64 file,struct key_t *key,struct config_t *c,struct health_t *h) {
    if (!c->names) return;
    struct name_key nk={.ino=key->ino,.dev=key->dev};
    if (bpf_map_lookup_elem(&names,&nk)) return;
    struct name_t name={};
    __u64 dentry=0,parent=0,p=0;
    if (GET(dentry,file,c->file_dentry) || !dentry) return;
    if (!GET(p,dentry,c->dentry_name) && p) bpf_probe_read_str(name.name,64,(void *)p);
    if (!GET(parent,dentry,c->dentry_parent) && parent && !GET(p,parent,c->dentry_name) && p)
        bpf_probe_read_str(name.parent,64,(void *)p);
    long err=bpf_map_update_elem(&names,&nk,&name,BPF_NOEXIST);
    if (err && err!=-17 && h) h->name_drops++; /* EEXIST is an ordinary first-use race. */
}
static __always_inline int start(struct pt_regs *ctx,__u32 slot,__u32 write) {
    __u32 z=0;
    struct config_t *c=bpf_map_lookup_elem(&config,&z);
    struct health_t *h=healthp();
    if (!c) return 0;
    struct hook_t *hk=&c->hooks[slot];
    __u64 rq=argument(ctx,hk->rq_arg),file=argument(ctx,hk->file_arg),cnt=argument(ctx,hk->count_arg);
    __u32 proto=0,vers=0; __u16 family=0,server_family=0;
    __u64 inode=0,sb=0;
    struct flight_t op={};
    struct flight_key fk={.tid=bpf_get_current_pid_tgid(),.slot=slot};
    if (!rq || !file || !cnt) return 0;
    if (GET(proto,rq,c->rq_prot) || GET(vers,rq,c->rq_vers)) goto bad;
    if ((c->transport && proto!=c->transport) || (vers!=3 && vers!=4)) return 0;
    if (GET(family,rq,c->rq_addr) || GET(server_family,rq,c->rq_daddr)) goto bad;
    op.key.family=family;
    if (family==2) {
        if (bpf_probe_read(op.key.client,4,(void *)(rq+c->rq_addr+4))) goto bad;
        if (server_family!=2 || bpf_probe_read(op.key.server,4,(void *)(rq+c->rq_daddr+4))) goto bad;
    } else if (family==10) {
        if (bpf_probe_read(op.key.client,16,(void *)(rq+c->rq_addr+8))) goto bad;
        if (server_family!=10 || bpf_probe_read(op.key.server,16,(void *)(rq+c->rq_daddr+8))) goto bad;
    } else return 0;
    if (hk->file_wrapper!=0xffffffff && GET(file,file,hk->file_wrapper)) goto bad;
    if (GET(op.key.uid,rq,c->rq_uid) || GET(op.key.gid,rq,c->rq_gid) ||
        GET(inode,file,c->file_inode) || !inode || GET(op.key.ino,inode,c->inode_ino) ||
        GET(sb,inode,c->inode_sb) || !sb || GET(op.key.dev,sb,c->sb_dev)) goto bad;
    op.count=cnt;op.file=file;op.write=write;op.count_bytes=hk->count_bytes;
    if (bpf_map_lookup_elem(&inflight,&fk) && h) h->overwritten++;
    if (bpf_map_update_elem(&inflight,&fk,&op,BPF_ANY) && h) h->inflight_full++;
    hint(file,&op.key,c,h);
    return 0;
bad: if (h) h->bad_reads++;return 0;
}
static __always_inline void add(void *map,struct key_t *key,__u64 bytes,__u32 write,struct health_t *h,__u32 client) {
    struct val_t init={},*v=bpf_map_lookup_elem(map,key);
    if (!v) {bpf_map_update_elem(map,key,&init,BPF_NOEXIST);v=bpf_map_lookup_elem(map,key);}
    if (!v) {if (h) {if (client) h->client_full++;else h->file_full++;}return;}
    if (write) {v->w_bytes+=bytes;v->w_ops++;}else {v->r_bytes+=bytes;v->r_ops++;}
}
static __always_inline int finish(struct pt_regs *ctx,__u32 slot) {
    struct flight_key fk={.tid=bpf_get_current_pid_tgid(),.slot=slot};
    struct flight_t *p=bpf_map_lookup_elem(&inflight,&fk);
    struct health_t *h=healthp();
    if (!p) return 0;
    struct flight_t op=*p;
    bpf_map_delete_elem(&inflight,&fk);
    if ((__u32)ctx->ax!=0) {if (h) h->failed_ops++;return 0;}
    __u64 bytes=0;
    if (op.count_bytes==4) {
        __u32 n=0;if (bpf_probe_read(&n,4,(void *)op.count)) goto bad;bytes=n;
    } else if (bpf_probe_read(&bytes,8,(void *)op.count)) goto bad;
    add(&counts,&op.key,bytes,op.write,h,0);
    op.key.ino=0;op.key.uid=0;op.key.gid=0;op.key.dev=0;
    add(&clients,&op.key,bytes,op.write,h,1);
    return 0;
bad: if (h) h->bad_reads++;return 0;
}
SEC("kprobe") int read_start(struct pt_regs *ctx) {return start(ctx,0,0);}
SEC("kretprobe") int read_end(struct pt_regs *ctx) {return finish(ctx,0);}
SEC("kprobe") int splice_start(struct pt_regs *ctx) {return start(ctx,1,0);}
SEC("kretprobe") int splice_end(struct pt_regs *ctx) {return finish(ctx,1);}
SEC("kprobe") int write_start(struct pt_regs *ctx) {return start(ctx,2,1);}
SEC("kretprobe") int write_end(struct pt_regs *ctx) {return finish(ctx,2);}
char LICENSE[] SEC("license")="Dual MIT/GPL";
