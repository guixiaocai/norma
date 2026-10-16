#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
norma_version=${VERSION:-v0.5.0}
norma_release=${RPM_RELEASE:-1}
norma_dist=${DIST_DIR:-$base/dist}
[[ $norma_version =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]] || { echo 'Invalid VERSION for RPM.' >&2; exit 1; }
[[ $norma_release =~ ^[1-9][0-9]*$ ]] || { echo 'RPM_RELEASE must be a positive integer.' >&2; exit 1; }
command -v rpmbuild >/dev/null || { echo 'Install rpm-build on Rocky/Alma/RHEL (or rpm on a Debian build host), then run make rpm.' >&2; exit 1; }
[[ -x $base/bin/norma ]] || { echo 'Build bin/norma first.' >&2; exit 1; }
norma_binary_version=$("$base/bin/norma" --version | awk 'NR == 1 { print $2 }')
[[ $norma_binary_version == "$norma_version" ]] || { echo "Binary is $norma_binary_version; build VERSION=$norma_version before packaging." >&2; exit 1; }
norma_rpm_version=${norma_version#v}
norma_rpm_version=${norma_rpm_version/-/~}
norma_work=$(mktemp -d)
trap 'rm -rf -- "$norma_work"' EXIT
mkdir -p "$norma_work"/{BUILD,BUILDROOT,RPMS,SOURCES,SPECS,SRPMS,tmp,rpmdb}
norma_source="$norma_work/norma-$norma_rpm_version"
mkdir -p "$norma_source/bin" "$norma_source/tools"
cp "$base/bin/norma" "$norma_source/bin/"
cp "$base/tools/prepare-layout.sh" "$norma_source/tools/"
cp "$base/README.md" "$base/README.zh-CN.md" "$base/LICENSE" "$base/NOTICE" "$base/CHANGELOG.md" "$norma_source/"
cp -R "$base/docs" "$base/licenses" "$norma_source/"
if [[ -d $base/examples ]]; then cp -R "$base/examples" "$norma_source/"; fi
printf '%s\n' "$norma_version" > "$norma_source/VERSION"
tar -czf "$norma_work/SOURCES/norma-$norma_rpm_version.tar.gz" -C "$norma_work" "norma-$norma_rpm_version"
rpmbuild -bb --target x86_64 \
    --define "_topdir $norma_work" \
    --define "_tmppath $norma_work/tmp" \
    --define "_dbpath $norma_work/rpmdb" \
    --define "norma_version $norma_rpm_version" \
    --define "norma_release $norma_release" \
    --define "dist .el8" "$base/packaging/norma.spec"
mkdir -p "$norma_dist"
cp "$norma_work/RPMS/x86_64/norma-$norma_rpm_version-$norma_release.el8.x86_64.rpm" "$norma_dist/"
(
    cd "$norma_dist"
    norma_assets=()
    for norma_asset in "norma-$norma_version-linux-amd64.tar.gz" "norma-$norma_version-linux-amd64.zip" norma-"$norma_rpm_version"-*.el8.x86_64.rpm; do
        if [[ -f $norma_asset ]]; then norma_assets+=("$norma_asset"); fi
    done
    sha256sum "${norma_assets[@]}" > SHA256SUMS
)
printf 'Created %s/norma-%s-%s.el8.x86_64.rpm\n' "$norma_dist" "$norma_rpm_version" "$norma_release"
