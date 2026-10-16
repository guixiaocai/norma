/* Test-only userspace stand-ins; the real object uses system libbpf headers. */
#define SEC(name)
#define __uint(name, value) int (*name)[value]
#define __type(name, value) value *name
#ifndef __always_inline
#define __always_inline inline __attribute__((always_inline))
#endif
static void *bpf_map_lookup_elem(void *, const void *);
static long bpf_map_update_elem(void *, const void *, const void *, unsigned long);
static long bpf_map_delete_elem(void *, const void *);
static unsigned long long bpf_ktime_get_ns(void);
static unsigned long long bpf_get_current_pid_tgid(void);
static long bpf_probe_read(void *, unsigned int, const void *);
static long bpf_probe_read_str(void *, unsigned int, const void *);
