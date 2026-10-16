#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Offline installer integration checks. Never downloads or touches /usr/local.
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
norma_test_tmp=$(mktemp -d)
norma_test_pid=
cleanup() {
    if [[ -n $norma_test_pid ]]; then kill "$norma_test_pid" 2>/dev/null || true; fi
    rm -rf -- "$norma_test_tmp"
}
trap cleanup EXIT
export NORMA_TEST_DATA="$norma_test_tmp/downloads"
export NORMA_TEST_CALLS="$norma_test_tmp/downloads.log"
export NORMA_TEST_PREPARED="$norma_test_tmp/prepared"
norma_test_name=norma-v0.5.0-linux-amd64
norma_test_source="$norma_test_tmp/source/$norma_test_name"
mkdir -p "$norma_test_source/bin" "$norma_test_source/tools" "$norma_test_source/licenses" "$norma_test_source/docs" "$NORMA_TEST_DATA" "$norma_test_tmp/mocks"
cp "$base/tools/install.sh" "$base/tools/manage.sh" "$base/tools/lifecycle.sh" "$norma_test_source/tools/"
printf "v0.5.0\n" > "$norma_test_source/VERSION"
printf '#!/usr/bin/env bash\nprintf "norma v0.5.0 fixture\\n"\n' > "$norma_test_source/bin/norma"
cat > "$norma_test_source/tools/prepare-layout.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ -x $NORMA_BIN ]]
printf '%s\n' "$NORMA_BIN" > "$NORMA_TEST_PREPARED"
EOF
printf 'License fixture\n' > "$norma_test_source/LICENSE"
printf 'Notice fixture\n' > "$norma_test_source/NOTICE"
printf 'Dependency license fixture\n' > "$norma_test_source/licenses/dependency.txt"
for norma_document in README.md README.zh-CN.md CHANGELOG.md docs/guide.md; do
    printf 'Documentation fixture\n' > "$norma_test_source/$norma_document"
done
chmod +x "$norma_test_source/bin/norma" "$norma_test_source/tools/prepare-layout.sh"
make_archive() {
    tar --mtime=@0 -czf "$NORMA_TEST_DATA/$norma_test_name.tar.gz" -C "$norma_test_tmp/source" "$norma_test_name"
    (cd "$NORMA_TEST_DATA" && sha256sum "$norma_test_name.tar.gz" > SHA256SUMS)
}
make_archive
printf '{"tag_name": "v0.5.0"}\n' > "$NORMA_TEST_DATA/release.json"
cat > "$norma_test_tmp/mocks/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
destination= url=
while (($#)); do
    case "$1" in
        --output) destination=$2; shift 2 ;;
        https://*) url=$1; shift ;;
        *) shift ;;
    esac
done
printf '%s\n' "$url" >> "$NORMA_TEST_CALLS"
case "$url" in
    https://api.github.com/repos/guixiaocai/norma/releases/latest) asset=release.json ;;
    https://github.com/guixiaocai/norma/releases/download/v0.5.0/*) asset=${url##*/} ;;
    *) echo "Unexpected download URL: $url" >&2; exit 1 ;;
esac
cp "$NORMA_TEST_DATA/$asset" "$destination"
EOF
chmod +x "$norma_test_tmp/mocks/curl"
export PATH="$norma_test_tmp/mocks:$PATH"
expect_failure() {
    if "$@" > "$norma_test_tmp/failure.log" 2>&1; then
        printf 'Expected failure: %s\n' "$*" >&2
        exit 1
    fi
}
contains() { grep -q -- "$1" "$2"; }
assert_installed() {
    [[ -x $1/bin/norma && -x $1/share/norma/tools/prepare-layout.sh ]]
    [[ -f $1/share/norma/LICENSE && -f $1/share/norma/NOTICE && -f $1/share/norma/licenses/dependency.txt ]]
    [[ -f $1/share/norma/README.zh-CN.md && -f $1/share/norma/docs/guide.md ]]
    [[ $("$1/bin/norma") == 'norma v0.5.0 fixture' ]]
}

# An offline archive installs under a user prefix, including paths with spaces.
bash "$norma_test_source/tools/install.sh" --prefix "$norma_test_tmp/user prefix" > "$norma_test_tmp/local.log"
assert_installed "$norma_test_tmp/user prefix"
contains --doctor "$norma_test_tmp/local.log"
contains --demo "$norma_test_tmp/local.log"
[[ ! -e $NORMA_TEST_PREPARED ]]
# Reinstalling is supported and does not nest the license directory.
bash "$norma_test_source/tools/install.sh" --prefix "$norma_test_tmp/user prefix" > /dev/null
[[ ! -e $norma_test_tmp/user\ prefix/share/norma/licenses/licenses ]]
# An upgrade replaces the directory entry while the old executable keeps running.
cp "$(command -v sleep)" "$norma_test_tmp/user prefix/bin/norma"
"$norma_test_tmp/user prefix/bin/norma" 30 &
norma_test_pid=$!
sleep 0.05
bash "$norma_test_source/tools/install.sh" --prefix "$norma_test_tmp/user prefix" > /dev/null
kill -0 "$norma_test_pid"
assert_installed "$norma_test_tmp/user prefix"
kill "$norma_test_pid"
wait "$norma_test_pid" 2>/dev/null || true
norma_test_pid=
DESTDIR="$norma_test_tmp/stage" PREFIX=/usr/local bash "$norma_test_source/tools/install.sh" > /dev/null
assert_installed "$norma_test_tmp/stage/usr/local"
expect_failure env DESTDIR="$norma_test_tmp/stage" bash "$norma_test_source/tools/install.sh" --prepare-layout
contains 'cannot be used with DESTDIR' "$norma_test_tmp/failure.log"
bash "$norma_test_source/tools/install.sh" --prefix "$norma_test_tmp/prepared-prefix" --prepare-layout > /dev/null
[[ $(cat "$NORMA_TEST_PREPARED") == "$norma_test_tmp/prepared-prefix/bin/norma" ]]
rm "$NORMA_TEST_PREPARED"
expect_failure bash "$norma_test_source/tools/install.sh" --prefix relative
expect_failure bash "$norma_test_source/tools/install.sh" --prefix

# Explicit versions skip release discovery; both paths verify the release hash.
bash "$base/install.sh" --version v0.5.0 --prefix "$norma_test_tmp/online pinned" > "$norma_test_tmp/online.log"
assert_installed "$norma_test_tmp/online pinned"
contains 'SHA256 verified' "$norma_test_tmp/online.log"
[[ $(wc -l < "$NORMA_TEST_CALLS") -eq 2 ]]
bash "$base/install.sh" --prefix "$norma_test_tmp/online-latest" > /dev/null
assert_installed "$norma_test_tmp/online-latest"
contains 'releases/latest' "$NORMA_TEST_CALLS"
[[ ! -e $NORMA_TEST_PREPARED ]]
bash "$base/install.sh" --version v0.5.0 --prefix "$norma_test_tmp/online-prepared" --prepare-layout > /dev/null
[[ $(cat "$NORMA_TEST_PREPARED") == "$norma_test_tmp/online-prepared/bin/norma" ]]
rm "$NORMA_TEST_PREPARED"
expect_failure env DESTDIR="$norma_test_tmp/stage" bash "$base/install.sh" --prepare-layout
contains 'cannot be used with DESTDIR' "$norma_test_tmp/failure.log"
expect_failure bash "$base/install.sh" --version ../../bad --prefix "$norma_test_tmp/invalid"
[[ ! -e $norma_test_tmp/invalid ]]
expect_failure bash "$base/install.sh" --unknown

# Corrupt/missing/duplicate hashes fail before any installation occurs.
cp "$NORMA_TEST_DATA/SHA256SUMS" "$norma_test_tmp/good-checksums"
printf '\ncorrupt\n' >> "$NORMA_TEST_DATA/$norma_test_name.tar.gz"
expect_failure bash "$base/install.sh" --version v0.5.0 --prefix "$norma_test_tmp/corrupt"
contains 'SHA256 verification failed' "$norma_test_tmp/failure.log"
[[ ! -e $norma_test_tmp/corrupt ]]
make_archive
printf '%064d  unrelated.tar.gz\n' 0 > "$NORMA_TEST_DATA/SHA256SUMS"
expect_failure bash "$base/install.sh" --version v0.5.0 --prefix "$norma_test_tmp/missing-hash"
contains 'Missing or ambiguous' "$norma_test_tmp/failure.log"
cat "$norma_test_tmp/good-checksums" "$norma_test_tmp/good-checksums" > "$NORMA_TEST_DATA/SHA256SUMS"
expect_failure bash "$base/install.sh" --version v0.5.0 --prefix "$norma_test_tmp/duplicate-hash"
contains 'Missing or ambiguous' "$norma_test_tmp/failure.log"

# Reject symbolic links even when the manifest checksum is valid.
ln -s /tmp "$norma_test_source/unsafe-link"
make_archive
expect_failure bash "$base/install.sh" --version v0.5.0 --prefix "$norma_test_tmp/unsafe"
contains 'unsupported links or special files' "$norma_test_tmp/failure.log"
[[ ! -e $norma_test_tmp/unsafe ]]
rm "$norma_test_source/unsafe-link"
make_archive

# Exercise wget-only systems without relying on the host's network tools.
mkdir "$norma_test_tmp/wget-only"
for norma_command in bash uname tar gzip sha256sum awk sed mktemp rm cp mkdir install dirname cat mv find sort chmod rmdir date; do
    ln -s "$(command -v "$norma_command")" "$norma_test_tmp/wget-only/$norma_command"
done
cat > "$norma_test_tmp/wget-only/wget" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
destination= url=
while (($#)); do
    case "$1" in
        --output-document=*) destination=${1#*=}; shift ;;
        https://*) url=$1; shift ;;
        *) shift ;;
    esac
done
case "$url" in
    https://api.github.com/repos/guixiaocai/norma/releases/latest) asset=release.json ;;
    https://github.com/guixiaocai/norma/releases/download/v0.5.0/*) asset=${url##*/} ;;
    *) exit 1 ;;
esac
cp "$NORMA_TEST_DATA/$asset" "$destination"
EOF
chmod +x "$norma_test_tmp/wget-only/wget"
PATH="$norma_test_tmp/wget-only" bash "$base/install.sh" --prefix "$norma_test_tmp/wget-prefix" > /dev/null
assert_installed "$norma_test_tmp/wget-prefix"
./tools/bundle-offline.sh --version v0.5.0 --kernel 4.18.0-553.el8_10.x86_64 --output "$norma_test_tmp/offline bundle" > /dev/null
grep -q 'kernel-devel-4.18.0-553.el8_10.x86_64' "$norma_test_tmp/offline bundle/DEPENDENCIES.txt"
grep -q 'Dependency RPMs included: NO' "$norma_test_tmp/offline bundle/DEPENDENCIES.txt"
norma_download_count=$(wc -l < "$NORMA_TEST_CALLS")
bash "$norma_test_tmp/offline bundle/install-offline.sh" --prefix "$norma_test_tmp/bundled-prefix" > /dev/null
assert_installed "$norma_test_tmp/bundled-prefix"
[[ $(wc -l < "$NORMA_TEST_CALLS") -eq $norma_download_count ]]
printf 'corrupt bundle\n' >> "$norma_test_tmp/offline bundle/$norma_test_name.tar.gz"
expect_failure bash "$norma_test_tmp/offline bundle/install-offline.sh" --prefix "$norma_test_tmp/bad-bundle"
grep -q 'Bundle integrity verification failed' "$norma_test_tmp/failure.log"
[[ ! -e $norma_test_tmp/bad-bundle ]]
expect_failure ./tools/bundle-offline.sh --version v0.5.0 --kernel 4.18.0-553.el8_10.x86_64 --output "$norma_test_tmp/offline bundle"
printf 'rpm fixture\n' > "$NORMA_TEST_DATA/norma-0.5.0-1.el8.x86_64.rpm"
(cd "$NORMA_TEST_DATA" && sha256sum norma-0.5.0-1.el8.x86_64.rpm >> SHA256SUMS)
./tools/bundle-offline.sh --version v0.5.0 --kernel 4.18.0-553.el8_10.x86_64 --format rpm --output "$norma_test_tmp/rpm bundle" > /dev/null
export NORMA_TEST_DNF="$norma_test_tmp/dnf-args"
cat > "$norma_test_tmp/mocks/dnf" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$@" > "$NORMA_TEST_DNF"
EOF
chmod +x "$norma_test_tmp/mocks/dnf"
bash "$norma_test_tmp/rpm bundle/install-offline.sh"
grep -qx -- '--disablerepo=\*' "$NORMA_TEST_DNF"
grep -qx 'install' "$NORMA_TEST_DNF"
grep -qx "$norma_test_tmp/rpm bundle/norma-0.5.0-1.el8.x86_64.rpm" "$NORMA_TEST_DNF"
printf 'Installer checks passed: tar/RPM offline bundles, user prefix, running upgrade, staging, layout opt-in, curl/wget, latest/pinned, checksum and archive rejection.\n'
