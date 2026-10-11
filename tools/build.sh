#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$base"
command -v go >/dev/null || { echo 'Install Go >=1.25.3.' >&2; exit 1; }
command -v clang >/dev/null || { echo 'Install clang/LLVM and libbpf development headers.' >&2; exit 1; }
# Debian/Ubuntu place asm/types.h in a multiarch include directory. EL8 uses
# /usr/include directly. Neither path contains runtime kernel structure layouts.
if command -v gcc >/dev/null; then
    nfsmon_sysinc="/usr/include/$(gcc -dumpmachine)"
    if [[ -d $nfsmon_sysinc ]]; then export BPF2GO_CFLAGS="${BPF2GO_CFLAGS:-} -I$nfsmon_sysinc"; fi
fi
go generate ./internal/native
mkdir -p bin
nfsmon_version=${VERSION:-v0.2.0}
nfsmon_commit=${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo source)}
nfsmon_flags="-s -w -X nfsmon/internal/app.Version=$nfsmon_version -X nfsmon/internal/app.Commit=$nfsmon_commit"
for nfsmon_name in nfstop nfsdtop; do
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -trimpath -ldflags "$nfsmon_flags" -o "bin/$nfsmon_name" "./cmd/$nfsmon_name"
done
echo 'Built bin/nfstop and bin/nfsdtop (Linux x86_64).'
