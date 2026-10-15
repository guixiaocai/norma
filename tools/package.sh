#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$base"
nfsmon_version=${VERSION:-v0.4.0}
nfsmon_name="norma-${nfsmon_version}-linux-amd64"
case "$nfsmon_version" in *[!a-zA-Z0-9._-]*) echo 'Invalid release version.' >&2; exit 1;; esac
nfsmon_tmp=$(mktemp -d)
trap 'rm -rf -- "$nfsmon_tmp"' EXIT
mkdir -p "$nfsmon_tmp/$nfsmon_name/bin" "$nfsmon_tmp/$nfsmon_name/tools"
cp bin/norma "$nfsmon_tmp/$nfsmon_name/bin/"
cp README.md README.zh-CN.md LICENSE NOTICE CHANGELOG.md "$nfsmon_tmp/$nfsmon_name/"
cp -a docs licenses "$nfsmon_tmp/$nfsmon_name/"
# Keep test/build/screenshot scripts out of release archives.
cp tools/install.sh tools/prepare-layout.sh "$nfsmon_tmp/$nfsmon_name/tools/"
mkdir -p dist
tar -czf "dist/$nfsmon_name.tar.gz" -C "$nfsmon_tmp" "$nfsmon_name"
(cd "$nfsmon_tmp" && zip -qr "$base/dist/$nfsmon_name.zip" "$nfsmon_name")
(cd dist && sha256sum "$nfsmon_name.tar.gz" "$nfsmon_name.zip" > SHA256SUMS)
echo "Created dist/$nfsmon_name.tar.gz and .zip."
