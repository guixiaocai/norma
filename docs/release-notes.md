NFSmon v0.2.0 provides nfstop and bpftrace-backed nfsdtop with a shared terminal UI.

- Default NFS/RDMA monitoring; TCP/UDP/all transport selection and interface-local IP filtering.
- Current rate, 1-minute/window averages, window peak and session totals.
- Automatic byte/bit units; alternate-screen restoration; plain text and JSON Lines.
- Runtime BTF layout/prototype discovery and reusable EL8 header-derived layout data.
- No executable lock to one kernel patch release.
- Linux amd64 static Go binaries, install/layout helpers and upstream notices.

nfsdtop requires bpftrace >=0.16 on the target host. Compatibility is capability
based; this revision has no runtime/cross-kernel test results. Assets are build
artifacts, not a claim of tested support for every Linux kernel.
