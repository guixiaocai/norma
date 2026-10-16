#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$base"
nfsmon_version=${VERSION:-v0.5.0}
nfsmon_dist=${DIST_DIR:-$base/dist}
nfsmon_name="norma-${nfsmon_version}-linux-amd64"
case "$nfsmon_version" in *[!a-zA-Z0-9._-]*) echo 'Invalid release version.' >&2; exit 1;; esac
nfsmon_binary_version=$(bin/norma --version | awk 'NR == 1 { print $2 }')
[[ $nfsmon_binary_version == "$nfsmon_version" ]] || { echo "Binary is $nfsmon_binary_version; build VERSION=$nfsmon_version before packaging." >&2; exit 1; }
nfsmon_tmp=$(mktemp -d)
trap 'rm -rf -- "$nfsmon_tmp"' EXIT
mkdir -p "$nfsmon_tmp/$nfsmon_name/bin" "$nfsmon_tmp/$nfsmon_name/tools"
cp bin/norma "$nfsmon_tmp/$nfsmon_name/bin/"
cp README.md README.zh-CN.md LICENSE NOTICE CHANGELOG.md "$nfsmon_tmp/$nfsmon_name/"
printf '%s\n' "$nfsmon_version" > "$nfsmon_tmp/$nfsmon_name/VERSION"
cp -a docs licenses "$nfsmon_tmp/$nfsmon_name/"
if [[ -d examples ]]; then cp -a examples "$nfsmon_tmp/$nfsmon_name/"; fi
# Keep test/build/screenshot scripts out of release archives.
cp tools/install.sh tools/prepare-layout.sh tools/manage.sh tools/lifecycle.sh tools/bundle-offline.sh "$nfsmon_tmp/$nfsmon_name/tools/"
mkdir -p "$nfsmon_dist"
nfsmon_dist=$(cd "$nfsmon_dist" && pwd)
tar -czf "$nfsmon_dist/$nfsmon_name.tar.gz" -C "$nfsmon_tmp" "$nfsmon_name"
(cd "$nfsmon_tmp" && zip -qr "$nfsmon_dist/$nfsmon_name.zip" "$nfsmon_name")
(
    cd "$nfsmon_dist"
    nfsmon_rpm_version=${nfsmon_version#v}
    nfsmon_rpm_version=${nfsmon_rpm_version/-/~}
    nfsmon_assets=("$nfsmon_name.tar.gz" "$nfsmon_name.zip")
    for nfsmon_asset in norma-"$nfsmon_rpm_version"-*.el8.x86_64.rpm; do
        if [[ -f $nfsmon_asset ]]; then nfsmon_assets+=("$nfsmon_asset"); fi
    done
    sha256sum "${nfsmon_assets[@]}" > SHA256SUMS
)
echo "Created $nfsmon_dist/$nfsmon_name.tar.gz and .zip."
