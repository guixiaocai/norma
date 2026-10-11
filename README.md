# NFSmon

NFS server I/O by client, with an iftop-style terminal interface.

Two collectors share the same command line and display: **nfstop** attaches
native eBPF probes; **nfsdtop** supervises a bpftrace collector. Both count
successful NFSD VFS read/write completion bytes for NFSv3/v4. The default
transport is **NFS over RDMA**. TCP and UDP NFS are selectable.

| Feature | Both tools |
|---|---|
| Per-client read/write | Current rate, 1-minute average, 5-minute average, 5-minute peak, session total |
| Window | `-w 3m`, `-w 10m`, up to 24 hours |
| Units | Automatic B/s → KiB/s → MiB/s → GiB/s; `-b` selects bits/s |
| Terminal | Alternate screen; `q` or Ctrl+C restores the original screen and terminal mode |
| Scope | Clients, servers, UIDs, GIDs, files; client IP/CIDR and local-interface address filters |
| Output | Interactive, plain text, JSON Lines |
| Kernel adaptation | Runtime BTF signatures and offsets, or per-kernel layout data generated from EL8 headers |

## Quick start

Download and unpack a `nfsmon-vX.Y.Z-linux-amd64` release archive:

```bash
sudo ./tools/install.sh
sudo nfstop
sudo nfsdtop
```

`nfsdtop` additionally needs `bpftrace >= 0.16`. On Rocky/Alma/RHEL 8 with no
usable module BTF, prepare the kernel layout once:

```bash
sudo dnf install -y gcc make "kernel-devel-$(uname -r)"
sudo ./tools/prepare-layout.sh
sudo nfstop
# For the alternative collector:
sudo dnf install -y bpftrace
sudo nfsdtop
```

Use the matching kernel headers after a kernel update to regenerate the layout
data. **The executable does not need rebuilding for an EL8 kernel patch update.**
These tools do not change exports, mount options, NFS service configuration or
NIC drivers. The NFS server must already be running.

```bash
sudo nfstop                         # NFS/RDMA, client view
sudo nfstop -p tcp                  # ordinary TCP NFS
sudo nfstop -i eno2                 # TCP NFS to eno2's local IP addresses
sudo nfstop -p rdma -i ib0          # RDMA NFS to ib0's local IP addresses
sudo nfstop -p all -F 40.0.0.0/16  # all NFS transports, selected client network
sudo nfstop -w 10m -b               # 1m and 10m averages, 10m peak, bit/s
sudo nfsdtop -g files               # per-file device/inode accounting
sudo nfsdtop -t -s 120              # plain text for two minutes
sudo nfstop --json -s 60            # machine-readable byte-based metrics
```

`-i` is an **NFS local-address filter**, not a packet capture/ingress filter.
These tools monitor ordinary **NFS** traffic when TCP is selected; they do not
replace a general all-protocol packet monitor.

## Display and controls

Each client has READ and WRITE rows. READ means a client reads from this NFS
server; WRITE means a client writes to it. `NOW` is the latest sampling interval.
`AVG 1m` and `AVG 5m` are time-weighted rolling averages including idle time.
`PEAK 5m` is the maximum sample rate in the same window. The default sample is
one second, configurable with `-r`.

| Key | Action |
|---|---|
| `q`, Ctrl+C | Exit and restore the original terminal |
| `B` | Toggle automatic byte/s and bit/s units |
| `o`, `--ops` | Toggle/show completed operations per second |
| `s` | Cycle sort by current, 1m average, window average, peak, total, name |
| `t` | Both directions, read, write, combined |
| `p` | Freeze the display; sampling and rolling-window updates continue |
| Up/Down, j/k, Page Up/Down | Scroll |

No screen-clear command or scrollback erase is used. Redirection and `-t` use
append-only text with no terminal escape sequences. Optional `-N` name lookup
runs asynchronously; addresses remain numeric by default.

## Compatibility and accounting

The main deployment range is the **x86_64 Rocky/Alma/RHEL 8 standard 4.18 kernel
family**, not one exact kernel release. A BTF route also adapts kernels exposing
the supported NFSD function semantics. Unknown signatures are reported as
capability errors instead of guessed from a version number.

See [compatibility](docs/compatibility.md) for requirements and scope,
[metrics](docs/metrics.md) for window and peak definitions, and
[architecture](docs/architecture.md) for the two backends. CX3, CX4 and later
adapters use the same NFSD collection path; supported NIC drivers do not need
modification.

These are **server logical completion bytes**, including reads satisfied by
the server page cache. They exclude RPC/RDMA headers and are not wire traffic,
physical-disk throughput or a guarantee of reply delivery/durable storage.
The host's own outgoing NFS client access is outside these server probes.

## Build and publish

Build with Go >= 1.25.3, clang/LLVM, libbpf development headers and gcc:

```bash
# Example on Ubuntu:
sudo apt-get install clang llvm libbpf-dev gcc make zip
make build
make release VERSION=v0.2.0
```

The repository includes build-only GitHub Actions and a tag-triggered Release
workflow. It produces both static Go executables, tar/zip archives and SHA256
checksums. See [publishing](docs/publishing.md) for the complete sequence.
Only Linux amd64 binaries are advertised; arm64 needs a separate register ABI
adapter. The bpftrace frontend still requires an installed bpftrace executable.

## Project status

This version was prepared as a source/binary release candidate. No tests,
probe attachment, NFS workload or cross-kernel runtime validation were performed
for this revision. Build workflows also intentionally omit tests.

## License and upstream credit

New code: MIT. Native BPF: MIT OR GPL-2.0. bpftrace backend: BSD-2-Clause.
Upstream notices and dependency licenses are retained in [NOTICE](NOTICE) and
`licenses/`. This is a downstream project; see NOTICE for upstream links.

[中文说明](README.zh-CN.md)
