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

`--doctor` runs read-only environment and layout checks before either collector
is created. It never tests a live BPF attachment. `--demo` supplies deterministic
synthetic cumulative snapshots to the shared frontend and never starts a real
collector. Demo mode is labeled in the interactive display and can also exercise
the text/JSON output paths without root or a running NFS server.

## Common collection contract

Both backends attach entry and return probes to three NFSD VFS helper paths:
ordinary read, splice read and write. They count completed bytes from an updated
count pointer only on successful NFS status, for NFSv3/v4. The resolved layout
supplies argument positions, count widths and field offsets. Transport is part
of every counter and diagnostic key: TCP `6`, UDP `17`, RDMA `256`; other values
are retained rather than guessed. The CLI opens either backend for all
transports, then projects transport, address and grouping selections in the
frontend. Switching groups or drilling into a client does not restart probes.

Every snapshot contains detailed file/UID/GID records, independent endpoint-pair
summaries and separate per-client diagnostics. The collectors retain these
datasets together regardless of the current view. Client/server totals can
therefore remain available even when the detailed map is full. Diagnostic keys
include client, server and transport, with READ and WRITE durations/error counts;
they do not attribute latency to a file, UID or GID. The same three helper paths
supply these timings; there are no GETATTR, LOOKUP or READDIR metadata probes.

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
| Client/server/transport endpoint pairs | 4,096 |
| Detailed client/server/transport/UID/GID/device/inode keys | 16,384 |
| Inflight task/probe records | 8,192 |
| File name hints | 16,384 |
| Shared latency/error counters and histograms, one key per endpoint pair/transport/direction | 8,192 |
| Per-CPU latency maxima, with the same directional key | 8,192 |

Each latency histogram value occupies 280 bytes: cumulative calls, failed calls,
total nanoseconds and 32 unsigned 64-bit buckets. Shared hash entries use atomic
additions; only the 8-byte maximum is stored per CPU and reduced in userspace.
This avoids multiplying the full histogram by the CPU count and needs neither
BPF spin locks nor compare-exchange instructions. A one-element, zero-valued
array provides the map-insertion seed without placing the histogram on the BPF
stack. Memory is bounded, but CPU count and active keys still affect usage.

Entry records save a monotonic timestamp and endpoint identity before attempting
file metadata. Return processing records latency and nonzero NFS status before
reading the completed-byte pointer. A missing file identity can therefore leave
endpoint byte totals and diagnostics available; a failed count-pointer read
increments `read_errors` and does not create a successful byte/operation record.
Histogram or maximum-map capacity failures increment `diagnostic_drops` without
disabling the separate byte counters.

The file identity is device+inode. A regular hash map also supplies
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
Entry timestamps and endpoint/transport identity are saved independently of the
completed-byte pointer. Return probes use `count()`, `sum()` and `max()` plus
explicit logarithmic buckets for cumulative latency and errors, including failed
helper returns.

The default `BPFTRACE_MAP_KEYS_MAX` is 16,384 unless the user overrides it. Separate
maps always collect detailed and endpoint-pair counters. Diagnostic counters use
endpoint/transport/direction keys; histogram keys additionally include a bucket
index. The limit applies per bpftrace map, not per distinct client or file, so
one client can occupy multiple diagnostic keys. The script reports missing
detail/count pointers, but it cannot reliably distinguish every arbitrary
kernel-memory read failure from a zero value as the native collector can.
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

The latency mean, maximum and histogram may reflect slightly different moments
during a live snapshot. Percentiles are available only when the bucket sum equals
the completed-call count and a nonzero maximum is available. Otherwise the
snapshot reports `diagnostic_incomplete` and/or `max_unavailable`, preserving raw
counters while withholding the derived quantiles. Warnings can clear in a later
coherent snapshot; they do not by themselves distinguish transient snapshot skew
from map loss. Available percentiles report bucket upper bounds capped by the
observed maximum; they are approximate session statistics, not exact samples or
rolling-window quantiles. See
[metrics](metrics.md#helper-latency-and-failures).

## Terminal lifecycle

The responsive dashboard divides context, READ/WRITE/SESSION overview, entity
details, collector health and controls into separate regions. Smaller widths
reduce visible columns and panels, then use a compact summary. The underlying metric calculations and
JSON fields do not depend on terminal size. `--no-color` and nonempty `NO_COLOR`
disable color without changing the interactive layout.

Interactive rendering uses Bubble Tea's alternate screen and terminal mode
restoration. No `clear`, erase-scrollback (`ESC[3J`), `stty sane`, shell-history
rewrite or shell-directed signal is used. Errors pass through cleanup before
printing to stderr. `-t`, redirected stdin/stdout and JSON use no ANSI controls.
