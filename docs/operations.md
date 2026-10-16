# Deployment and live investigation (v0.5)

[简体中文](operations.zh-CN.md) | [README](../README.md)

**Rocky Linux 8.10 x86_64 is the primary deployment target.** Install on the
machine serving NFS exports; these probes cannot monitor a remote server from
an NFS client machine. Native monitoring needs no target-host Go, clang,
bpftrace or libbpf shared library. Kernel capabilities and NFSD layouts still
determine support; see [compatibility](compatibility.md).

## RPM for Rocky, AlmaLinux and RHEL

Use the Linux x86_64 RPM for managed fleets. It contains the static binary,
runtime helpers, node configuration example, documentation and licenses.
Install the downloaded, SHA256-verified package on the NFS server:

```bash
sudo dnf install ./norma-0.5.0-1.el8.x86_64.rpm
rpm -q norma
rpm -ql norma
norma --demo
sudo norma --doctor
sudo norma
```

RPM installs `/usr/bin/norma` and `/usr/share/norma`. If doctor reports missing
layout metadata, follow [server preparation](#check-and-prepare-the-running-server).

Upgrade using `sudo dnf upgrade ./norma-NEW-1.el8.x86_64.rpm`; roll back using
`sudo dnf downgrade ./norma-OLD-1.el8.x86_64.rpm`. Retain the previous RPM for an
isolated network. `sudo dnf remove norma` removes package-owned files and keeps
layout caches and operator-created exports. RPM and tar installations must not
be overlaid: remove the existing installation through its own management path
before switching. RPM packages target EL8 userland; the NFSD kernel/layout
contract still applies on Rocky, AlmaLinux and RHEL.

## Tar installation, upgrade, rollback and removal

Online and extracted-release installers support `--prefix`; `DESTDIR` remains
available for package staging. The new tar installer records relative paths,
permissions and SHA256 hashes in
`<prefix>/share/norma/.install/manifest.tsv`. It snapshots the complete tracked
payload before an upgrade, including the binary, helpers and documentation.

```bash
curl -fsSL https://raw.githubusercontent.com/guixiaocai/norma/main/install.sh -o install-norma.sh
sudo bash install-norma.sh --version v0.5.0
norma --version
norma --demo
norma-manage list
sudo norma-manage rollback SNAPSHOT_ID
sudo norma-manage uninstall
```

Omit `--version` to select the latest stable release. The installer verifies the
archive against `SHA256SUMS` before extraction; it supports curl or wget and
defaults to `/usr/local`. Use `bash install-norma.sh --help` for options.
`--demo` previews the real dashboard with synthetic data, without root, NFSD or
BPF attachment; press `q` to exit. The [README](../README.md#display-and-controls)
includes a labeled preview image.

For an existing v0.4 tar installation, use `--adopt-legacy` once when installing
v0.5. The installer recognizes the previous norma layout and records it before
replacement. It refuses unrelated files rather than silently taking ownership.
Each rollback first snapshots the current version, making reversal possible.
Binary replacement is atomic; an already running monitor continues using its
opened executable. Restart it to run the replacement version. Multiple-file
installation is serialized, but is not a filesystem-wide atomic transaction.

Uninstall preserves layout caches, saved installation snapshots and user-created
recordings by default. Locally modified managed files are preserved. If the
management command was removed by uninstall or by rollback to a pre-manager
release, recover using a freshly extracted v0.5 release:

```bash
bash ./tools/manage.sh --prefix /usr/local list
sudo bash ./tools/manage.sh --prefix /usr/local rollback SNAPSHOT_ID
```

`uninstall --purge-data` also removes norma's installation snapshot tree and
built-in `layout-*.json` cache files (system prefixes only). It does not search
for or delete user-selected `--layout` files, redirected JSON recordings or
other operator files. Norma's live rolling statistics are held in memory; it
does not automatically record monitoring history to disk.

For a user-owned installation:

```bash
bash install-norma.sh --version v0.5.0 --prefix "$HOME/.local"
# Extracted archive alternative: ./tools/install.sh --prefix "$HOME/.local"
export PATH="$HOME/.local/bin:$PATH"
norma --demo
sudo "$HOME/.local/bin/norma" --doctor
sudo "$HOME/.local/bin/norma"
```

Installation/demo need no root with this prefix; live collection still needs
BPF/probe permissions. Use the full path under sudo because its PATH may omit
your personal bin directory. When layout preparation is needed, use:

```bash
sudo env NORMA_BIN="$HOME/.local/bin/norma" "$HOME/.local/share/norma/tools/prepare-layout.sh"
```

## Prepare one offline deployment directory

On a connected machine, download a specified version and prepare its deployment
materials in one directory:

```bash
./tools/bundle-offline.sh --version v0.5.0 \
  --kernel 4.18.0-553.el8_10.x86_64 --format rpm \
  --output ./norma-offline-v0.5.0
```

Omit `--format rpm` to select the tar package. The bundle includes the verified
release asset, published checksums, documentation, offline installation entrypoint
and dependency manifest for the supplied target kernel. The kernel must be the
target server's exact `uname -r`, not the downloader's kernel. EL8 header-helper
dependencies list gcc, make and the exact kernel-devel package; native runtime
does not require those packages if usable BTF/layout data already exists.

Dependency RPMs are not downloaded automatically. Obtain them and their
dependencies through the target distribution's approved package sources before
entering the isolated network. Transfer the directory together and follow its
included installation instructions. The bundle does not contain a universal
kernel layout or install/change the NFS service.

```bash
cd norma-offline-v0.5.0
sha256sum -c BUNDLE-SHA256SUMS
sudo bash install-offline.sh
```

The offline entrypoint verifies the bundle again. For RPM it disables remote
repositories; provide any missing dependencies locally first. For tar it accepts
the same `--prefix`, `--adopt-legacy` and `--prepare-layout` options as the local
installer.

For a simple manual tar transfer, copy the archive and `SHA256SUMS` from the
same release, then verify before extracting:

```bash
sha256sum --ignore-missing -c SHA256SUMS
tar -xzf norma-v0.5.0-linux-amd64.tar.gz
cd norma-v0.5.0-linux-amd64
sudo ./tools/install.sh
```

Continue only when the downloaded archive reports `OK`. The local installer
requires no network; layout preparation additionally needs the matching system
packages described below.

## Check and prepare the running server

`sudo norma --doctor` checks OS/architecture, running kernel, active NFSD worker
threads, BTF/cache/explicit layout resolution, required symbols when kallsyms is
readable, and bpftrace version when selected. Failed prerequisites return a
nonzero status; root/lockdown observations are advisory. It does not load BPF,
attach probes, install packages or change services. Passing it does not prove
live attachment or workload accuracy. Doctor uses a plain checklist and cannot
be combined with `--json` or `--demo`.

The NFS server must already be configured and serving exports. When a supported
`4.18.0-*.el8*` kernel lacks usable BTF, install gcc, make and development headers
that **exactly match the running kernel**, then prepare the layout:

```bash
sudo dnf install gcc make "kernel-devel-$(uname -r)"
sudo /usr/share/norma/tools/prepare-layout.sh       # RPM installation
# For the default tar prefix, use /usr/local/share/norma/tools/prepare-layout.sh
sudo norma --doctor
```

The helper compiles field-offset constants into an unlinked object and writes
`/var/cache/norma/layout-<running-kernel-release>.json`. It does not load a kernel
module, change drivers or rebuild norma. Its fixed EL8 function profile rejects
mismatched headers and other kernel families. If matching kernel-devel is
unavailable, obtain the exact package through the approved Rocky repositories,
or update/reboot into an approved kernel with matching headers. Never substitute
another kernel's headers; regenerate after a kernel change if still needed.

Once these dependencies exist, tar installation can prepare the layout with
`sudo bash install-norma.sh --version v0.5.0 --prepare-layout` or
`sudo ./tools/install.sh --prepare-layout`. It does not install system packages
automatically. A reviewed custom layout can be checked and used explicitly:

```bash
sudo norma --doctor --layout /path/to/layout.json
sudo norma --layout /path/to/layout.json
sudo norma -p tcp                  # TCP view
sudo norma -p rdma -i ib0           # RDMA view, filtered by ib0's local addresses
sudo norma --no-color              # monochrome dashboard
sudo norma -t -s 60                # one minute of plain text
sudo norma --json -s 60            # one minute of JSON Lines
```

Default transport has been `all` since v0.4; add `-p rdma` to retain an older
RDMA-only workflow. `-i eno2` without `-p` selects TCP; explicit `-p` takes
precedence. Interface filtering uses local IP addresses, not physical ingress.
TCP NFS over IPoIB remains TCP. The optional `--backend bpftrace` requires
bpftrace >= 0.16 and the same compatible layouts; there is no automatic fallback.

## Follow a client through the live data

Start `sudo norma`, select a client with Up/Down, and press Enter to inspect
that client's file counters. `u` switches the client detail between files and
UIDs. `Esc` returns. The read and write rows, average operation size and selected
entity's helper diagnostics explain the selected workload. `First` means the
first activity observed during this monitor session, resolved to sampling time;
it cannot establish when a workload began before norma started.

| Key | Investigation action |
|---|---|
| `g` | Cycle clients, files, UIDs, GIDs, server addresses, transports and node groups |
| `/`, Enter | Enter an IP/CIDR or `file=substring` filter and apply it |
| Esc | Cancel editing, return from detail, or clear view filters |
| `T` | Cycle all, TCP, RDMA and UDP transport views |
| Enter on a transport | Expand that transport's clients |
| `o` | Toggle bytes/s and completed operations/s |
| `t` | Both directions, READ, WRITE or combined |
| `s` | Select NOW, averages, peak, session total or name ordering |
| `d` | Open selected-entity diagnostics on a compact terminal; d/Esc returns |

Sorting uses the displayed metric and direction: READ ops/s ranks READ operation
rates, WRITE total ranks WRITE session totals, and combined peak uses the maximum
simultaneous combined sample. Filtering, regrouping and drill-down reuse retained
session attribution without detaching probes or resetting counters. These are
view filters; collection retains all supported transports so changing `T` can
show earlier samples. Address/interface filters are also applied in userspace.

The header displays transport-separated throughput alongside the entity view;
transport identity is recorded in both collectors' aggregation keys. Average
READ/WRITE operation size is successful session bytes divided by successful
operations. Rate share compares an entity with the current filtered population,
using the selected units and direction. File view remains keyed by **device
number plus inode**. Parent/basename is a bounded optional hint, not a full export
path; bpftrace supplies no filename hints. Renames, hard links, inode reuse and
truncated/stale hints must be considered when matching a path.

## Use cluster names without losing IP identity

```bash
sudo norma --config /path/to/nodes.json
sudo norma --config /path/to/nodes.json -g nodegroups
```

See [examples/nodes.json](../examples/nodes.json):

```json
{"nodes":{"192.0.2.21":{"name":"compute01","group":"compute"},
          "192.0.2.23":{"name":"login01","group":"login"}}}
```

`NORMA_CONFIG` can also select the file. Client labels retain the IP; labels
never merge clients implicitly. `nodegroups` is the explicit aggregate by role.
DNS lookup with `-N` also retains IP identity; configured aliases take priority.

## Read slow-operation diagnostics correctly

Both collectors timestamp the supported NFSD VFS helper entry/return and retain
per-client/server/transport READ/WRITE call counts, failures and duration buckets.
The selected-client panel shows session average, P95, P99 and maximum duration.
JSON retains raw cumulative nanoseconds and the histogram. Percentiles are
**histogram upper bounds**, not exact per-operation samples: bucket i is up to
2^i microseconds, with the last bucket saturated and bounded by the measured
maximum. Durations include failed helper returns; successful byte/operation
rates exclude them. A nonzero NFS status counts as a failed helper call.

These durations measure NFSD VFS helper elapsed time, including scheduler waits
inside that interval. They do not measure end-to-end client RPC, network/RDMA
latency, durable disk writes, or time outside the helper. The maximum and
percentiles are session cumulative, separate from the rolling throughput window.
Client-only failures remain visible even when no successful bytes were counted.
The client histogram cannot be attributed to an individual file or UID; those
views explicitly identify helper diagnostics as unavailable.

Counts, buckets and maxima are live reads. If bucket counts do not match calls,
or a required maximum is missing, affected percentile/maximum values are marked
unavailable and JSON carries availability flags. A later coherent snapshot can
restore them. In bpftrace, sequential map printing can make this more frequent
under continuous load; an incomplete histogram must not be read as zero latency.

Host correlation shows CPU busy percentage, NFSD thread count and the busiest
whole block device/interface rates when their `/proc` data is readable. CPU idle
includes iowait; guest time is not counted twice. Disk sectors use Linux's 512-byte
accounting unit. Interface READ/WRITE fields in JSON mean RX/TX. Loopback is
excluded; these are host-wide indicators, and an unrelated workload or a virtual
device can dominate. The selected busiest device can change between samples.
Unavailable readings stay absent, and demo mode does not mix real host statistics
with synthetic NFS data.

## Troubleshooting

| Symptom | Next step |
|---|---|
| Command not found | Use `/usr/bin/norma` for RPM, `/usr/local/bin/norma` for default tar, or the explicit user-prefix path. |
| Missing BTF/layout or mismatched headers | Compare kernel-devel with `uname -r`, run the supported EL8 helper and repeat `--doctor`. |
| NFSD unavailable | Run on the actual NFS server and check its existing service and exports. |
| BPF/probe permission failure | Check privileges and host tracing/security policy; doctor does not test attachment. |
| No client rows | Generate real client I/O and check transport/address filters. Client-cache hits without RPC do not appear. |
| Crowded terminal | Enlarge it, use `d` for selected-entity diagnostics, or choose `--no-color`, `-t` or `--json`. |
| Unsupported NFSD signature | Use supported BTF or a reviewed compatible layout; switching backend does not change the semantics requirement. |

## Limits and next probe families

Snapshots and kernel maps are bounded and non-atomic. Inspect health counters
for lost attribution or diagnostic capacity failures; missing return probes
cannot be recovered. Detailed maps may fill while independent client summaries
remain useful. A view can only show data retained by its source map.

Userspace history bounds retained samples to prevent unbounded growth at high
cardinality or long windows. When a needed portion of history has expired under
that guard, affected averages/peaks are shown as unavailable, with JSON validity
flags and a health warning. Current rates and cumulative counters remain usable;
missing older samples are not presented as an accurate window statistic.

GETATTR, LOOKUP, READDIR, CREATE and REMOVE probes are a follow-up family. This
release adds helper latency/error diagnostics first and does not label those
metadata operations as measured. Each added operation needs reviewed function
semantics, compatibility coverage, failure behavior and workload validation.
Full export-path reconstruction, including rename/hard-link handling, is also
future work. See [metrics](metrics.md) and [architecture](architecture.md).
