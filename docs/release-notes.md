norma v0.3.0 unifies both collectors in one executable.

- `make` and release/install packages now produce `bin/norma`; run `norma` for native eBPF or `norma --backend bpftrace` for the alternative collector.
- Shared flags, interface, rolling averages, sample peaks and JSON metrics remain available across both backends; no automatic fallback is performed.
- English and Chinese documentation explain common capabilities, backend tradeoffs, selection and deployment requirements.
- Remove obsolete distribution/version-focused guidance in favor of NFSD function, layout and probe-capability requirements.
- Ordinary Go builds use the checked-in BPF object; `make generate` regenerates it when BPF source changes.
- Linux amd64 static binary, installation/layout tools, checksums and upstream notices are included.

Native needs no runtime bpftrace/compiler. The alternative backend needs installed
bpftrace >= 0.16 with a working runtime. Both require compatible NFSD signatures,
accurate layout data and BPF/probe permissions. Native can show optional file-name
hints; bpftrace file grouping uses device/inode. Build/CLI checks do not establish
cross-kernel probe/workload support or a measured performance advantage.
