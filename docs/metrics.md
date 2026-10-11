# Metrics

READ/WRITE follow the NFS client's file operation. A READ delivers file content
from server to client; a WRITE accepts client content at the server.

The collectors retain cumulative success counters and compute userspace
differences. They do not clear live data counters at each tick.

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
outgoing NFS client path. It therefore keeps access to an upstream whale mount
outside this machine's NFSD per-client list.
