# Publish on GitHub

The source archive contains a repository root, not a patch that must be applied
to upstream. It intentionally has no credentials, upstream `.git` directories,
local kernel headers, profiling logs or host-specific layout JSON.

## First push

Create an empty GitHub repository (for example `nfsmon`). From the unpacked
source root, use your actual account/repository in place of `YOUR_ACCOUNT`:

```bash
git init -b main
git add .
git commit -m "Introduce portable NFS server I/O monitors"
git remote add origin git@github.com:YOUR_ACCOUNT/nfsmon.git
git push -u origin main
```

The module is `nfsmon`, so the repository builds before the owner/name is known.
For a public Go module namespace, replace `module nfsmon`, Go imports beginning
with `nfsmon/`, and the two linker `-X nfsmon/internal/app...` paths in
`tools/build.sh` with `github.com/YOUR_ACCOUNT/nfsmon`. This namespace change is
optional for users who download the source and run `make build`.

Do not change upstream ownership notices when adopting a repository name.

## Binary release

Update the version/change log as desired, then create a semantic-version tag:

```bash
git tag -a v0.2.0 -m "NFSmon v0.2.0"
git push origin v0.2.0
```

`.github/workflows/release.yml` builds both executables on a GitHub runner and
publishes the tag's release using GitHub's own `gh` CLI and `GITHUB_TOKEN`.
Allow repository Actions and its release job's `contents: write` permission.
No personal access token or username is embedded.

Release assets:

- `nfsmon-v0.2.0-linux-amd64.tar.gz`
- `nfsmon-v0.2.0-linux-amd64.zip`
- `SHA256SUMS`
- A source snapshot, also automatically offered by GitHub for the tag.

Both executables use `CGO_ENABLED=0`. `nfstop` embeds the BPF object; runtime
kernel adaptation occurs via BTF or generated layout JSON. `nfsdtop` invokes
the user's installed bpftrace and is not a bundled bpftrace runtime. Release
archives include runtime tools and notices. Generated BPF bindings/object are
also included in the source, so users can build frontend binaries without
regenerating BPF; `make build` does regenerate it from source.

`workflow_dispatch` builds downloadable artifacts without publishing a release.
Push/PR build workflows and release workflows compile/package only; they do
not run tests, load BPF, alter NFS services or benchmark workloads.

The prepared artifacts were compiled locally but **not uploaded to GitHub**.
This document describes the later release flow; no public repository is assumed.

References: [GitHub Go build workflows](https://docs.github.com/en/actions/tutorials/build-and-test-code/go),
[gh release create](https://cli.github.com/manual/gh_release_create).
