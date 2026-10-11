# Publish on GitHub

The project is published at [guixiaocai/norma](https://github.com/guixiaocai/norma).
Public commands and release packages use `norma`. The internal Go module name
remains `nfsmon`; that name does not affect the executable or repository name.
Upstream ownership notices must remain intact.

## Build a release

Ordinary builds use the checked-in BPF object and require Go >= 1.25.3 and make.
Release packaging also needs tar and zip:

```bash
make build
./bin/norma --version
make release VERSION=v0.3.0
```

There is one static Go executable, **`bin/norma`**, containing both collectors.
Its default native backend requires no installed bpftrace/compiler. The
`--backend bpftrace` path invokes the target host's installed bpftrace >= 0.16
and does not bundle that runtime.

If BPF source was changed, first install clang/LLVM, libbpf development headers
and gcc, run `make generate`, and retain the generated object/bindings alongside
the source. This step belongs on the build machine.

## Publish a version tag

Update `CHANGELOG.md`, `docs/release-notes.md` and the default version, then
commit the completed changes and push the branch. Create a new version tag;
existing published tags must continue to identify their original source.

```bash
git tag -a v0.3.0 -m "norma v0.3.0"
git push origin v0.3.0
```

`.github/workflows/release.yml` regenerates BPF, runs Go checks and builds the
release on a GitHub runner. It publishes the tag's release using GitHub's `gh`
CLI and `GITHUB_TOKEN`. Repository Actions and the release job's
`contents: write` permission must be enabled. No personal access token or
username is embedded in the project.

Release assets:

- `norma-v0.3.0-linux-amd64.tar.gz`
- `norma-v0.3.0-linux-amd64.zip`
- `SHA256SUMS`
- GitHub's source snapshots for the tag.

Archives contain `bin/norma`, runtime tools, documentation and licenses.
`workflow_dispatch` prepares downloadable artifacts without publishing a
release. Build/check jobs do not attach BPF probes, alter NFS services or run an
NFS workload; deployment validation must happen on the intended server.

## Repository discovery

Suggested repository description:

> iftop-style Linux NFS server I/O monitor: per-client RDMA/TCP/UDP throughput, native eBPF or bpftrace, rolling averages and JSON output.

Suggested GitHub topics:

```text
nfs nfs-server rdma infiniband ebpf bpftrace linux monitoring observability
performance terminal tui golang hpc storage
```

Keep the English README as the landing page and link the Chinese translation
near the top. Set description/topics in the repository's About panel.

References: [GitHub Go workflows](https://docs.github.com/en/actions/tutorials/build-and-test-code/go),
[gh release create](https://cli.github.com/manual/gh_release_create),
[GitHub repository topics](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/classifying-your-repository-with-topics).
