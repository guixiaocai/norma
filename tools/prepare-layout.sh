#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Compile type-offset constants only. Never builds or loads a kernel module.
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
nfsmon_release=$(uname -r)
nfsmon_tree=${NORMA_KERNEL_BUILD:-${NFSMON_KERNEL_BUILD:-/lib/modules/$nfsmon_release/build}}
nfsmon_binary=${NORMA_BIN:-${NFSMON_BIN:-$base/bin/norma}}
if [[ ! -x $nfsmon_binary ]]; then nfsmon_binary=$(command -v norma || true); fi
[[ -n $nfsmon_binary && -x $nfsmon_binary ]] || { echo 'Build or install norma first.' >&2; exit 1; }
[[ $(uname -m) == x86_64 ]] || { echo 'This release supplies the x86_64 ABI adapter.' >&2; exit 1; }
# This header fallback knows EL8's file-pointer NFSD calling convention. Other
# families use runtime BTF prototypes or an explicit reviewed JSON profile.
[[ $nfsmon_release == 4.18.0-*.el8* ]] || {
    echo 'This kernel is outside the header helper function profile. Use runtime BTF or a reviewed --layout file; see docs/compatibility.md.' >&2
    exit 1
}
for nfsmon_tool in make gcc; do
    command -v "$nfsmon_tool" >/dev/null || { echo "Install $nfsmon_tool." >&2; exit 1; }
done
[[ -r $nfsmon_tree/include/linux/sunrpc/svc.h && -r $nfsmon_tree/include/generated/utsrelease.h ]] || {
    echo "Install development headers matching $nfsmon_release. Missing headers: $nfsmon_tree" >&2; exit 1;
}
nfsmon_actual=$(sed -n 's/^#define UTS_RELEASE "\(.*\)"/\1/p' "$nfsmon_tree/include/generated/utsrelease.h")
[[ $nfsmon_actual == "$nfsmon_release" ]] || { echo "Header layout describes $nfsmon_actual, running $nfsmon_release." >&2; exit 1; }
nfsmon_tmp=$(mktemp -d)
trap 'rm -rf -- "$nfsmon_tmp"' EXIT
cat > "$nfsmon_tmp/Makefile" <<'EOF'
obj-m += nfsmon_layout.o
EOF
cat > "$nfsmon_tmp/nfsmon_layout.c" <<'EOF'
#include <linux/fs.h>
#include <linux/sunrpc/svc.h>
#include <linux/stddef.h>
const unsigned int nfsmon_offsets[] __attribute__((used)) = {
    offsetof(struct svc_rqst,rq_addr),
    offsetof(struct svc_rqst,rq_daddr),
    offsetof(struct svc_rqst,rq_vers),
    offsetof(struct svc_rqst,rq_prot),
    offsetof(struct svc_rqst,rq_cred.cr_uid.val),
    offsetof(struct svc_rqst,rq_cred.cr_gid.val),
    offsetof(struct file,f_inode),
    offsetof(struct file,f_path.dentry),
    offsetof(struct inode,i_ino),
    offsetof(struct inode,i_sb),
    offsetof(struct super_block,s_dev),
    offsetof(struct dentry,d_parent),
    offsetof(struct dentry,d_name.name)
};
EOF
make --no-print-directory -s -C "$nfsmon_tree" M="$nfsmon_tmp" CC=gcc nfsmon_layout.o
nfsmon_cache=${NORMA_LAYOUT_DIR:-${NFSMON_LAYOUT_DIR:-/var/cache/norma}}
mkdir -p -- "$nfsmon_cache"
# Atomic write: an interrupted generation cannot replace a usable layout.
"$nfsmon_binary" --layout-object "$nfsmon_tmp/nfsmon_layout.o" "$nfsmon_release" > "$nfsmon_tmp/layout.json"
install -m 0644 "$nfsmon_tmp/layout.json" "$nfsmon_cache/.layout-$nfsmon_release.tmp"
mv -- "$nfsmon_cache/.layout-$nfsmon_release.tmp" "$nfsmon_cache/layout-$nfsmon_release.json"
echo "Prepared NFSD layout: $nfsmon_cache/layout-$nfsmon_release.json"
