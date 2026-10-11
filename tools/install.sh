#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
nfsmon_prefix=${PREFIX:-/usr/local}
nfsmon_dest=${DESTDIR:-}
install -d "$nfsmon_dest$nfsmon_prefix/bin" "$nfsmon_dest$nfsmon_prefix/share/nfsmon/tools"
for nfsmon_name in nfstop nfsdtop; do
    install -m 0755 "$base/bin/$nfsmon_name" "$nfsmon_dest$nfsmon_prefix/bin/$nfsmon_name"
done
install -m 0755 "$base/tools/prepare-layout.sh" "$nfsmon_dest$nfsmon_prefix/share/nfsmon/tools/prepare-layout.sh"
install -m 0644 "$base/LICENSE" "$base/NOTICE" "$nfsmon_dest$nfsmon_prefix/share/nfsmon/"
cp -a "$base/licenses" "$nfsmon_dest$nfsmon_prefix/share/nfsmon/"
echo "Installed into $nfsmon_dest$nfsmon_prefix."
