# Metrics

READ/WRITE follow the NFS client's file operation. A READ delivers file content
from server to client; a WRITE accepts client content at the server.

The collectors retain cumulative success counters and compute userspace
differences. They do not clear live data counters at each tick.

The dashboard's READ and WRITE overview cards show the selected population's
totals; the SESSION area identifies observed time and the active view. The
table's `SESSION` column is the same cumulative byte/operation measure previously
labeled `TOTAL`. Compact terminal widths hide columns, not data; widen the
terminal or use text/JSON output to inspect the complete statistics.

`--demo` uses synthetic snapshots, clearly labeled in the interactive display.
Its rates and totals demonstrate formatting and controls and must not be used
as real workload evidence.

## Rolling averages

For a segment of elapsed length `dt`, the rate is `delta_bytes / dt`. A window
average is the sum of each rate multiplied by its overlap with the requested
window, divided by the observed window length. Segments with no traffic have
rate zero and still contribute elapsed time to the denominator.

The observed window length is `min(window, time_since_monitor_start)`. Before
the tool has run for five minutes, AVG 5m is the observed shorter-period average.
The display identifies how much of the window is available. No historical
traffic from before tool startup is implied.

At a window boundary intersecting a sample, the implementation assigns the
sample's average rate uniformly across that segment. This is the sampling
resolution; it does not infer sub-sample timing.

## Peaks

PEAK 5m is the maximum **individual sample rate** overlapping the last five
minutes. It is not a five-minute moving-average maximum or an instantaneous
packet/hardware peak. A burst entirely between samples is included in those
samples' byte differences but its sub-sample peak is not recoverable.

The combined READ+WRITE view takes the maximum of simultaneous combined sample
rates. The total footer uses samples of all clients combined, so its peak is
not the sum of different clients' peaks at different times.

Pause freezes the screen only. Acquisition, history pruning and peak expiration
continue; resuming shows the current rolling window.

## Units

Default bytes use binary prefixes B, KiB, MiB, GiB, TiB, PiB. `-b` / interactive
`B` selects decimal bit, kbit, Mbit, Gbit, Tbit, Pbit. Each rate/total is formatted
independently. JSON always uses raw bytes, bytes/s, operation counts and ops/s;
it never depends on presentation units.

## Helper latency and failures

READ and WRITE diagnostic statistics measure **NFSD VFS helper entry to return**
using monotonic nanoseconds. Ordinary reads and splice reads contribute to READ.
The population includes successful and failed helper returns; failure means a
nonzero returned NFS status. These are not end-to-end client RPC latency, network
RTT, RDMA completion latency or physical-disk service time. Metadata operations
such as GETATTR, LOOKUP and READDIR are not instrumented.

| Statistic | Definition |
|---|---|
| Calls | Observed completed helper calls, including failures |
| Errors | Calls whose returned NFS status is nonzero |
| Mean | Cumulative helper nanoseconds divided by completed calls |
| P95 / P99 | Approximate session quantiles from cumulative histogram counts |
| Max | Largest observed completed helper duration during this session |

These are **session** statistics, independent of the throughput averaging window.
Diagnostics are collected per client/server/transport and direction; a file view
does not turn them into per-file latency measurements. Bytes and successful
operation counters remain separate: failed calls contribute latency/errors but
no successful bytes, and a count-pointer read failure can leave a duration
without a byte count. Collector read/capacity errors are health counters and must
not be interpreted as returned NFS errors.

Histogram buckets use a base-2 scale in microseconds. Durations are rounded up
to microseconds; bucket 0 contains durations up to 1 µs, bucket 1 up to 2 µs,
bucket 2 up to 4 µs, and so on. Bucket 31 also absorbs longer durations.
When the histogram count matches the completed-call count and a nonzero maximum
is available, P95/P99 select the first bucket reaching the requested rank among
histogram entries, report that bucket's upper bound and cap it at the observed
maximum. For the saturated final bucket, the maximum supplies the upper bound.
For example, a P95 displayed as 128 µs may come from samples between 64 and
128 µs; it is not a precise 128 µs measurement.

Maps are read while collection continues, so calls, totals, histogram counts
and maximum are not a globally atomic snapshot. Incomplete histograms or missing
maxima are marked unavailable: the UI shows `—`, and JSON summaries include
availability flags alongside the preserved raw counters. The health message
`diagnostic_incomplete` can indicate transient snapshot skew or map loss;
`max_unavailable` indicates that no usable maximum was observed. Merging several
clients must preserve any source's unavailability even if mismatches cancel in
the combined counts. Missing return events and full maps can undercount;
inspect collector health before drawing conclusions. A
zero value with no completed calls means there is no observation, not proof of
zero latency or perfect service.

## Transport and view projection

Both backends retain the actual request transport in detailed counters,
endpoint summaries and diagnostics. TCP is `6`, UDP is `17`, RDMA is `256`, and
unrecognized values remain distinguishable. Runtime transport/address/group
selections project these retained data without detaching the collector.
Independent endpoint summaries protect client/server throughput from detailed
file-map capacity limits; a detailed view may therefore account for less data
when its map is full. This difference is reported through collector health.

## Accounting boundary

Completed bytes are read from the VFS helper's updated count pointer only after
zero NFS status. Failed helper operations do not contribute successful bytes.
NFSv4 actual read execution may occur while encoding a reply; measuring only
the NFSv4 READ dispatcher entry/return is insufficient for this metric.

Server-cache reads count; client-cache hits which issue no RPC do not. Protocol
headers, wire retransmissions, RDMA verb traffic and physical disk writes have
different accounting boundaries. A VFS operation can succeed before a later
reply-send failure; this counter does not claim proof of client receipt or
media durability. Re-executed successful operations can be counted more than
once according to the server's actual work.

The frontend's sampling/terminal cleanup adds no probes on the machine's own
outgoing NFS client path. It therefore keeps access to another NFS server
outside this machine's NFSD per-client list.
