# SPDX-License-Identifier: MIT
%global debug_package %{nil}
%global __os_install_post %{nil}
%global _build_id_links none
%global _binary_payload w9.gzdio

Name:           norma
Version:        %{norma_version}
Release:        %{norma_release}%{?dist}
Summary:        Native eBPF NFS server traffic monitor
License:        MIT AND BSD-2-Clause AND BSD-3-Clause AND Apache-2.0
URL:            https://github.com/guixiaocai/norma
Source0:        norma-%{version}.tar.gz
ExclusiveArch:  x86_64
AutoReqProv:    no
Requires:      bash
Requires:      coreutils

%description
norma displays NFS server traffic using a statically linked native eBPF
collector, with a terminal dashboard and JSON output. An optional bpftrace
backend is available separately. No services, mounts or kernel settings are
changed by installing this package. Layout preparation is an explicit step
and may need gcc, make and development headers for the running EL8 kernel.

%prep
%setup -q

%build
# Release binary was built with CGO_ENABLED=0 and embedded BPF objects.

%install
mkdir -p %{buildroot}%{_bindir} %{buildroot}%{_datadir}/norma/tools
install -m 0755 bin/norma %{buildroot}%{_bindir}/norma
install -m 0755 tools/prepare-layout.sh %{buildroot}%{_datadir}/norma/tools/prepare-layout.sh
install -m 0644 LICENSE NOTICE README.md README.zh-CN.md CHANGELOG.md VERSION %{buildroot}%{_datadir}/norma/
cp -R docs licenses %{buildroot}%{_datadir}/norma/
mkdir -p %{buildroot}%{_datadir}/norma/examples
if [ -d examples ]; then cp -R examples/. %{buildroot}%{_datadir}/norma/examples/; fi

%pre
# Refuse parallel tar/RPM installations: /usr/local otherwise shadows /usr/bin.
if [ -e /usr/local/bin/norma ] || [ -L /usr/local/bin/norma ] ||
   [ -e /usr/local/share/norma/.install/manifest.tsv ] ||
   [ -e /usr/share/norma/.install/manifest.tsv ]; then
    echo "norma: tar installation detected; uninstall it with norma-manage first (data is preserved)." >&2
    echo "For legacy v0.4 installs, migrate with --adopt-legacy before uninstalling, or move the old installation aside." >&2
    exit 1
fi
if { [ -e /usr/bin/norma ] || [ -L /usr/bin/norma ]; } && ! rpm -qf /usr/bin/norma >/dev/null 2>&1; then
    echo "norma: /usr/bin/norma is not owned by RPM; move it aside before installing this package." >&2
    exit 1
fi

%files
%{_bindir}/norma
%dir %{_datadir}/norma
%license %{_datadir}/norma/LICENSE
%{_datadir}/norma/NOTICE
%{_datadir}/norma/VERSION
%doc %{_datadir}/norma/README.md
%doc %{_datadir}/norma/README.zh-CN.md
%doc %{_datadir}/norma/CHANGELOG.md
%doc %{_datadir}/norma/docs
%{_datadir}/norma/licenses
%{_datadir}/norma/examples
%dir %{_datadir}/norma/tools
%{_datadir}/norma/tools/prepare-layout.sh
# /var/cache/norma and user configuration are deliberately not package-owned.
