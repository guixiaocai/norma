#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Prepare a portable, verified release bundle. Does not install/download RPM dependencies.
set -euo pipefail
fail() { printf 'norma offline bundle: %s\n' "$*" >&2; exit 1; }
usage() {
    cat <<'EOF'
Prepare an offline deployment directory on a connected workstation.

Usage: tools/bundle-offline.sh --version v0.5.0 --kernel KERNEL_RELEASE --output DIRECTORY [--format tar|rpm]

All three values are required. Output must be absent or an empty directory.
The tar release supplies the installer and documentation in both formats; rpm
also includes norma-VERSION-1.el8.x86_64.rpm. Every downloaded asset is checked
against the release SHA256SUMS. Dependency package names are recorded for the
specified target kernel; dependency RPMs are NOT downloaded. The EL8 layout
helper may additionally need gcc, make and kernel-devel matching that kernel.
Copy the entire output directory and run install-offline.sh on the target.
EOF
}
norma_version= norma_kernel= norma_output= norma_format=tar
while (($#)); do
    case "$1" in
        --version|--kernel|--output|--format)
            (($# >= 2)) && [[ -n $2 && $2 != --* ]] || fail "$1 requires a value."
            case "$1" in
                --version) norma_version=$2;; --kernel) norma_kernel=$2;;
                --output) norma_output=$2;; --format) norma_format=$2;;
            esac
            shift 2;;
        -h|--help) usage; exit 0;;
        *) fail "Unknown option: $1";;
    esac
done
[[ $norma_version =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]] || fail 'Specify an explicit release version such as v0.5.0.'
[[ $norma_kernel =~ ^[a-zA-Z0-9][a-zA-Z0-9._+-]+$ ]] || fail '--kernel must be the target host uname -r value, without slashes or spaces.'
[[ -n $norma_output && $norma_output != / && $norma_output != *$'\n'* && $norma_output != *$'\t'* ]] || fail '--output is required.'
[[ $norma_format == tar || $norma_format == rpm ]] || fail '--format must be tar or rpm.'
if [[ -e $norma_output || -L $norma_output ]]; then
    [[ -d $norma_output && ! -L $norma_output && -z $(find "$norma_output" -mindepth 1 -maxdepth 1 -print -quit) ]] ||
        fail 'Output already exists and is not an empty regular directory.'
fi
for norma_tool in tar sha256sum awk mktemp find sort xargs; do command -v "$norma_tool" >/dev/null || fail "Required command not found: $norma_tool"; done
if command -v curl >/dev/null; then
    download() { curl --fail --location --silent --show-error --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 180 --retry 2 --output "$2" "$1"; }
elif command -v wget >/dev/null; then
    download() { wget --https-only --timeout=30 --tries=3 --quiet --output-document="$2" "$1"; }
else
    fail 'Install curl or wget on the connected workstation.'
fi
norma_work=$(mktemp -d)
trap 'rm -rf -- "$norma_work"' EXIT
norma_bundle="$norma_work/bundle"
mkdir "$norma_bundle"
norma_name="norma-$norma_version-linux-amd64"
norma_tar="$norma_name.tar.gz"
norma_url="https://github.com/guixiaocai/norma/releases/download/$norma_version"
download "$norma_url/SHA256SUMS" "$norma_bundle/SHA256SUMS" || fail 'Cannot download release checksums.'
norma_assets=("$norma_tar")
norma_rpm_version=${norma_version#v}
norma_rpm_version=${norma_rpm_version/-/~}
norma_rpm="norma-$norma_rpm_version-1.el8.x86_64.rpm"
if [[ $norma_format == rpm ]]; then norma_assets+=("$norma_rpm"); fi
: > "$norma_bundle/ASSET-SHA256SUMS"
for norma_asset in "${norma_assets[@]}"; do
    norma_hash=$(awk -v asset="$norma_asset" '$2 == asset || $2 == "*" asset { print $1 }' "$norma_bundle/SHA256SUMS")
    [[ $norma_hash =~ ^[a-fA-F0-9]{64}$ ]] || fail "Missing or ambiguous checksum: $norma_asset"
    download "$norma_url/$norma_asset" "$norma_bundle/$norma_asset" || fail "Cannot download $norma_asset"
    printf '%s  %s\n' "$norma_hash" "$norma_asset" >> "$norma_bundle/ASSET-SHA256SUMS"
done
(cd "$norma_bundle" && sha256sum --check --status ASSET-SHA256SUMS) || fail 'Release checksum verification failed.'
tar -tzf "$norma_bundle/$norma_tar" > "$norma_work/paths"
awk -v root="$norma_name" '$0 != root && index($0, root "/") != 1 { bad=1 } /(^|\/)\.\.(\/|$)/ { bad=1 } END { exit bad }' "$norma_work/paths" || fail 'Unexpected paths in release archive.'
tar -tvzf "$norma_bundle/$norma_tar" > "$norma_work/types"
awk 'substr($0,1,1) != "-" && substr($0,1,1) != "d" { bad=1 } END { exit bad }' "$norma_work/types" || fail 'Unsupported links or special files in release archive.'
tar -xzf "$norma_bundle/$norma_tar" -C "$norma_work" --no-same-owner --no-same-permissions
[[ -f $norma_work/$norma_name/tools/install.sh && -d $norma_work/$norma_name/docs ]] || fail 'Release archive lacks installer or documentation.'
cp -R "$norma_work/$norma_name/docs" "$norma_bundle/docs"
for norma_document in README.md README.zh-CN.md CHANGELOG.md LICENSE NOTICE; do
    cp "$norma_work/$norma_name/$norma_document" "$norma_bundle/"
done
if [[ -d $norma_work/$norma_name/examples ]]; then cp -R "$norma_work/$norma_name/examples" "$norma_bundle/"; fi
cat > "$norma_bundle/DEPENDENCIES.txt" <<EOF
norma offline dependency manifest
Release: $norma_version
Format: $norma_format (Linux x86_64)
Target kernel: $norma_kernel
Dependency RPMs included: NO
Target distribution for the RPM package/header helper: Rocky/Alma/RHEL 8 x86_64

The native collector binary needs no Go, clang, libbpf shared library or bpftrace.
Tar installation uses bash and ordinary coreutils, tar and gzip.
RPM installation uses the target's dnf/rpm plus bash and coreutils.

Optional EL8 header-layout helper dependencies (not downloaded):
  kernel-devel-$norma_kernel
  gcc
  make
These are only needed when runtime BTF/a reviewed compatible layout is unavailable.
The helper accepts the built-in EL8 4.18 x86_64 profile; a kernel version string
alone does not establish compatibility. Run norma --doctor on the target first.
Headers must match the RUNNING target kernel exactly; never substitute the build
workstation's kernel-devel package.

Optional alternative backend (not downloaded):
  bpftrace >= 0.16 and its target-distribution runtime dependencies

For a fully disconnected host, obtain dependencies and their complete closure
from a connected machine with the same target distribution/repositories. On a
matching EL8 host with dnf-plugins-core, an explicit preparation command is:
  dnf download --resolve --alldeps --destdir ./rpms "kernel-devel-$norma_kernel" gcc make
Check repository availability and transfer those RPMs separately. This bundle
does not claim that kernel packages or any dependency closure were downloaded.
The offline installer disables repositories for local RPM installation.
EOF
{
    printf '#!/usr/bin/env bash\nset -euo pipefail\n'
    printf 'norma_format=%q\nnorma_tar=%q\nnorma_name=%q\nnorma_rpm=%q\n' "$norma_format" "$norma_tar" "$norma_name" "$norma_rpm"
    cat <<'EOF'
norma_bundle=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
cd "$norma_bundle"
sha256sum --check --status BUNDLE-SHA256SUMS || { echo 'Bundle integrity verification failed; nothing was installed.' >&2; exit 1; }
sha256sum --check --status ASSET-SHA256SUMS || { echo 'Release asset verification failed.' >&2; exit 1; }
if [[ $norma_format == rpm ]]; then
    (($# == 0)) || { echo 'RPM installation accepts no --prefix/options here; use dnf directly for package management.' >&2; exit 1; }
    command -v dnf >/dev/null || { echo 'RPM bundles require dnf on a matching EL8 target.' >&2; exit 1; }
    exec dnf --disablerepo='*' install "$norma_bundle/$norma_rpm"
fi
norma_tmp=$(mktemp -d)
trap 'rm -rf -- "$norma_tmp"' EXIT
tar -xzf "$norma_bundle/$norma_tar" -C "$norma_tmp" --no-same-owner --no-same-permissions
exec_status=0
bash "$norma_tmp/$norma_name/tools/install.sh" "$@" || exec_status=$?
exit "$exec_status"
EOF
} > "$norma_bundle/install-offline.sh"
chmod +x "$norma_bundle/install-offline.sh"
(
    cd "$norma_bundle"
    find . -type f ! -name BUNDLE-SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > BUNDLE-SHA256SUMS
)
mkdir -p -- "$(dirname -- "$norma_output")"
if [[ -d $norma_output ]]; then rmdir -- "$norma_output"; fi
mv -T -- "$norma_bundle" "$norma_output"
printf 'Created verified offline bundle: %s\nDependency RPMs were not downloaded; see DEPENDENCIES.txt.\n' "$norma_output"
