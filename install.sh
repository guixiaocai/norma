#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Download a published binary; no compiler, package manager or service changes.
set -euo pipefail

usage() {
    cat <<'EOF'
Install norma for Linux x86_64 from an official GitHub release.

Usage: bash install.sh [--version v0.4.0] [--prefix /usr/local] [--prepare-layout]

  --version VERSION  Release tag, or "latest" (default: latest stable release)
  --prefix PATH      Absolute installation prefix (default: PREFIX or /usr/local)
  --prepare-layout   Explicitly run the EL8 header layout helper after installation
  -h, --help         Show this help

Requires curl or wget, tar and sha256sum. Downloads are HTTPS-only and the
release archive is checked against its published SHA256SUMS before extraction.
Use --prefix "$HOME/.local" to install without root. Monitoring still requires
root/BPF permissions. No dependencies are installed and no services are changed.
For offline hosts, extract a verified release archive and run tools/install.sh.
EOF
}
fail() { printf 'norma installer: %s\n' "$*" >&2; exit 1; }
norma_version=latest
norma_prefix=${PREFIX:-/usr/local}
norma_prepare=false
while (($#)); do
    case "$1" in
        --version|--prefix)
            (($# >= 2)) && [[ -n $2 && $2 != --* ]] || fail "$1 requires a value."
            if [[ $1 == --version ]]; then norma_version=$2; else norma_prefix=$2; fi
            shift 2 ;;
        --prepare-layout) norma_prepare=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) fail "Unknown option: $1. See --help." ;;
    esac
done
[[ $norma_prefix == /* ]] || fail '--prefix must be an absolute path.'
if $norma_prepare && [[ -n ${DESTDIR:-} ]]; then fail '--prepare-layout cannot be used with DESTDIR staging.'; fi
[[ $(uname -s) == Linux ]] || fail 'This release supports Linux only.'
[[ $(uname -m) == x86_64 ]] || fail 'This release provides Linux x86_64 (amd64) binaries only.'
for norma_tool in tar sha256sum awk sed mktemp; do
    command -v "$norma_tool" >/dev/null || fail "Required command not found: $norma_tool."
done
if command -v curl >/dev/null; then
    download() {
        curl --fail --location --silent --show-error --proto '=https' \
            --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 180 \
            --retry 2 --output "$2" "$1"
    }
elif command -v wget >/dev/null; then
    download() { wget --https-only --timeout=30 --tries=3 --quiet --output-document="$2" "$1"; }
else
    fail 'Install curl or wget, or use a release archive with tools/install.sh offline.'
fi

norma_tmp=$(mktemp -d)
trap 'rm -rf -- "$norma_tmp"' EXIT
norma_repository=https://github.com/guixiaocai/norma
if [[ $norma_version == latest ]]; then
    printf 'Finding the latest norma release...\n'
    download 'https://api.github.com/repos/guixiaocai/norma/releases/latest' "$norma_tmp/release.json" ||
        fail 'Cannot find the latest release. Check network access or specify --version v0.4.0.'
    norma_version=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$norma_tmp/release.json")
fi
[[ $norma_version =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][a-zA-Z0-9._-]+)?$ ]] ||
    fail 'Invalid release tag. Expected v0.4.0 or latest.'
norma_name="norma-$norma_version-linux-amd64"
norma_asset="$norma_name.tar.gz"
norma_url="$norma_repository/releases/download/$norma_version"
printf 'Downloading norma %s (Linux x86_64)...\n' "$norma_version"
download "$norma_url/$norma_asset" "$norma_tmp/$norma_asset" ||
    fail "Cannot download $norma_asset. Check the release tag and network access."
download "$norma_url/SHA256SUMS" "$norma_tmp/SHA256SUMS" || fail 'Cannot download SHA256SUMS.'
norma_hash=$(awk -v asset="$norma_asset" '$2 == asset || $2 == "*" asset { print $1 }' "$norma_tmp/SHA256SUMS")
[[ $norma_hash =~ ^[a-fA-F0-9]{64}$ ]] || fail "Missing or ambiguous SHA256 checksum for $norma_asset."
if ! (cd "$norma_tmp" && printf '%s  %s\n' "$norma_hash" "$norma_asset" | sha256sum --check --status); then
    fail 'SHA256 verification failed; nothing was installed. Download the release again.'
fi
printf 'SHA256 verified.\n'
# Refuse unexpected paths before extracting, including paths containing "..".
tar -tzf "$norma_tmp/$norma_asset" > "$norma_tmp/archive.list" || fail 'Invalid release archive.'
awk -v root="$norma_name" '
    $0 != root && index($0, root "/") != 1 { bad = 1 }
    /(^|\/)\.\.(\/|$)/ { bad = 1 }
    END { exit bad }
' "$norma_tmp/archive.list" || fail 'Unexpected paths in the release archive.'
tar -tvzf "$norma_tmp/$norma_asset" > "$norma_tmp/archive.types"
awk 'substr($0, 1, 1) != "-" && substr($0, 1, 1) != "d" { bad = 1 } END { exit bad }' \
    "$norma_tmp/archive.types" || fail 'Release archive contains unsupported links or special files.'
tar --extract --gzip --file "$norma_tmp/$norma_asset" --directory "$norma_tmp" \
    --no-same-owner --no-same-permissions
[[ -f $norma_tmp/$norma_name/tools/install.sh ]] || fail 'Release archive is missing tools/install.sh.'
# PREFIX also works with older release installers that predate CLI flags.
PREFIX="$norma_prefix" bash "$norma_tmp/$norma_name/tools/install.sh"
if $norma_prepare; then
    printf 'Preparing the EL8 layout using the running kernel headers...\n'
    if ! NORMA_BIN="$norma_prefix/bin/norma" "$norma_prefix/share/norma/tools/prepare-layout.sh"; then
        fail 'norma is installed, but layout preparation failed. Check the helper diagnostics above.'
    fi
fi
