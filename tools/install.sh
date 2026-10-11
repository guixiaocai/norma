#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
nfsmon_prefix=${PREFIX:-/usr/local}
nfsmon_dest=${DESTDIR:-}
install -d "$nfsmon_dest$nfsmon_prefix/bin" "$nfsmon_dest$nfsmon_prefix/share/norma/tools"
install -m 0755 "$base/bin/norma" "$nfsmon_dest$nfsmon_prefix/bin/norma"
install -m 0755 "$base/tools/prepare-layout.sh" "$nfsmon_dest$nfsmon_prefix/share/norma/tools/prepare-layout.sh"
install -m 0644 "$base/LICENSE" "$base/NOTICE" "$nfsmon_dest$nfsmon_prefix/share/norma/"
cp -a "$base/licenses" "$nfsmon_dest$nfsmon_prefix/share/norma/"
echo "Installed into $nfsmon_dest$nfsmon_prefix."
