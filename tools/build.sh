#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$base"
command -v go >/dev/null || { echo 'Install Go >=1.25.3.' >&2; exit 1; }
mkdir -p bin
nfsmon_version=${VERSION:-v0.4.0}
nfsmon_commit=${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo source)}
nfsmon_flags="-s -w -X nfsmon/internal/app.Version=$nfsmon_version -X nfsmon/internal/app.Commit=$nfsmon_commit"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -trimpath -ldflags "$nfsmon_flags" -o bin/norma ./cmd/norma
# Remove the previous generated entrypoints after a successful build.
rm -f -- bin/nfstop bin/nfsdtop
echo 'Built bin/norma (Linux x86_64, native eBPF and bpftrace backends).'
