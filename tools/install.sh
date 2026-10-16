#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=tools/lifecycle.sh
source "$base/tools/lifecycle.sh"
norma_prefix=${PREFIX:-/usr/local}
norma_prepare=false
norma_adopt=false
usage() {
    cat <<'EOF'
Install norma from an extracted release archive or a local build (offline).

Usage: ./tools/install.sh [--prefix PATH] [--prepare-layout] [--adopt-legacy]

  --prefix PATH      Absolute prefix (default: PREFIX or /usr/local)
  --prepare-layout   Explicitly run the EL8 helper with existing matching headers
  --adopt-legacy     Back up/import an older norma tar install without a manifest
  -h, --help         Show this help

Upgrades save a full installation snapshot. Installed norma-manage supports
list, rollback SNAPSHOT_ID and uninstall [--purge-data]. Layout cache and
snapshots are preserved by default. Existing RPM installations are not modified.
Use --prefix "$HOME/.local" for a user install. DESTDIR supports package staging;
it cannot be combined with --prepare-layout. No packages/services are changed.
EOF
}
while (($#)); do
    case "$1" in
        --prefix)
            (($# >= 2)) && [[ -n $2 && $2 != --* ]] || { norma_fail '--prefix requires a value.'; exit 1; }
            norma_prefix=$2; shift 2 ;;
        --prepare-layout) norma_prepare=true; shift ;;
        --adopt-legacy) norma_adopt=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) norma_fail "Unknown option: $1. See --help."; exit 1 ;;
    esac
done
norma_init_paths
norma_check_rpm
if $norma_prepare && [[ -n $norma_dest ]]; then norma_fail '--prepare-layout cannot be used with DESTDIR staging.'; exit 1; fi
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || { norma_fail 'This release supports Linux x86_64 only.'; exit 1; }
[[ -f $base/bin/norma && -x $base/bin/norma && ! -L $base/bin/norma ]] || { norma_fail 'Missing bin/norma. Extract a release archive, or run make first.'; exit 1; }
for norma_file in LICENSE NOTICE README.md README.zh-CN.md CHANGELOG.md tools/prepare-layout.sh tools/manage.sh tools/lifecycle.sh; do
    [[ -f $base/$norma_file && ! -L $base/$norma_file ]] || { norma_fail "Missing regular release file: $norma_file."; exit 1; }
done
for norma_directory in docs licenses; do
    [[ -d $base/$norma_directory && ! -L $base/$norma_directory ]] || { norma_fail "Missing release directory: $norma_directory."; exit 1; }
done
if [[ -f $base/VERSION ]]; then
    norma_version=$(cat "$base/VERSION")
else
    norma_version=$("$base/bin/norma" --version | awk 'NR == 1 { print $2 }')
fi
[[ $norma_version =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][a-zA-Z0-9._-]+)?$ ]] || { norma_fail 'Cannot identify release version from VERSION or bin/norma --version.'; exit 1; }
norma_work=$(mktemp -d)
cleanup() { rm -rf -- "$norma_work"; norma_unlock; }
trap cleanup EXIT
trap 'exit 130' INT TERM
norma_payload="$norma_work/payload"
install -d "$norma_payload/bin" "$norma_payload/share/norma/tools"
install -m 0755 "$base/bin/norma" "$norma_payload/bin/norma"
install -m 0755 "$base/tools/prepare-layout.sh" "$base/tools/manage.sh" "$base/tools/lifecycle.sh" "$norma_payload/share/norma/tools/"
install -m 0644 "$base/LICENSE" "$base/NOTICE" "$base/README.md" "$base/README.zh-CN.md" "$base/CHANGELOG.md" "$norma_payload/share/norma/"
printf '%s\n' "$norma_version" > "$norma_payload/share/norma/VERSION"
for norma_directory in docs licenses examples; do
    if [[ -d $base/$norma_directory ]]; then
        [[ ! -L $base/$norma_directory && -z $(find "$base/$norma_directory" -type l -print -quit) ]] || { norma_fail "Release directory contains symbolic links: $norma_directory"; exit 1; }
        cp -R "$base/$norma_directory" "$norma_payload/share/norma/"
    fi
done
cat > "$norma_payload/bin/norma-manage" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
norma_installed_prefix=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
unset DESTDIR
exec bash "$norma_installed_prefix/share/norma/tools/manage.sh" --prefix "$norma_installed_prefix" "$@"
EOF
chmod 0755 "$norma_payload/bin/norma-manage"
norma_make_manifest "$norma_payload" "$norma_work/new-manifest.tsv"
norma_lock
norma_previous="$norma_work/absent-manifest"
norma_old_snapshot=
if [[ -f $norma_manifest ]]; then
    norma_validate_manifest "$norma_manifest"
    cp "$norma_manifest" "$norma_work/previous-manifest.tsv"
    norma_previous="$norma_work/previous-manifest.tsv"
elif $norma_adopt; then
    [[ -x $norma_target/bin/norma && -f $norma_target/share/norma/LICENSE && -f $norma_target/share/norma/tools/prepare-layout.sh ]] || {
        norma_fail '--adopt-legacy requires an existing norma binary, LICENSE and prepare-layout.sh in this prefix.'; exit 1;
    }
    norma_assert_path "$norma_target/bin/norma"
    install -d "$norma_work/legacy/bin" "$norma_work/legacy/share/norma/tools"
    cp "$norma_target/bin/norma" "$norma_work/legacy/bin/"
    for norma_file in LICENSE NOTICE README.md README.zh-CN.md CHANGELOG.md tools/prepare-layout.sh; do
        if [[ -f $norma_target/share/norma/$norma_file ]]; then
            norma_assert_path "$norma_target/share/norma/$norma_file"
            cp "$norma_target/share/norma/$norma_file" "$norma_work/legacy/share/norma/$norma_file"
        fi
    done
    for norma_directory in docs licenses; do
        if [[ -d $norma_target/share/norma/$norma_directory ]]; then
            norma_assert_path "$norma_target/share/norma/$norma_directory"
            [[ -z $(find "$norma_target/share/norma/$norma_directory" -type l -print -quit) ]] || { norma_fail 'Legacy files contain symbolic links; move them aside before adoption.'; exit 1; }
            cp -R "$norma_target/share/norma/$norma_directory" "$norma_work/legacy/share/norma/"
        fi
    done
    norma_make_manifest "$norma_work/legacy" "$norma_work/previous-manifest.tsv"
    norma_previous="$norma_work/previous-manifest.tsv"
fi
norma_check_conflicts "$norma_work/new-manifest.tsv" "$norma_previous"
if [[ -f $norma_previous ]]; then
    if [[ -f $norma_manifest ]]; then norma_snapshot upgrade "$norma_previous"; else norma_snapshot adopted-legacy "$norma_previous" legacy; fi
    norma_old_snapshot="$norma_state/snapshots/$norma_snapshot_id"
fi
if ! norma_activate "$norma_payload" "$norma_work/new-manifest.tsv" "$norma_version" "$norma_previous"; then
    if [[ -n $norma_old_snapshot ]]; then
        norma_activate "$norma_old_snapshot/payload" "$norma_old_snapshot/manifest.tsv" "$(cat "$norma_old_snapshot/version")" "$norma_work/new-manifest.tsv" ||
            printf 'Automatic restoration failed; restore snapshot %s with tools/manage.sh.\n' "$norma_snapshot_id" >&2
    else
        norma_remove_manifest "$norma_work/new-manifest.tsv" || true
        rm -f -- "$norma_manifest" "$norma_state/version"
        norma_cleanup_empty_dirs "$norma_work/new-manifest.tsv"
    fi
    norma_fail 'Installation failed; see the diagnostics above.'; exit 1
fi
printf '\nInstalled norma %s into %s.\nManifest: %s\n' "$norma_version" "$norma_target" "$norma_manifest"
if $norma_prepare; then
    if ! NORMA_BIN="$norma_target/bin/norma" "$norma_target/share/norma/tools/prepare-layout.sh"; then
        norma_fail 'norma is installed, but layout preparation failed. Run norma --doctor for next steps.'; exit 1
    fi
fi
printf '\nNext steps%s:\n' "${norma_dest:+ on the target host}"
printf '  %q --demo\n  sudo %q --doctor\n  sudo %q\n' "$norma_prefix/bin/norma" "$norma_prefix/bin/norma" "$norma_prefix/bin/norma"
printf '  %q list  # snapshots, rollback and uninstall\n' "$norma_prefix/bin/norma-manage"
case ":$PATH:" in *":$norma_prefix/bin:"*) ;; *) printf '\nOptional PATH setup: export PATH=%q/bin:"$PATH"\n' "$norma_prefix";; esac
