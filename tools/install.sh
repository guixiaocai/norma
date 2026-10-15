#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
norma_prefix=${PREFIX:-/usr/local}
norma_dest=${DESTDIR:-}
norma_prepare=false
fail() { printf 'norma installer: %s\n' "$*" >&2; exit 1; }
usage() {
    cat <<'EOF'
Install norma from an extracted release archive or a local build (offline).

Usage: ./tools/install.sh [--prefix /usr/local] [--prepare-layout]

  --prefix PATH      Absolute prefix (default: PREFIX or /usr/local)
  --prepare-layout   Run the EL8 helper using already installed gcc, make and
                     development headers matching the running kernel
  -h, --help         Show this help

Use --prefix "$HOME/.local" for an unprivileged installation. DESTDIR is supported
for staging, but cannot be combined with --prepare-layout. No dependencies are
installed and no services, exports, mounts or kernel settings are changed.
EOF
}
while (($#)); do
    case "$1" in
        --prefix)
            (($# >= 2)) && [[ -n $2 && $2 != --* ]] || fail '--prefix requires a value.'
            norma_prefix=$2; shift 2 ;;
        --prepare-layout) norma_prepare=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) fail "Unknown option: $1. See --help." ;;
    esac
done
[[ $norma_prefix == /* ]] || fail '--prefix must be an absolute path.'
[[ -z $norma_dest || $norma_dest == /* ]] || fail 'DESTDIR must be an absolute path.'
if $norma_prepare && [[ -n $norma_dest ]]; then fail '--prepare-layout cannot be used with DESTDIR staging.'; fi
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || fail 'This release supports Linux x86_64 only.'
[[ -f $base/bin/norma && -x $base/bin/norma ]] || fail 'Missing bin/norma. Extract a release archive, or run make first.'
for norma_file in LICENSE NOTICE README.md README.zh-CN.md CHANGELOG.md tools/prepare-layout.sh; do
    [[ -f $base/$norma_file ]] || fail "Missing release file: $norma_file."
done
[[ -d $base/licenses ]] || fail 'Missing release licenses directory.'
[[ -d $base/docs ]] || fail 'Missing release documentation directory.'
norma_target="$norma_dest$norma_prefix"
if ! install -d "$norma_target/bin" "$norma_target/share/norma/tools" "$norma_target/share/norma/licenses" "$norma_target/share/norma/docs"; then
    fail "Cannot write $norma_target. Run with sudo, or use --prefix \"\$HOME/.local\"."
fi
# Replace atomically so an in-progress monitor can keep its old executable open.
norma_new_binary=$(mktemp "$norma_target/bin/.norma-install.XXXXXX")
trap 'rm -f -- "$norma_new_binary"' EXIT
install -m 0755 "$base/bin/norma" "$norma_new_binary"
mv -fT -- "$norma_new_binary" "$norma_target/bin/norma"
install -m 0755 "$base/tools/prepare-layout.sh" "$norma_target/share/norma/tools/prepare-layout.sh"
install -m 0644 "$base/LICENSE" "$base/NOTICE" "$base/README.md" "$base/README.zh-CN.md" "$base/CHANGELOG.md" "$norma_target/share/norma/"
cp -R "$base/licenses/." "$norma_target/share/norma/licenses/"
cp -R "$base/docs/." "$norma_target/share/norma/docs/"
printf '\nInstalled norma into %s.\n' "$norma_target"
if $norma_prepare; then
    printf 'Preparing the EL8 layout using the running kernel headers...\n'
    if ! NORMA_BIN="$norma_target/bin/norma" "$norma_target/share/norma/tools/prepare-layout.sh"; then
        printf 'norma is installed, but layout preparation failed. Run the doctor below for next steps.\n' >&2
        printf '  sudo %q --doctor\n' "$norma_target/bin/norma" >&2
        exit 1
    fi
fi
if [[ -n $norma_dest ]]; then
    printf 'Staged installation only; run these on the target host:\n'
else
    printf '\nNext steps:\n'
fi
printf '  %q --demo          # preview the dashboard without root or NFS traffic\n' "$norma_prefix/bin/norma"
printf '  sudo %q --doctor   # check this host and show any required setup\n' "$norma_prefix/bin/norma"
printf '  sudo %q            # monitor this NFS server\n' "$norma_prefix/bin/norma"
case ":$PATH:" in
    *":$norma_prefix/bin:"*) ;;
    *) printf '\nOptional PATH setup: export PATH=%q/bin:"$PATH"\n' "$norma_prefix" ;;
esac
