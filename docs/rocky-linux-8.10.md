# Rocky Linux 8.10 deployment

[简体中文](rocky-linux-8.10.zh-CN.md) | [README](../README.md)

Rocky Linux 8.10 on **x86_64** is the primary deployment target. Install on the
machine serving NFS exports. A client machine cannot use these NFSD probes to
measure a remote server. The prebuilt native collector needs no Go, clang,
bpftrace or libbpf shared library on the target host.

Kernel capabilities, function semantics and layout metadata still determine
compatibility. This guide does not claim runtime validation on every Rocky
kernel or NIC. See the [compatibility contract](compatibility.md).

## 1. Install and preview

```bash
curl -fsSL https://raw.githubusercontent.com/guixiaocai/norma/main/install.sh -o install-norma.sh
sudo bash install-norma.sh
norma --version
norma --demo
```

The installer selects the latest stable release, downloads its Linux amd64
archive and `SHA256SUMS`, verifies the checksum, then installs into `/usr/local`.
It supports `curl` or `wget`; use `wget -O install-norma.sh URL` if curl is absent.
Read `bash install-norma.sh --help` for options. To pin a version:

```bash
sudo bash install-norma.sh --version v0.4.0
```

`--demo` uses deterministic synthetic data and the real dashboard. It does not
load BPF, require root or need a running NFS server. Press `q` to exit.

![Synthetic dashboard preview](images/dashboard.png)

*Synthetic demo data, not production measurements.*

## 2. Check the running server

```bash
sudo norma --doctor
```

This is a read-only prerequisite check. It reports environment and layout
readiness and exits nonzero when a required check fails. It does not load a BPF
program, attach probes, alter NFS exports, install packages or start services.
Passing it does not prove that kernel security policy will permit attachment
or that live workload accounting is correct.

Checks include OS/architecture, running kernel, active NFSD worker threads,
layout resolution from BTF/cache/explicit file, required NFSD symbols when
`/proc/kallsyms` is readable, and bpftrace version when that backend is selected.
Root status and visible kernel lockdown settings are advisory. Run it under
sudo for a view closer to live collection; a nonroot warning alone does not
cause failure. Doctor uses a plain checklist and cannot be combined with
`--json` or `--demo`.

The NFS server must already be configured and serving exports. If runtime BTF
can resolve the supported NFSD functions and fields, proceed to collection. If
the report identifies missing layout metadata, prepare the constrained EL8
fallback below.

## 3. Prepare EL8 layout metadata when needed

For a supported `4.18.0-*.el8*` kernel without usable BTF, install development
headers that **exactly match the running kernel**, plus gcc and make:

```bash
sudo dnf install -y gcc make "kernel-devel-$(uname -r)"
sudo /usr/local/share/norma/tools/prepare-layout.sh
sudo norma --doctor
```

The helper compiles field-offset constants into an unlinked object, then writes
`/var/cache/norma/layout-<running-kernel-release>.json`. It does not build or
load a kernel module, change NIC drivers or rebuild norma. It uses a fixed,
supported NFSD function profile; it is not a general generator for arbitrary
kernels. It rejects mismatched headers and kernels outside its EL8 profile.

Once these packages are available, installation and layout preparation can also
be combined explicitly:

```bash
sudo bash install-norma.sh --version v0.4.0 --prepare-layout
# Or from an unpacked release:
sudo ./tools/install.sh --prepare-layout
```

The installer does not install those system packages automatically. If the
matching `kernel-devel` package is unavailable in the configured repositories,
obtain the exact matching package through your normal Rocky repository process,
or update and reboot into an approved kernel for which matching headers exist.
Do not substitute another kernel's headers. Rerun the helper after a kernel
change if the new kernel still needs this fallback.

For a reviewed layout stored elsewhere, pass it explicitly:

```bash
sudo norma --doctor --layout /path/to/layout.json
sudo norma --layout /path/to/layout.json
```

## 4. Collect live NFS I/O

```bash
sudo norma                         # all supported NFS transports
sudo norma -p tcp                  # TCP only
sudo norma -p rdma                 # RDMA only
sudo norma -i eno2                 # TCP, filtered by eno2's local addresses
sudo norma -p rdma -i ib0           # RDMA, filtered by ib0's local addresses
sudo norma --no-color              # monochrome dashboard
sudo norma -t -s 60                # one minute of plain text
sudo norma --json -s 60            # one minute of JSON Lines
```

Default transport changed from RDMA to `all` in v0.4.0. Add `-p rdma` to preserve
an existing RDMA-only workflow. `-i` alone continues to select TCP; an explicit
`-p` takes precedence. An interface filter uses local IP addresses, not physical
packet ingress. Ordinary TCP NFS over IPoIB is still TCP, not NFS/RDMA.

Native is the default backend. An existing bpftrace environment can use
`sudo norma --backend bpftrace`; it needs bpftrace >= 0.16 and the same compatible
layout and probe semantics. There is no automatic backend fallback.

## Offline or user-owned installation

On a connected machine, download `norma-v0.4.0-linux-amd64.tar.gz` and
`SHA256SUMS` from the **same** [release](https://github.com/guixiaocai/norma/releases/tag/v0.4.0).
Transfer both to the server and run:

```bash
sha256sum --ignore-missing -c SHA256SUMS
tar -xzf norma-v0.4.0-linux-amd64.tar.gz
cd norma-v0.4.0-linux-amd64
sudo ./tools/install.sh
sudo norma --doctor
```

Continue only if the downloaded archive reports `OK`. The local installer has
no network dependency; offline layout preparation additionally needs gcc,
make and matching kernel-devel packages supplied through your normal package
distribution process.

For installation without writing system directories:

```bash
bash install-norma.sh --version v0.4.0 --prefix "$HOME/.local"
# Local archive alternative: ./tools/install.sh --prefix "$HOME/.local"
export PATH="$HOME/.local/bin:$PATH"
norma --demo
sudo "$HOME/.local/bin/norma" --doctor
sudo "$HOME/.local/bin/norma"
```

Installation and demo mode need no root with this prefix; live monitoring still
needs BPF/probe permissions. Use the full path under sudo because its PATH may
not include your personal bin directory. For layout preparation with this
prefix, explicitly supply the executable path:

```bash
sudo env NORMA_BIN="$HOME/.local/bin/norma" "$HOME/.local/share/norma/tools/prepare-layout.sh"
```

## Troubleshooting

| Symptom | Next step |
|---|---|
| `norma: command not found` | Check the prefix; try `/usr/local/bin/norma`, or use the explicit user-prefix path. |
| Missing layout/BTF | Install matching kernel-devel, gcc and make; run the EL8 helper, then `--doctor` again. |
| Header version mismatch | Compare `uname -r` with installed kernel-devel; do not reuse another kernel's layout. |
| NFSD unavailable | Run on the NFS server and verify its existing NFS service/exports before collecting. |
| BPF/probe permission failure | Run with the required privileges and inspect the host's tracing/security policy; `--doctor` does not test a live attachment. |
| No rows | Generate real client reads/writes and check transport/address filters. Client-cache hits that issue no RPC do not appear. |
| Crowded terminal | Enlarge the terminal for all columns; use `--no-color`, `-t` or `--json` as appropriate. |
| Unknown NFSD signature | Use supported runtime BTF or a reviewed compatible layout; switching backend does not change the shared semantics requirement. |

The UI reports logical NFSD completion bytes, including server-cache reads;
it does not report wire traffic or physical disk throughput. See
[metrics](metrics.md) and [architecture](architecture.md) for boundaries and
collection limits.
