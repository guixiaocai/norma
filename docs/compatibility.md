# Compatibility contract

Support depends on architecture, NFSD function semantics and accurate layout
metadata. A distribution name or kernel version number alone does not establish
compatibility. Build success does not establish runtime probe/workload support.

The primary deployment target is **Rocky Linux 8.10 x86_64**. Follow the
[English](rocky-linux-8.10.md) or [Chinese](rocky-linux-8.10.zh-CN.md) installation
guide for the EL8 layout route. Other Linux x86_64 hosts can use the same
executable when they meet this contract; this is not an all-kernel or
all-architecture support claim.

Run `sudo norma --doctor` before collection. It checks deployment prerequisites
without loading BPF or attaching probes, reports missing requirements and
returns nonzero on failed checks. Actual attachment, security permissions and
workload accounting must still be verified on the intended server. To preview
the dashboard without any NFS or probe requirements, use `norma --demo`.

## Shared requirements

Both `norma` backends require:

- Linux x86_64 with in-kernel NFSD already serving NFSv3 or NFSv4 exports.
- BPF syscall, kprobes/return probes and compatible BPF helpers/maps, with
  permission to load and attach them. Running as root is the practical default;
  kernel security policy may still restrict tracing.
- Recognized ordinary-read, splice-read and write function semantics, as below.
- Accurate layouts for `svc_rqst`, `file`, `inode`, `super_block` and `dentry`,
  obtained from usable runtime BTF or a reviewed matching JSON layout.

Only Linux amd64 binaries are supplied. Other architectures require a register
ABI adapter and corresponding backend argument mapping.

| Deployment requirement | Native (default) | `--backend bpftrace` |
|---|---|---|
| `norma` executable | Required; includes embedded BPF object | Required; includes bpftrace frontend/script generator |
| Runtime bpftrace | Not required | >= 0.16, with working dependencies and required probe/features support |
| Runtime Go compiler, clang, libbpf shared library | Not required | No separate Go build required; bpftrace needs its own compilation/runtime environment |
| NFSD profiles, layout and BPF permissions | Required | Same requirements |

The native collector uses kprobes and hash/per-CPU maps, without ring-buffer or
fentry requirements. Selecting bpftrace does not bypass the shared layout or
function-profile requirements. Backend failures are reported, with no automatic
fallback. The [README comparison](../README.md#choose-a-backend) explains which
backend to choose and their operational tradeoffs.

## Layout selection

1. If `--layout file.json` is supplied, validate and use that file.
2. Otherwise, try runtime BTF types and function prototypes from loaded
   `nfsd`/`sunrpc` modules and vmlinux. Missing module BTF is not always fatal if
   another source contains the complete required metadata.
3. Try a prepared `/var/cache/norma/layout-<running-release>.json`.
4. Report missing layout support and guidance for a reviewed explicit layout.

```bash
sudo norma --layout /path/to/layout.json
sudo norma --backend bpftrace --layout /path/to/layout.json
```

Layout JSON is bound to the running kernel release and architecture to prevent
using incompatible memory offsets. After a kernel change, provide newly resolved
or regenerated data; the executable can remain the same when the function
semantics still match a supported profile.

## Supported function semantics

All three probe slots must be resolved; incomplete read coverage is rejected.

| Probe slot | Recognized functions |
|---|---|
| Ordinary read | `nfsd_readv`, or `nfsd_iter_read` with the same recognized status/count semantics |
| Splice read | `nfsd_splice_read` |
| Write | `nfsd_vfs_write` |

The BTF resolver recognizes unsigned 32-bit NFS-status returns, the request/file
pointer arguments by type, and the `count`/`cnt` completed-count pointer by name
and unsigned integer type. It accepts 4/8-byte counts, argument positions 1–8,
and `struct nfsd_file *` wrappers exposing `nf_file`.

Signed byte-return functions, scalar requested-count arguments, asynchronous
completion, unfamiliar wrappers or different RPC transport markers require an
explicit profile/collector change. Matching a symbol name alone is insufficient.
A custom JSON file can describe offsets and argument placement for reviewed
compatible semantics; it cannot make a different completion model compatible.

## Advanced header helper

`tools/prepare-layout.sh` is a constrained fallback for its built-in direct-file
function profile. It is **not a general layout generator for arbitrary kernel
headers**. The helper checks its supported kernel family and matching headers;
if rejected, use complete runtime BTF or a reviewed `--layout` profile.

On a host covered by that helper, install gcc, make and development headers
matching the running kernel, then run:

```bash
sudo ./tools/prepare-layout.sh
# Installed location:
sudo /usr/local/share/norma/tools/prepare-layout.sh
```

It compiles an unlinked object containing thirteen `offsetof` constants, without
building/loading a kernel module or changing NIC drivers. `norma` reads the
constants from the ELF object and the helper writes JSON atomically. This route
supplies field offsets for a fixed profile; it does not discover new NFSD
function semantics from headers.

The JSON schema is defined in `internal/layout/layout.go`: kernel/architecture
identity, field offsets and three hooks with argument positions, count width,
and wrapper offset (`4294967295` denotes a direct `struct file *`). Review both
kernel source semantics and offsets before supplying a custom layout.

## NICs and interface filtering

NFS/RDMA across supported NIC drivers reaches the same NFSD handling layer;
collection does not use vendor NIC counters. Monitoring installation does not
change exports, mount options, NFS configuration or NIC drivers.

`-i` resolves the interface's current local IP addresses once at startup and
filters the server destination address of RPC requests. It does not identify
physical ingress with policy routing, bonding or shared addresses. Restart after
an address change. IPv4 and IPv6 parsing are included; IPv6 link-local scope is
not encoded in the grouping key, so prefer unique/global addresses where
identical link-local addresses exist on multiple interfaces.

## Collection limits

Both backends use bounded maps and return probes. Native client/server totals
have a map separate from detailed file/UID/GID data, with additional map/read
health counters. bpftrace follows its configured per-map key limit and reports
its own launch/runtime diagnostics. Neither can reconstruct undelivered events.
See [architecture](architecture.md) for map limits, snapshot timing and diagnostics.

Primary references:

- [Linux NFSD VFS source](https://github.com/torvalds/linux/blob/master/fs/nfsd/vfs.c)
- [Linux BTF documentation](https://docs.kernel.org/bpf/btf.html)
- [Linux kprobe documentation](https://docs.kernel.org/trace/kprobes.html)
