# Contributing

Describe the affected NFSD function semantics, not just a distribution version.
Keep both frontends' metrics and command-line behavior consistent. Kernel
adaptation belongs in `internal/layout`, transport-independent I/O in collectors,
and display changes in `internal/app`.

Build with `make build`. Existing workflows compile/package only. No test suite
or target runtime validation is claimed by this release candidate. When reporting
support for an additional kernel, distinguish source compatibility, compilation
and actual probe/workload observations; do not relabel one as another.

Retain SPDX tags and upstream notices. Submit source changes rather than host
profiles, user files, IP listings or machine-specific layouts. Dependencies are
version-locked in go.mod/go.sum. Never embed secrets in workflows or releases.
