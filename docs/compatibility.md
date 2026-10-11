# Compatibility contract

Support is defined by architecture, NFSD probe semantics and available layout
metadata, rather than equality to one kernel release string. This revision
contains no cross-kernel runtime test results.

| Platform or capability | Implementation scope | Layout source |
|---|---|---|
| Rocky/Alma/RHEL 8, x86_64, standard distribution 4.18 kernels | Primary target family; all patch releases with the listed NFSD signatures and fields | Matching kernel-devel header constants, or usable runtime BTF |
| Other x86_64 kernels with complete BTF | Conditional adaptation when the resolver recognizes all required prototypes | `/sys/kernel/btf/nfsd`, `sunrpc`, `vmlinux` |
| Mainline/distro 5.x and 6.x | Candidate families, not an unconditional guarantee; BTF prototype recognition is required | Runtime BTF or explicitly reviewed custom layout |
| Custom/backported kernels | Supported when their function semantics match a profile and layout data is correct | Runtime BTF or `--layout file.json` |
| CentOS/RHEL 7 3.10 | Outside the advertised release range | Use the earlier SystemTap approach |
| arm64, other architectures | Not supplied by this release | Add a register ABI adapter and backend argument mapping |

## Required capabilities

- Linux in-kernel NFSD providing the monitored exports; NFSv3 or NFSv4.
- BPF syscall, kprobes and return probes, compatible BPF helpers/maps, permission
  to load and attach probes. The practical default is to run as root.
- Readable layout metadata describing `svc_rqst`, `file`, `inode`, `super_block`
  and `dentry`. The EL8 fallback requires gcc, make and **matching** kernel-devel.
- `nfstop`: no runtime Go compiler, clang, libbpf library or module BTF dependency
  after its executable and layout data are supplied.
- `nfsdtop`: bpftrace >= 0.16 plus the same layout data. Kernel headers are not
  included in the generated bpftrace program; layout generation is separate.

The native collector no longer creates ring-buffer maps or fentry attachments,
and therefore does not impose those upstream nfstop feature requirements.

## Selection logic

1. If `--layout file.json` is supplied, validate and use it.
2. Try runtime BTF types and function prototypes. BTF may come from loaded
   `nfsd`/`sunrpc` modules or vmlinux; missing module BTF is not a universal failure.
3. Use `/var/cache/nfsmon/layout-<running-release>.json` if previously generated.
4. Report missing capabilities and the layout preparation command.

On EL8:

```bash
sudo dnf install -y gcc make "kernel-devel-$(uname -r)"
sudo ./tools/prepare-layout.sh
```

The helper compiles an unlinked object containing thirteen `offsetof` constants.
It never builds/loads a module or touches NIC drivers. The executable extracts
these constants directly from the ELF object; no helper executable is run to
guess memory layouts. The resulting JSON is written atomically.

Kernel release equality is checked **for layout data**, to prevent reading the
wrong memory offsets. It does not restrict the executable to that release:
generate a new JSON for a new EL8 kernel and keep using the same executable.

## Function profiles

The EL8 header fallback uses these NFSD VFS success-return signatures:

| Probe slot | Function | Request arg | File arg | Completed-byte pointer arg | Count width |
|---|---|---:|---:|---:|---:|
| Ordinary read | `nfsd_readv` | 1 | 3 (`struct file *`) | 7 | 8 bytes |
| Splice read | `nfsd_splice_read` | 1 | 3 (`struct file *`) | 5 | 8 bytes |
| Write | `nfsd_vfs_write` | 1 | 3 (`struct file *`) | 7 | 8 bytes |

For BTF-based adaptation the resolver discovers the request/file arguments by
type and the `count`/`cnt` pointer argument by name and unsigned integer type.
It accepts 4/8-byte completed counts, positions 1–8 and `struct nfsd_file *`
wrappers exposing `nf_file`. Ordinary reads may use `nfsd_iter_read` instead
of `nfsd_readv` **only when the same recognized status/count semantics apply**.
All three slots must be resolved; missing read paths are not silently omitted.

Signed byte-return functions, scalar requested-count arguments, asynchronous
completion paths, unfamiliar wrappers or different RPC transport markers need
a new explicit profile/collector change. A function with the same name alone
is not enough to claim support.

## Extend without rebuilding the UI

JSON has schema, kernel/architecture identity, field offsets and three hooks.
The Go types in `internal/layout/layout.go` define the complete schema. Each
hook supplies argument positions, count width and a wrapper offset, or
`4294967295` for a direct `struct file *`. With a reviewed compatible signature,
new offsets can be supplied using `--layout`; changing byte-return semantics
requires code changes, not simply a different JSON.

## NICs and interface filtering

CX3/`mlx4_ib`, CX4/`mlx5_ib`, later ConnectX cards and supported RDMA transports
all reach the same NFSD handling layer. This project does not use per-NIC vendor
counter APIs. A driver/firmware may affect whether NFS/RDMA works, but changing
it is not part of monitoring installation.

`-i` resolves that interface's current local IP addresses once at startup and
filters the server destination address of the RPC request. It does not identify
physical ingress with policy routing/bonding/shared addresses; restart after an
address change. IPv6 link-local scope is not encoded in the grouping key, so
use unique/global addresses when identical link-local addresses exist on
multiple interfaces.

Counters use bounded maps. `nfstop` maintains independent client totals so a
full per-file table does not invalidate client throughput. It reports map/read
failures in the UI. `nfsdtop` uses bpftrace's key limits and diagnostics; inspect
`kprobe_profile` for missed return probes under very high concurrency. No tool
can reconstruct events the kernel did not deliver.

Primary sources:

- [EL8/Rocky kernel source and development packages](https://download.rockylinux.org/pub/rocky/8/BaseOS/x86_64/)
- [Linux NFSD VFS source](https://github.com/torvalds/linux/blob/master/fs/nfsd/vfs.c)
- [Linux BTF documentation](https://docs.kernel.org/bpf/btf.html)
- [Linux kprobe documentation](https://docs.kernel.org/trace/kprobes.html)
