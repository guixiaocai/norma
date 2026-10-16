#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Run only in the disposable Rocky 8.10 CI container, never on a deployed host.
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
[[ ${NORMA_RPM_TEST_CONTAINER:-} == 1 && $EUID -eq 0 && ( -f /.dockerenv || -f /run/.containerenv ) ]] || {
    echo 'Run RPM lifecycle tests in the disposable Rocky Linux 8.10 CI container with NORMA_RPM_TEST_CONTAINER=1.' >&2
    exit 1
}
source /etc/os-release
[[ $ID == rocky && $VERSION_ID == 8.10 ]] || { echo 'RPM lifecycle checks require the Rocky Linux 8.10 test image.' >&2; exit 1; }
(($# == 1)) || { echo 'Usage: tools/test-rpm.sh /path/to/norma-0.5.0-1.el8.x86_64.rpm' >&2; exit 1; }
rpm_file=$(readlink -f -- "$1")
[[ -f $rpm_file ]] || { echo 'RPM artifact not found.' >&2; exit 1; }
if rpm -q norma >/dev/null 2>&1 || [[ -e /usr/bin/norma || -e /usr/local/bin/norma ]]; then
    echo 'The test container already contains norma; refusing to alter it.' >&2
    exit 1
fi
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
trap 'printf "RPM lifecycle check failed at line %s\n" "$LINENO" >&2' ERR
norma_version=$(rpm -qp --qf '%{VERSION}' "$rpm_file")
norma_release=$(rpm -qp --qf '%{RELEASE}' "$rpm_file")
[[ $norma_release == 1.el8 && $(rpm -qp --qf '%{ARCH}' "$rpm_file") == x86_64 ]]
rpm -qpl "$rpm_file" > "$work/files"
grep -qx '/usr/bin/norma' "$work/files"
grep -qx '/usr/share/norma/tools/prepare-layout.sh' "$work/files"
if grep -qE '/norma-manage|/\.install/|/var/cache/norma' "$work/files"; then
    echo 'RPM contains tar-manager state or package-owned layout cache.' >&2
    exit 1
fi
# Build a second native package revision to exercise real upgrade/downgrade.
VERSION="v${norma_version/~/-}" RPM_RELEASE=2 DIST_DIR="$work/rpms" "$base/tools/package-rpm.sh"
rpm_upgrade="$work/rpms/norma-$norma_version-2.el8.x86_64.rpm"
# Detect a legacy tar installation before the first RPM can be installed.
mkdir -p /usr/local/bin
printf '#!/bin/sh\nexit 0\n' > /usr/local/bin/norma
if dnf --disablerepo='*' -y install "$rpm_file" > "$work/conflict.log" 2>&1; then
    echo 'RPM unexpectedly installed alongside a legacy tar binary.' >&2
    exit 1
fi
grep -q 'tar installation detected' "$work/conflict.log"
rm /usr/local/bin/norma
mkdir -p /var/cache/norma /etc/norma
printf '{"test":"preserve"}\n' > /var/cache/norma/layout-rpm-test.json
printf 'user-owned configuration\n' > /etc/norma/local-test.conf
dnf --disablerepo='*' -y install "$rpm_file"
[[ $(rpm -q --qf '%{VERSION}-%{RELEASE}' norma) == "$norma_version-1.el8" ]]
rpm -ql norma
rpm -V norma
/usr/bin/norma --version
/usr/bin/norma --demo --json -r 250ms -s 1 > "$work/demo.jsonl"
[[ -s $work/demo.jsonl ]]
# The tar installer must also refuse the reverse mixture.
if "$base/tools/install.sh" --prefix "$work/tar-prefix" > "$work/tar-conflict.log" 2>&1; then
    echo 'Tar installer unexpectedly accepted an existing RPM package.' >&2
    exit 1
fi
grep -q 'RPM installation owns norma' "$work/tar-conflict.log"
[[ ! -e $work/tar-prefix ]]
dnf --disablerepo='*' -y upgrade "$rpm_upgrade"
[[ $(rpm -q --qf '%{VERSION}-%{RELEASE}' norma) == "$norma_version-2.el8" ]]
rpm -V norma
dnf --disablerepo='*' -y downgrade "$rpm_file"
[[ $(rpm -q --qf '%{VERSION}-%{RELEASE}' norma) == "$norma_version-1.el8" ]]
rpm -V norma
dnf --disablerepo='*' -y remove norma
if rpm -q norma >/dev/null 2>&1; then echo 'RPM was not removed.' >&2; exit 1; fi
[[ ! -e /usr/bin/norma && -f /var/cache/norma/layout-rpm-test.json && -f /etc/norma/local-test.conf ]]
printf 'RPM checks passed: install/query/verify/upgrade/downgrade/remove, bidirectional tar conflict and cache/config preservation.\n'
