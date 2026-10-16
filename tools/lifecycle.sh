#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Shared tar-install lifecycle functions. Manifests are data, never shell code.

norma_fail() { printf 'norma: %s\n' "$*" >&2; return 1; }
norma_init_paths() {
    norma_prefix=${norma_prefix:-${PREFIX:-/usr/local}}
    norma_dest=${DESTDIR:-}
    while [[ $norma_prefix != / && $norma_prefix == */ ]]; do norma_prefix=${norma_prefix%/}; done
    while [[ $norma_dest != / && $norma_dest == */ ]]; do norma_dest=${norma_dest%/}; done
    [[ $norma_prefix == /* && $norma_prefix != / && $norma_prefix != *$'\n'* && $norma_prefix != *$'\t'* ]] ||
        { norma_fail 'The installation prefix must be an absolute path other than /.'; return 1; }
    [[ -z $norma_dest || ( $norma_dest == /* && $norma_dest != / && $norma_dest != *$'\n'* && $norma_dest != *$'\t'* ) ]] ||
        { norma_fail 'DESTDIR must be an absolute staging path other than /.'; return 1; }
    case "$norma_prefix/$norma_dest/" in *'/../'*|*'/./'*) norma_fail 'Paths must not contain . or .. components.'; return 1;; esac
    norma_target="$norma_dest$norma_prefix"
    norma_state="$norma_target/share/norma/.install"
    norma_manifest="$norma_state/manifest.tsv"
    norma_assert_path "$norma_state" || return 1
    norma_assert_path "$norma_state/snapshots" || return 1
    norma_assert_path "$norma_state/lock" || return 1
}
norma_assert_path() {
    local candidate=$1
    while [[ $candidate != / && -n $candidate ]]; do
        [[ ! -L $candidate ]] || { norma_fail "Refusing symbolic-link path: $candidate"; return 1; }
        candidate=${candidate%/*}
    done
}
norma_check_rpm() {
    command -v rpm >/dev/null || return 0
    local rpm_args=()
    if [[ -n $norma_dest ]]; then rpm_args=(--root "$norma_dest"); fi
    if rpm "${rpm_args[@]}" -q norma >/dev/null 2>&1 ||
       rpm "${rpm_args[@]}" -qf "$norma_prefix/bin/norma" >/dev/null 2>&1; then
        norma_fail 'An RPM installation owns norma. Use dnf upgrade/downgrade/remove norma; do not mix RPM and tar installers.'
        return 1
    fi
}
norma_lock() {
    norma_assert_path "$norma_state/snapshots" || return 1
    norma_assert_path "$norma_state/lock" || return 1
    install -d "$norma_state/snapshots" || return 1
    mkdir "$norma_state/lock" 2>/dev/null || {
        norma_fail "Another lifecycle operation may be running. Check $norma_state/lock before retrying."
        return 1
    }
    norma_locked=true
}
norma_unlock() {
    if [[ ${norma_locked:-false} == true ]]; then
        rmdir "$norma_state/lock" 2>/dev/null || true
        rmdir "$norma_state" 2>/dev/null || true
    fi
}
norma_valid_relative() {
    local relative=$1
    [[ -n $relative && $relative != /* && $relative != *$'\n'* && $relative != *$'\t'* && $relative != *'//'* ]] || return 1
    case "/$relative/" in *'/../'*|*'/./'*) return 1;; esac
    case "$relative" in
        bin/norma|bin/norma-manage|share/norma/LICENSE|share/norma/NOTICE|share/norma/README.md|share/norma/README.zh-CN.md|share/norma/CHANGELOG.md|share/norma/VERSION) ;;
        share/norma/tools/prepare-layout.sh|share/norma/tools/manage.sh|share/norma/tools/lifecycle.sh) ;;
        share/norma/docs/*|share/norma/licenses/*|share/norma/examples/*) ;;
        *) return 1 ;;
    esac
}
norma_validate_manifest() {
    local manifest=$1 hash mode relative extra count=0
    local -A seen=()
    [[ -f $manifest && ! -L $manifest ]] || { norma_fail "Missing regular manifest: $manifest"; return 1; }
    while IFS=$'\t' read -r hash mode relative extra || [[ -n $hash ]]; do
        [[ $hash =~ ^[a-fA-F0-9]{64}$ && ( $mode == 0644 || $mode == 0755 ) && -z $extra ]] &&
            norma_valid_relative "$relative" || { norma_fail "Invalid entry in $manifest"; return 1; }
        [[ -z ${seen[$relative]+present} ]] || { norma_fail "Duplicate manifest path: $relative"; return 1; }
        seen[$relative]=1
        count=$((count + 1))
    done < "$manifest"
    ((count > 0)) || { norma_fail "Empty manifest: $manifest"; return 1; }
}
norma_hash() { local result; result=$(sha256sum -- "$1") || return 1; printf '%s' "${result%% *}"; }
norma_make_manifest() {
    local payload=$1 manifest=$2 relative hash mode
    : > "$manifest"
    while IFS= read -r -d '' relative; do
        relative=${relative#"$payload/"}
        norma_valid_relative "$relative" || { norma_fail "Unexpected payload file: $relative"; return 1; }
        mode=0644
        case "$relative" in bin/*|share/norma/tools/*.sh) mode=0755;; esac
        hash=$(norma_hash "$payload/$relative") || return 1
        printf '%s\t%s\t%s\n' "$hash" "$mode" "$relative" >> "$manifest"
    done < <(find "$payload" -type f -print0 | LC_ALL=C sort -z)
    norma_validate_manifest "$manifest"
}
norma_verify_payload() {
    local payload=$1 manifest=$2 hash mode relative
    norma_validate_manifest "$manifest" || return 1
    while IFS=$'\t' read -r hash mode relative; do
        norma_assert_path "$payload/$relative" || return 1
        [[ -f $payload/$relative && $(norma_hash "$payload/$relative") == "$hash" ]] || {
            norma_fail "Snapshot/payload checksum mismatch: $relative"; return 1;
        }
    done < "$manifest"
}
norma_snapshot() {
    local reason=$1 manifest=${2:-$norma_manifest} version=${3:-} staging hash mode relative actual id
    norma_validate_manifest "$manifest" || return 1
    norma_assert_path "$norma_state/snapshots" || return 1
    if [[ -z $version ]]; then version=$(cat "$norma_state/version"); fi
    [[ $version =~ ^[a-zA-Z0-9._-]+$ ]] || { norma_fail 'Invalid installed version metadata.'; return 1; }
    staging=$(mktemp -d "$norma_state/snapshots/.snapshot.XXXXXX") || return 1
    : > "$staging/manifest.tsv"
    while IFS=$'\t' read -r hash mode relative; do
        norma_assert_path "$norma_target/$relative" || return 1
        [[ -f $norma_target/$relative ]] || { norma_fail "Installed file missing: $relative. Restore it before upgrading or rolling back."; return 1; }
        install -D -m "$mode" "$norma_target/$relative" "$staging/payload/$relative" || return 1
        actual=$(norma_hash "$staging/payload/$relative") || return 1
        printf '%s\t%s\t%s\n' "$actual" "$mode" "$relative" >> "$staging/manifest.tsv"
    done < "$manifest"
    printf '%s\n' "$version" > "$staging/version"
    printf '%s\n' "$reason" > "$staging/reason"
    date -u +'%Y-%m-%dT%H:%M:%SZ' > "$staging/created"
    chmod 0755 "$staging"
    chmod 0700 "$staging/payload"
    chmod 0644 "$staging/manifest.tsv" "$staging/version" "$staging/reason" "$staging/created"
    id="$version-$(date -u +%Y%m%dT%H%M%S)-${staging##*.}"
    mv -T -- "$staging" "$norma_state/snapshots/$id" || return 1
    norma_snapshot_id=$id
    printf 'Saved full installation snapshot: %s\n' "$id"
}
norma_manifest_has() {
    local manifest=$1 relative=$2
    [[ -f $manifest ]] && awk -F '\t' -v path="$relative" '$3 == path { found=1 } END { exit !found }' "$manifest"
}
norma_check_conflicts() {
    local manifest=$1 previous=$2 allow_equal=${3:-false} preserved=${4:-} hash mode relative
    while IFS=$'\t' read -r hash mode relative; do
        norma_assert_path "$norma_target/$relative" || return 1
        if [[ -e $norma_target/$relative ]] && ! norma_manifest_has "$previous" "$relative"; then
            if [[ $allow_equal == true && -f $norma_target/$relative && $(norma_hash "$norma_target/$relative") == "$hash" ]]; then
                if [[ -n $preserved ]]; then printf '%s\t%s\t%s\n' "$hash" "$mode" "$relative" >> "$preserved"; fi
                continue
            fi
            norma_fail "Unmanaged file already exists: $norma_target/$relative. Move it aside, or use --adopt-legacy for an older norma tar installation."
            return 1
        fi
    done < "$manifest"
}
norma_atomic_file() {
    local source=$1 destination=$2 mode=$3 temporary
    norma_assert_path "$destination" || return 1
    install -d "${destination%/*}" || return 1
    temporary=$(mktemp "${destination%/*}/.norma-new.XXXXXX") || return 1
    if ! install -m "$mode" "$source" "$temporary" || ! mv -fT -- "$temporary" "$destination"; then
        rm -f -- "$temporary"
        return 1
    fi
}
norma_remove_manifest() {
    local manifest=$1 except=${2:-} hash mode relative
    norma_validate_manifest "$manifest" || return 1
    while IFS=$'\t' read -r hash mode relative; do
        if [[ -n $except ]] && norma_manifest_has "$except" "$relative"; then continue; fi
        norma_assert_path "$norma_target/$relative" || return 1
        if [[ -f $norma_target/$relative ]]; then
            if [[ $(norma_hash "$norma_target/$relative") == "$hash" ]]; then
                rm -- "$norma_target/$relative" || return 1
            else
                printf 'Preserved locally modified file: %s\n' "$norma_target/$relative"
            fi
        fi
    done < "$manifest"
}
norma_activate() {
    local payload=$1 manifest=$2 version=$3 previous=$4 hash mode relative version_file
    norma_verify_payload "$payload" "$manifest" || return 1
    while IFS=$'\t' read -r hash mode relative; do
        norma_atomic_file "$payload/$relative" "$norma_target/$relative" "$mode" || return 1
    done < "$manifest"
    if [[ -f $previous ]]; then norma_remove_manifest "$previous" "$manifest" || return 1; fi
    norma_atomic_file "$manifest" "$norma_manifest" 0644 || return 1
    version_file=$(mktemp "$norma_state/.version.XXXXXX") || return 1
    printf '%s\n' "$version" > "$version_file"
    chmod 0644 "$version_file" || { rm -f -- "$version_file"; return 1; }
    if ! mv -fT -- "$version_file" "$norma_state/version"; then rm -f -- "$version_file"; return 1; fi
}
norma_cleanup_empty_dirs() {
    local manifest=$1 hash mode relative directory
    while IFS=$'\t' read -r hash mode relative; do
        directory="$norma_target/${relative%/*}"
        while [[ $directory != "$norma_target" && $directory == "$norma_target/"* ]]; do
            rmdir "$directory" 2>/dev/null || break
            directory=${directory%/*}
        done
    done < "$manifest"
}
norma_purge_data() {
    norma_assert_path "$norma_state/snapshots" || return 1
    # The scope is fixed: our snapshot tree and built-in layout-*.json cache only.
    rm -rf -- "$norma_state/snapshots" || return 1
    if [[ $norma_prefix == /usr/local || $norma_prefix == /usr ]]; then
        local cache="$norma_dest/var/cache/norma"
        norma_assert_path "$cache" || return 1
        if [[ -d $cache ]]; then
            find "$cache" -maxdepth 1 -type f -name 'layout-*.json' -delete || return 1
            rmdir "$cache" 2>/dev/null || true
        fi
    fi
}
