#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Temporary-prefix integration tests; no host packages or global files change.
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
trap 'printf "Lifecycle check failed at line %s\n" "$LINENO" >&2' ERR
source_dir="$work/release"
prefix="$work/user prefix"
mkdir -p "$source_dir"/{bin,tools,docs,licenses,examples}
cp "$base/tools/install.sh" "$base/tools/manage.sh" "$base/tools/lifecycle.sh" "$source_dir/tools/"
printf '#!/usr/bin/env bash\nexit 0\n' > "$source_dir/tools/prepare-layout.sh"
chmod +x "$source_dir/tools/prepare-layout.sh"
for file in LICENSE NOTICE README.md README.zh-CN.md CHANGELOG.md licenses/dependency.txt examples/nodes.json; do
    printf 'fixture\n' > "$source_dir/$file"
done
release() {
    printf '%s\n' "$1" > "$source_dir/VERSION"
    printf '#!/usr/bin/env bash\nprintf "norma %s fixture\\\\n"\n' "$1" > "$source_dir/bin/norma"
    chmod +x "$source_dir/bin/norma"
    printf '%s documentation\n' "$1" > "$source_dir/docs/guide.md"
}
expect_failure() {
    if "$@" > "$work/failure.log" 2>&1; then
        printf 'Expected command to fail: %s\n' "$*" >&2
        exit 1
    fi
}
release v0.5.0
bash "$source_dir/tools/install.sh" --prefix "$prefix" > /dev/null
"$prefix/bin/norma-manage" list > "$work/list"
grep -q 'Installed: v0.5.0' "$work/list"
[[ -s $prefix/share/norma/.install/manifest.tsv ]]
cp "$prefix/share/norma/.install/manifest.tsv" "$work/original-manifest.tsv"
release v0.5.1
printf 'new file\n' > "$source_dir/docs/new.md"
bash "$source_dir/tools/install.sh" --prefix "$prefix" > /dev/null
[[ $("$prefix/bin/norma" --version) == 'norma v0.5.1 fixture' ]]
snapshot=$(find "$prefix/share/norma/.install/snapshots" -maxdepth 1 -type d -name 'v0.5.0-*' -printf '%f\n')
[[ -n $snapshot ]]
cmp "$work/original-manifest.tsv" "$prefix/share/norma/.install/snapshots/$snapshot/manifest.tsv"
[[ -f $prefix/share/norma/.install/snapshots/$snapshot/payload/share/norma/examples/nodes.json ]]
"$prefix/bin/norma-manage" rollback "$snapshot" > /dev/null
[[ $("$prefix/bin/norma" --version) == 'norma v0.5.0 fixture' ]]
[[ $(cat "$prefix/share/norma/docs/guide.md") == 'v0.5.0 documentation' ]]
[[ ! -e $prefix/share/norma/docs/new.md ]]
restore=$(find "$prefix/share/norma/.install/snapshots" -maxdepth 1 -type d -name 'v0.5.1-*' -printf '%f\n')
[[ -n $restore ]]
# Refuse traversing or corrupt snapshot references, retaining the active binary.
expect_failure "$prefix/bin/norma-manage" rollback ../../outside
cp "$prefix/share/norma/.install/snapshots/$restore/payload/bin/norma" "$work/snapshot-binary"
printf 'corrupted\n' >> "$prefix/share/norma/.install/snapshots/$restore/payload/bin/norma"
expect_failure "$prefix/bin/norma-manage" rollback "$restore"
grep -q 'checksum mismatch' "$work/failure.log"
cp "$work/snapshot-binary" "$prefix/share/norma/.install/snapshots/$restore/payload/bin/norma"
# Valid-looking hashes never authorize arbitrary paths outside the payload.
cp "$prefix/share/norma/.install/manifest.tsv" "$work/good-manifest.tsv"
printf 'keep me\n' > "$work/important"
printf '%064d\t0644\t../../important\n' 0 >> "$prefix/share/norma/.install/manifest.tsv"
expect_failure "$prefix/bin/norma-manage" uninstall
[[ $(cat "$work/important") == 'keep me' && -x $prefix/bin/norma ]]
cp "$work/good-manifest.tsv" "$prefix/share/norma/.install/manifest.tsv"
# Snapshot and lock directories must never redirect writes outside the prefix.
mv "$prefix/share/norma/.install/snapshots" "$prefix/share/norma/.install/snapshots-original"
mkdir "$work/redirected-snapshots"
ln -s "$work/redirected-snapshots" "$prefix/share/norma/.install/snapshots"
expect_failure bash "$source_dir/tools/install.sh" --prefix "$prefix"
expect_failure "$prefix/bin/norma-manage" uninstall
[[ -z $(find "$work/redirected-snapshots" -mindepth 1 -print -quit) ]]
rm "$prefix/share/norma/.install/snapshots"
mv "$prefix/share/norma/.install/snapshots-original" "$prefix/share/norma/.install/snapshots"
# A link introduced in an installed directory must not escape the prefix.
mkdir "$work/outside"
printf 'keep me too\n' > "$work/outside/guide.md"
mv "$prefix/share/norma/docs" "$prefix/share/norma/docs-original"
ln -s "$work/outside" "$prefix/share/norma/docs"
expect_failure "$prefix/bin/norma-manage" uninstall
[[ $(cat "$work/outside/guide.md") == 'keep me too' ]]
rm "$prefix/share/norma/docs"
mv "$prefix/share/norma/docs-original" "$prefix/share/norma/docs"
"$prefix/bin/norma-manage" uninstall > /dev/null
[[ ! -e $prefix/bin/norma && ! -e $prefix/bin/norma-manage ]]
[[ -d $prefix/share/norma/.install/snapshots/$restore ]]
# Recovery remains possible after the installed manager has been removed.
bash "$source_dir/tools/manage.sh" --prefix "$prefix" rollback "$restore" > /dev/null
[[ $("$prefix/bin/norma" --version) == 'norma v0.5.1 fixture' ]]
printf 'user edits\n' >> "$prefix/share/norma/docs/guide.md"
# A snapshot made during uninstall can recover identical preserved local edits.
"$prefix/bin/norma-manage" uninstall > /dev/null
edited_snapshot=
for candidate in "$prefix/share/norma/.install/snapshots"/*; do
    if [[ $(cat "$candidate/reason") == before-uninstall ]] && grep -q 'user edits' "$candidate/payload/share/norma/docs/guide.md"; then
        edited_snapshot=${candidate##*/}
    fi
done
[[ -n $edited_snapshot ]]
# Fail the last metadata write after files+manifest have been copied.
mkdir "$work/fail-bin"
real_mv=$(command -v mv)
printf '#!/usr/bin/env bash\ncase "${@: -1}" in */.install/version) exit 1;; esac\nexec %q "$@"\n' "$real_mv" > "$work/fail-bin/mv"
chmod +x "$work/fail-bin/mv"
expect_failure env PATH="$work/fail-bin:$PATH" bash "$source_dir/tools/manage.sh" --prefix "$prefix" rollback "$edited_snapshot"
[[ ! -e $prefix/bin/norma && ! -e $prefix/share/norma/.install/manifest.tsv && ! -e $prefix/share/norma/.install/version ]]
grep -q 'user edits' "$prefix/share/norma/docs/guide.md"
bash "$source_dir/tools/manage.sh" --prefix "$prefix" rollback "$edited_snapshot" > /dev/null
[[ -x $prefix/bin/norma ]]
grep -q 'user edits' "$prefix/share/norma/docs/guide.md"
expect_failure env PATH="$work/fail-bin:$PATH" bash "$source_dir/tools/install.sh" --prefix "$work/failed-first-install"
[[ ! -e $work/failed-first-install/bin/norma && ! -e $work/failed-first-install/share/norma/.install/manifest.tsv && ! -e $work/failed-first-install/share/norma/.install/version ]]
bash "$source_dir/tools/install.sh" --prefix "$work/failed-first-install" > /dev/null
# An additional local edit remains preserved by a later purge uninstall.
printf 'another local edit\n' >> "$prefix/share/norma/docs/guide.md"
mkdir -p "$prefix/var/cache/norma"
printf 'custom cache\n' > "$prefix/var/cache/norma/layout-custom.json"
"$prefix/bin/norma-manage" uninstall --purge-data > "$work/uninstall"
[[ ! -e $prefix/bin/norma && ! -d $prefix/share/norma/.install/snapshots ]]
grep -q 'user edits' "$prefix/share/norma/docs/guide.md"
[[ -f $prefix/var/cache/norma/layout-custom.json && -f $work/important ]]
# Known v0.4 tar installs require explicit adoption and can be restored exactly.
legacy="$work/legacy"
mkdir -p "$legacy/bin" "$legacy/share/norma/tools" "$legacy/share/norma/docs"
printf '#!/usr/bin/env bash\nprintf "norma v0.4.0 legacy\\\\n"\n' > "$legacy/bin/norma"
chmod +x "$legacy/bin/norma"
cp "$source_dir/LICENSE" "$legacy/share/norma/LICENSE"
cp "$source_dir/tools/prepare-layout.sh" "$legacy/share/norma/tools/"
printf 'legacy docs\n' > "$legacy/share/norma/docs/guide.md"
expect_failure bash "$source_dir/tools/install.sh" --prefix "$legacy"
grep -q 'adopt-legacy' "$work/failure.log"
bash "$source_dir/tools/install.sh" --prefix "$legacy" --adopt-legacy > /dev/null
legacy_snapshot=$(find "$legacy/share/norma/.install/snapshots" -maxdepth 1 -type d -name 'legacy-*' -printf '%f\n')
"$legacy/bin/norma-manage" rollback "$legacy_snapshot" > /dev/null
[[ $("$legacy/bin/norma") == 'norma v0.4.0 legacy' && ! -e $legacy/bin/norma-manage ]]
[[ $(cat "$legacy/share/norma/docs/guide.md") == 'legacy docs' ]]
bash "$source_dir/tools/manage.sh" --prefix "$legacy" list > "$work/legacy-list"
grep -q 'Installed: legacy' "$work/legacy-list"
# Staged system-prefix purge has a strictly bounded cache scope.
stage="$work/stage"
DESTDIR="$stage" bash "$source_dir/tools/install.sh" --prefix /usr/local > /dev/null
mkdir -p "$stage/var/cache/norma"
printf 'layout\n' > "$stage/var/cache/norma/layout-example.json"
printf 'custom\n' > "$stage/var/cache/norma/custom.json"
DESTDIR="$stage" bash "$source_dir/tools/manage.sh" --prefix /usr/local uninstall > /dev/null
[[ -f $stage/var/cache/norma/layout-example.json ]]
staged_snapshot=$(find "$stage/usr/local/share/norma/.install/snapshots" -maxdepth 1 -type d -name 'v0.5.1-*' -printf '%f\n')
DESTDIR="$stage" bash "$source_dir/tools/manage.sh" --prefix /usr/local rollback "$staged_snapshot" > /dev/null
DESTDIR="$stage" bash "$source_dir/tools/manage.sh" --prefix /usr/local uninstall --purge-data > /dev/null
[[ ! -e $stage/var/cache/norma/layout-example.json && -f $stage/var/cache/norma/custom.json ]]
# A detected RPM package blocks the tar installer before it writes a payload.
mkdir "$work/mock-bin"
printf '#!/usr/bin/env bash\nexit 0\n' > "$work/mock-bin/rpm"
chmod +x "$work/mock-bin/rpm"
expect_failure env PATH="$work/mock-bin:$PATH" bash "$source_dir/tools/install.sh" --prefix "$work/rpm-conflict"
grep -q 'RPM installation owns norma' "$work/failure.log"
[[ ! -e $work/rpm-conflict ]]
printf 'Lifecycle checks passed: full upgrade/rollback, recovery after uninstall, legacy adoption, manifests, modified files, bounded purge, symlinks and RPM conflict.\n'
