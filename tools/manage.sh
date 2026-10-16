#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail
norma_tools=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=tools/lifecycle.sh
source "$norma_tools/lifecycle.sh"
norma_prefix=${PREFIX:-/usr/local}
norma_purge=false
norma_action=
norma_selection=
usage() {
    cat <<'EOF'
Manage a norma tar installation (RPM users should use dnf).

Usage: norma-manage [--prefix PATH] list
       norma-manage [--prefix PATH] rollback SNAPSHOT_ID
       norma-manage [--prefix PATH] uninstall [--purge-data]
       bash tools/manage.sh --prefix PATH COMMAND

Upgrades/rollback/uninstall save complete payload snapshots under
PREFIX/share/norma/.install/snapshots. Uninstall removes only unchanged files
listed in the validated installed manifest; local edits are preserved.
Default uninstall preserves snapshots and layout cache. --purge-data deletes
only those snapshots and, for /usr or /usr/local, regular layout-*.json files in
/var/cache/norma (inside DESTDIR when staging). Custom layout paths, arbitrary
configuration files, exports and services are never removed. After uninstall,
use tools/manage.sh from an extracted release to list or restore saved snapshots.
EOF
}
while (($#)); do
    case "$1" in
        --prefix)
            (($# >= 2)) && [[ -n $2 && $2 != --* ]] || { norma_fail '--prefix requires a value.'; exit 1; }
            norma_prefix=$2; shift 2 ;;
        --purge-data) norma_purge=true; shift ;;
        list|rollback|uninstall)
            [[ -z $norma_action ]] || { norma_fail 'Specify one command.'; exit 1; }
            norma_action=$1; shift ;;
        -h|--help) usage; exit 0 ;;
        --*) norma_fail "Unknown option: $1"; exit 1 ;;
        *)
            [[ $norma_action == rollback && -z $norma_selection ]] || { norma_fail "Unexpected argument: $1"; exit 1; }
            norma_selection=$1; shift ;;
    esac
done
[[ -n $norma_action ]] || { usage; exit 1; }
if $norma_purge && [[ $norma_action != uninstall ]]; then norma_fail '--purge-data is only valid with uninstall.'; exit 1; fi
norma_init_paths
if [[ $norma_action == list ]]; then
    if [[ -f $norma_manifest ]]; then printf 'Installed: %s\n' "$(cat "$norma_state/version")"; else printf 'No active tar installation.\n'; fi
    printf 'SNAPSHOT ID\tVERSION\tREASON\tCREATED (UTC)\n'
    for norma_snapshot_dir in "$norma_state"/snapshots/*; do
        [[ -d $norma_snapshot_dir && ! -L $norma_snapshot_dir ]] || continue
        printf '%s\t%s\t%s\t%s\n' "${norma_snapshot_dir##*/}" "$(cat "$norma_snapshot_dir/version")" "$(cat "$norma_snapshot_dir/reason")" "$(cat "$norma_snapshot_dir/created")"
    done
    exit 0
fi
norma_check_rpm
norma_work=$(mktemp -d)
cleanup() { rm -rf -- "$norma_work"; norma_unlock; }
trap cleanup EXIT
trap 'exit 130' INT TERM
norma_lock
norma_previous="$norma_work/absent-manifest"
if [[ -f $norma_manifest ]]; then
    norma_validate_manifest "$norma_manifest"
    cp "$norma_manifest" "$norma_work/previous-manifest.tsv"
    norma_previous="$norma_work/previous-manifest.tsv"
fi
if [[ $norma_action == rollback ]]; then
    [[ $norma_selection =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]+$ && $norma_selection != *'..'* ]] || { norma_fail 'Use a snapshot ID printed by list.'; exit 1; }
    norma_restore="$norma_state/snapshots/$norma_selection"
    norma_assert_path "$norma_restore"
    [[ -d $norma_restore ]] || { norma_fail "Snapshot not found: $norma_selection"; exit 1; }
    norma_verify_payload "$norma_restore/payload" "$norma_restore/manifest.tsv"
    norma_version=$(cat "$norma_restore/version")
    [[ $norma_version =~ ^[a-zA-Z0-9._-]+$ ]] || { norma_fail 'Invalid snapshot version.'; exit 1; }
    norma_preserved="$norma_work/preserved.tsv"
    : > "$norma_preserved"
    norma_check_conflicts "$norma_restore/manifest.tsv" "$norma_previous" true "$norma_preserved"
    norma_saved=
    if [[ -f $norma_previous ]]; then norma_snapshot before-rollback; norma_saved="$norma_state/snapshots/$norma_snapshot_id"; fi
    if ! norma_activate "$norma_restore/payload" "$norma_restore/manifest.tsv" "$norma_version" "$norma_previous"; then
        if [[ -n $norma_saved ]]; then
            norma_activate "$norma_saved/payload" "$norma_saved/manifest.tsv" "$(cat "$norma_saved/version")" "$norma_restore/manifest.tsv" ||
                printf 'Automatic restoration failed; saved snapshot: %s\n' "${norma_saved##*/}" >&2
        else
            norma_remove_manifest "$norma_restore/manifest.tsv" "$norma_preserved" || true
            rm -f -- "$norma_manifest" "$norma_state/version"
            norma_cleanup_empty_dirs "$norma_restore/manifest.tsv"
        fi
        norma_fail 'Rollback failed; the saved snapshots are retained.'; exit 1
    fi
    printf 'Restored %s from %s.\n' "$norma_version" "$norma_selection"
else
    [[ -f $norma_previous ]] || { norma_fail 'No active tar installation was found in this prefix.'; exit 1; }
    norma_snapshot before-uninstall
    norma_remove_manifest "$norma_previous"
    rm -- "$norma_manifest" "$norma_state/version"
    norma_cleanup_empty_dirs "$norma_previous"
    if $norma_purge; then
        norma_purge_data
        printf 'Uninstalled norma; managed snapshots and eligible built-in layout cache were purged.\n'
    else
        printf 'Uninstalled norma. Saved snapshots: %s/snapshots\nLayout cache and locally modified files were preserved.\n' "$norma_state"
    fi
fi
