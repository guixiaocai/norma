# Contributing

Describe the affected NFSD function semantics and layout requirements, alongside
the running kernel. Keep both backends' metric definitions and CLI behavior
consistent. Backend selection belongs in `cmd/norma`, kernel adaptation in
`internal/layout`, collection in `internal/native` or `internal/trace`, and the
shared display/output in `internal/app`.

Build with `make build` and run the Go checks with `go test ./...`. Ordinary
builds use the checked-in BPF object. After changing `internal/native/bpf`, install
the generation dependencies described in the README and run `make generate`
before building; commit the generated object/bindings with the BPF source.

When reporting support for an additional kernel, distinguish compilation and
userspace checks from actual probe attachment and NFS workload observations.
Include the selected backend, layout route and collection-health diagnostics.
A change to function completion semantics requires collector review, not merely
new offset data. Compare backend overhead only with reproducible measurements.

Retain SPDX tags and upstream notices. Submit source changes rather than host
profiles, private files, IP listings or machine-specific layouts. Dependencies
are version-locked in go.mod/go.sum. Never embed secrets in workflows or releases.
