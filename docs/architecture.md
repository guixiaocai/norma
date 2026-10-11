# Architecture

Both entrypoints use `internal/app`: option parsing, address filtering,
aggregation, duration-weighted history, unit conversion and terminal lifecycle.
`internal/collect` defines cumulative snapshots shared by the two backends.

| Directory | Responsibility |
|---|---|
| `cmd/nfstop` | Native collector executable |
| `cmd/nfsdtop` | bpftrace collector executable |
| `internal/native` | Embedded eBPF object, maps and kprobe attachments |
| `internal/trace` | Generate numeric-offset bpftrace probes, supervise child process, parse cumulative frames |
| `internal/layout` | Runtime BTF signatures, offsets, header-object extraction and layout identity |
| `internal/app` | Shared UI, rolling windows and output |
| `tools` | Build, install, layout generation and release packaging |

The previous separate Go/Bash/awk displays were replaced with one Go frontend,
which avoids subtly different window definitions and terminal cleanup between
the tools. nfsdtop's acquisition still uses bpftrace; the frontend does not
switch it to the native eBPF collector.

The native object contains register ABI definitions and configurable numeric
offsets rather than the previous fixed `kernel-types.h`. It does not need
kernel CO-RE relocation. Runtime BTF is parsed in Go for layouts/signatures when
available. On EL8 the header helper supplies the offsets with the known EL8
file-pointer function profile. Modern BTF-recognized wrappers use configurable
file-pointer indirection.

Inflight records include the probe slot and task ID, so distinct read/write
helpers do not overwrite each other's records. Native return hooks attach
before their entry hooks. Per-CPU counts avoid data-counter clearing races.
Client maps are separate from file maps. Optional parent/basename hints are
stored in a regular hash map, removing the earlier ring-buffer requirement.
The native file identity is device+inode; hints are truncated, may become stale
after rename and are not absolute paths. The bpftrace file view uses the same
device+inode identity without a pathname hint.

bpftrace prints framed cumulative maps. The frontend consumes snapshots and
supervises its private process group, retains a bounded diagnostic tail, and
signals only the owned child group on shutdown. Slow display consumption may
coalesce snapshots; cumulative byte totals remain intact, but coalescing lowers
peak time resolution. Both userspace map iteration and bpftrace map printing
are live snapshots, not atomic snapshots of every CPU.

Interactive rendering uses Bubble Tea's alternate screen and terminal mode
restoration. No `clear`, erase-scrollback (`ESC[3J`), `stty sane`, shell-history
rewrite or shell-directed signal is used. Errors return through cleanup before
printing to stderr. `-t`, redirected stdin/stdout and JSON use no ANSI controls.

No tests were added or run for this revision. Runtime backend differences and
return-probe losses must not be described as empirically eliminated by the
shared UI or successful compilation.
