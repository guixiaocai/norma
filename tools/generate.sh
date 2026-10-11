#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$base"
command -v go >/dev/null || { echo 'Install Go >=1.25.3.' >&2; exit 1; }
command -v clang >/dev/null || { echo 'Install clang/LLVM and libbpf development headers.' >&2; exit 1; }
# Include the system's multiarch asm headers when present.
if command -v gcc >/dev/null; then
    norma_sysinc="/usr/include/$(gcc -dumpmachine)"
    if [[ -d $norma_sysinc ]]; then export BPF2GO_CFLAGS="${BPF2GO_CFLAGS:-} -I$norma_sysinc"; fi
fi
go generate ./internal/native
