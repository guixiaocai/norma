/* SPDX-License-Identifier: MIT
 * Execute the actual entry/return C paths with mock BPF helpers. This validates
 * accounting control flow; it does not replace a kernel verifier/attach test. */
#include <assert.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>
#include "../bpf/collector.c"

static struct config_t cfg;
static struct health_t health_value;
static struct flight_t pending;
static struct val_t detail_value, client_value;
static struct diagnostic_t diagnostic_value, zero_value;
static struct diagnostic_key seen_diagnostic_key;
static struct key_t seen_client_key;
static __u64 peak_value, clock_ns;
static int pending_exists, detail_exists, client_exists, diagnostic_exists, peak_exists;
static int reject_diagnostic, reject_peak;
static void *bpf_map_lookup_elem(void *map, const void *key) {
    (void)key;
    if (map==&config) return &cfg;
    if (map==&health) return &health_value;
    if (map==&inflight) return pending_exists ? &pending : NULL;
    if (map==&counts) return detail_exists ? &detail_value : NULL;
    if (map==&clients) return client_exists ? &client_value : NULL;
    if (map==&diagnostics) return diagnostic_exists ? &diagnostic_value : NULL;
    if (map==&diagnostic_max) return peak_exists ? &peak_value : NULL;
    if (map==&diagnostic_zero) return &zero_value;
    return NULL;
}
static long bpf_map_update_elem(void *map, const void *key, const void *value, unsigned long flags) {
    (void)flags;
    if (map==&inflight) { memcpy(&pending,value,sizeof(pending));pending_exists=1; }
    if (map==&counts) { memcpy(&detail_value,value,sizeof(detail_value));detail_exists=1; }
    if (map==&clients) { memcpy(&client_value,value,sizeof(client_value));memcpy(&seen_client_key,key,sizeof(seen_client_key));client_exists=1; }
    if (map==&diagnostics) {
        if (reject_diagnostic) return -1;
        memcpy(&diagnostic_value,value,sizeof(diagnostic_value));memcpy(&seen_diagnostic_key,key,sizeof(seen_diagnostic_key));diagnostic_exists=1;
    }
    if (map==&diagnostic_max) { if (reject_peak) return -1;memcpy(&peak_value,value,sizeof(peak_value));peak_exists=1; }
    return 0;
}
static long bpf_map_delete_elem(void *map, const void *key) { (void)key;if(map==&inflight) pending_exists=0;return 0; }
static unsigned long long bpf_ktime_get_ns(void) { return clock_ns+=1000; }
static unsigned long long bpf_get_current_pid_tgid(void) { return 99; }
static long bpf_probe_read(void *out, unsigned int size, const void *source) {
    if ((uintptr_t)source<4096) return -1;
    memcpy(out,source,size);return 0;
}
static long bpf_probe_read_str(void *out, unsigned int size, const void *source) { (void)out;(void)size;(void)source;return -1; }

struct address { __u16 family,pad; __u32 ip; };
struct request { __u32 protocol,version,uid,gid; struct address client,server; };
struct super { __u32 device; };
struct inode { __u64 number; struct super *super; };
struct file { struct inode *inode; };
static struct request rq;
static struct super sb;
static struct inode ino;
static struct file file;
static __u32 completed;
static struct pt_regs regs;
static void reset(void) {
    memset(&cfg,0,sizeof(cfg));memset(&health_value,0,sizeof(health_value));memset(&pending,0,sizeof(pending));
    memset(&detail_value,0,sizeof(detail_value));memset(&client_value,0,sizeof(client_value));
    memset(&diagnostic_value,0,sizeof(diagnostic_value));memset(&zero_value,0,sizeof(zero_value));
    pending_exists=detail_exists=client_exists=diagnostic_exists=peak_exists=0;
    reject_diagnostic=reject_peak=0;peak_value=clock_ns=0;
    cfg.rq_prot=offsetof(struct request,protocol);cfg.rq_vers=offsetof(struct request,version);
    cfg.rq_addr=offsetof(struct request,client);cfg.rq_daddr=offsetof(struct request,server);
    cfg.rq_uid=offsetof(struct request,uid);cfg.rq_gid=offsetof(struct request,gid);
    cfg.file_inode=offsetof(struct file,inode);cfg.inode_ino=offsetof(struct inode,number);
    cfg.inode_sb=offsetof(struct inode,super);cfg.sb_dev=offsetof(struct super,device);
    cfg.hooks[0]=(struct hook_t){.rq_arg=1,.file_arg=2,.count_arg=3,.count_bytes=4,.file_wrapper=0xffffffff};
    rq=(struct request){.protocol=256,.version=4,.uid=1000,.gid=1001,.client={.family=2,.ip=0x010200c0},.server={.family=2,.ip=0x020200c0}};
    sb=(struct super){.device=259};ino=(struct inode){.number=1234,.super=&sb};file.inode=&ino;completed=4096;
    regs=(struct pt_regs){.di=(__u64)&rq,.si=(__u64)&file,.dx=(__u64)&completed};
}
int main(void) {
    reset();start(&regs,0,0);finish(&regs,0);
    assert(detail_value.r_bytes==4096 && detail_value.r_ops==1 && client_value.r_bytes==4096);
    assert(seen_client_key.transport==256 && seen_diagnostic_key.transport==256);
    assert(diagnostic_value.calls==1 && diagnostic_value.errors==0 && diagnostic_value.total_ns==1000 && diagnostic_value.buckets[0]==1 && peak_value==1000);
    assert(!pending_exists && health_value.bad_reads==0);

    reset();rq.protocol=999;start(&regs,0,0);clock_ns+=7001;finish(&regs,0);
    assert(seen_client_key.transport==999 && seen_diagnostic_key.transport==999);
    assert(diagnostic_value.total_ns==8001 && diagnostic_value.buckets[4]==1 && peak_value==8001);

    /* A failed return is accounted even when neither file nor count exists. */
    reset();regs.si=0;regs.dx=0;start(&regs,0,1);regs.ax=5;finish(&regs,0);
    assert(diagnostic_value.calls==1 && diagnostic_value.errors==1 && seen_diagnostic_key.write==1);
    assert(health_value.failed_ops==1 && !client_exists && !detail_exists && !pending_exists);

    /* A successful byte completion remains in endpoint totals when the file
     * identity is unavailable; latency is independent of both data maps. */
    reset();regs.si=0;start(&regs,0,0);finish(&regs,0);
    assert(client_value.r_bytes==4096 && client_value.r_ops==1 && !detail_exists);
    assert(health_value.bad_reads==1 && diagnostic_value.calls==1 && diagnostic_value.errors==0);

    /* Failed count-pointer reads never become fake successful zero-byte ops. */
    reset();regs.dx=1;start(&regs,0,0);finish(&regs,0);
    assert(!client_exists && !detail_exists && health_value.bad_reads==1);
    assert(diagnostic_value.calls==1 && diagnostic_value.errors==0);

    reset();reject_diagnostic=1;start(&regs,0,0);finish(&regs,0);
    assert(health_value.diagnostic_drops==1 && client_value.r_bytes==4096);
    reset();reject_peak=1;start(&regs,0,0);finish(&regs,0);
    assert(health_value.diagnostic_drops==1 && diagnostic_value.calls==1 && client_value.r_bytes==4096);
    return 0;
}
