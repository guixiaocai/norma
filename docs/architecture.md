# Architecture

`cmd/norma` selects the collector through `--backend`: `native` is the default,
`bpftrace` is explicit. Both use `internal/app` for option parsing, address
filtering, aggregation, duration-weighted history, units and terminal lifecycle.
`internal/collect` defines their shared cumulative snapshot interface. A backend
failure is returned to the user; there is no automatic switch to another backend.

| Directory | Responsibility |
|---|---|
| `cmd/norma` | Unified executable and backend selection |
| `internal/native` | Embedded eBPF object, maps and kprobe attachments |
| `internal/trace` | Generate numeric-offset bpftrace probes, supervise the child process and parse cumulative frames |
| `internal/layout` | Runtime BTF signatures, offsets, header-object extraction and layout identity |
| `internal/app` | Shared CLI, UI, rolling windows and output |
| `tools` | Build, install, layout generation and release packaging |

## Common collection contract

Both backends attach entry and return probes to three NFSD VFS helper paths:
ordinary read, splice read and write. They count completed bytes from an updated
count pointer only on successful NFS status, for NFSv3/v4. The resolved layout
supplies argument positions, count widths and field offsets. Both filter the
same transport IDs and support client/server addresses, UID/GID and device/inode
keys. Grouping, address filters, window calculations and output formats live in
the shared frontend, so backend choice does not change their intended meaning.

Runtime BTF is parsed in Go for layouts and supported function signatures. BPF
code uses configurable numeric offsets and register ABI definitions, without
kernel CO-RE relocation. Recognized `struct nfsd_file *` wrappers can supply a
configurable indirection to `struct file *`. Unknown return/count semantics need
an explicit collector change; a familiar symbol name is insufficient.

## Native backend

The generated BPF object is embedded in `norma`. Go loads it, sets the layout
configuration, attaches probes and reads the maps directly. No installed
bpftrace, Go compiler, clang or libbpf shared library is needed at runtime.
Return hooks attach before their entry hooks. Inflight records include the
probe slot and task ID, avoiding collisions between different helper paths.
Per-CPU cumulative counters are summed in userspace and are not cleared at each
sample.

Independent endpoint-pair counters serve client/server views; a full detailed
map does not by itself invalidate these totals. Maps are bounded:

| Map | Maximum keys |
|---|---:|
| Client/server endpoint pairs | 4,096 |
| Detailed client/server/UID/GID/device/inode keys | 16,384 |
| Inflight task/probe records | 8,192 |
| File name hints | 16,384 |

The file identity is device+inode. In file view, a regular hash map also supplies
optional parent/basename hints, without a ring-buffer dependency. Hints are
truncated, may become stale after rename and are not absolute paths. Health
counters report memory-read failures, map/inflight capacity problems, overwritten
records, failed operations and dropped hints. These diagnostics do not prove
that every return probe reached the collector.

## bpftrace backend

`norma --backend bpftrace` requires installed bpftrace >= 0.16 and a working
bpftrace runtime on the target host. Go generates a temporary script from the
same resolved layout, launches bpftrace in a private process group and parses
its framed cumulative text maps. The script uses numeric offsets and no kernel
header includes; any header-based layout preparation is a separate step.

bpftrace performs its own compilation and probe attachment at startup. Its
version, packaged dependencies and support for the required features are extra
deployment requirements. The frontend retains a bounded diagnostic tail and
reports bpftrace startup/runtime errors, failed operations and overwritten
inflight records. File grouping uses device+inode without a pathname hint.

The default `BPFTRACE_MAP_KEYS_MAX` is 16,384 unless the user overrides it. Client
and server views use endpoint-pair keys; UID, GID and file views use detailed
keys. This limit applies per bpftrace map and is not a distinct-client/file limit.
On shutdown, the frontend signals only its owned process group and removes the
temporary script. It does not signal the user's shell or unrelated tracing jobs.

## Sampling and tradeoffs

Native avoids subprocess startup, script compilation and text-frame processing.
bpftrace can be useful on hosts already maintained for that tracing environment,
and its compiler/probe diagnostics help investigate that backend. These are
implementation and deployment differences, not a measured CPU or throughput
comparison. See the [backend selection table](../README.md#choose-a-backend).

Both userspace map iteration and bpftrace map printing are live snapshots, not
atomic snapshots of all CPUs. If bpftrace output arrives faster than the frontend
can consume it, queued snapshots may be coalesced. Cumulative byte totals remain
intact, but fewer samples reduce peak time resolution. Missed return probes and
map-capacity losses cannot be reconstructed by the UI. Inspect tracefs
`kprobe_profile` when investigating probe loss under high concurrency.

## Terminal lifecycle

Interactive rendering uses Bubble Tea's alternate screen and terminal mode
restoration. No `clear`, erase-scrollback (`ESC[3J`), `stty sane`, shell-history
rewrite or shell-directed signal is used. Errors pass through cleanup before
printing to stderr. `-t`, redirected stdin/stdout and JSON use no ANSI controls.
