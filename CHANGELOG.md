# Changelog

## v0.2.0

- Replace exact-kernel executable restrictions with capability-based layout and function profiles.
- Add runtime BTF and EL8 header-offset layout routes; keep executable builds independent of target patch release.
- Share an iftop-style frontend between native nfstop and bpftrace nfsdtop.
- Add rolling 1-minute and configurable-window averages, sample-window peaks and automatic units.
- Restore the original terminal after q/Ctrl+C; isolate bpftrace process cleanup.
- Simplify transport/interface/output flags; default to NFS/RDMA client view.
- Retain UID/GID/server/file grouping and raw operation counters/rates in JSON.
- Package open-source notices, documentation and build/release workflows.
- Compile Linux amd64 binaries; no tests or probe/runtime validation in this revision.
