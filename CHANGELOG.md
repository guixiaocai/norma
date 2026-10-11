# Changelog

## v0.3.0

- Unify the public CLI and build/install/release output as `norma`.
- Include both collectors in one binary: native eBPF by default, bpftrace through explicit `--backend bpftrace`, with no automatic fallback.
- Document shared metrics, backend differences, deployment requirements and selection in English and Chinese.
- Replace obsolete distribution/version-focused guidance with capability-based NFSD/layout requirements.
- Use checked-in BPF objects for ordinary Go builds; add explicit `make generate` for BPF source updates.
- Use `norma` installation/cache paths and retain upstream notices.

## v0.2.0

- Replace exact-kernel executable restrictions with capability-based layout and function profiles.
- Add runtime BTF and a constrained header-offset layout route; keep executable builds independent of the target patch release.
- Share an iftop-style frontend between the historical native `nfstop` and bpftrace `nfsdtop` executables.
- Add rolling 1-minute and configurable-window averages, sample-window peaks and automatic units.
- Restore the original terminal after q/Ctrl+C; isolate bpftrace process cleanup.
- Simplify transport/interface/output flags; default to NFS/RDMA client view.
- Retain UID/GID/server/file grouping and raw operation counters/rates in JSON.
- Package open-source notices, documentation and build/release workflows.
- Compile Linux amd64 binaries without a cross-kernel runtime-support claim.
