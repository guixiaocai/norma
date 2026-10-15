# Changelog

## v0.4.0

- Redesign the terminal UI as a responsive dashboard with separate context,
  READ/WRITE/SESSION overview, entity table, collector health and controls.
  Reduce visible columns on smaller terminals and retain full metrics in
  text/JSON output. Keep scrolling, sorting, direction, operations and pause
  controls, including terminal restoration on exit. Paused views reflow on resize;
  paging follows visible table capacity and scrolling clamps to available rows.
- Add `--no-color` and nonempty `NO_COLOR` support for monochrome terminals.
- Add `--demo`: deterministic synthetic data through the real frontend without
  root, NFSD or a live collector; label the dashboard/text output and emit
  `demo: true` in demo JSON records.
- Add read-only `--doctor` checks for platform, active NFSD, layout availability,
  probe symbols and the selected backend's dependencies, with a nonzero status
  for failed prerequisites and guidance for EL8 layout preparation. These checks
  do not test live BPF attachment or workload accuracy.
- Change the default transport from RDMA to `all` (RDMA/TCP/UDP). Existing
  RDMA-only workflows should pass `-p rdma`. `-i` without `-p` still selects TCP.
- Add a download installer with latest/pinned version selection, SHA256 archive
  verification and a custom installation prefix. Retain local/offline install,
  add explicit `--prepare-layout`, and install documentation with the executable.
- Replace the installed binary atomically so an upgrade can complete while an
  existing monitor keeps running. Test installers and Rocky Linux 8.10 userland
  in CI; the container check does not validate the target kernel's probes.
- Add English/Chinese Rocky Linux 8.10 guides covering matching kernel-devel,
  optional EL8 layout preparation, offline/user-prefix install, diagnostics and
  upgrade behavior. Include a dashboard image rendered from synthetic data.
- Keep Linux x86_64 and the existing NFSD layout/function contract; no automatic
  backend fallback or unverified cross-kernel/runtime performance claim.

### 中文更新说明

- 终端界面按运行信息、读写/会话概览、明细、健康状态和快捷键分区，随终端尺寸
  调整显示；窄窗口精简列，文本和 JSON 保留完整统计。
  暂停后可缩放重排，翻页按可见容量移动，滚动不会越界卡住。
- 新增 `--no-color` / 非空 `NO_COLOR` 无彩色模式，以及无需 root、NFS 或探针的
  `--demo` 合成数据预览。
  文本输出标记 DEMO，演示 JSON 带 `demo: true`，便于区分真实采集。
- 新增只读 `--doctor` 部署检查，必要条件缺失时返回非零状态，并提供 EL8 布局
  准备提示；该检查不等于实际挂接与工作负载验证。
- 默认传输由 RDMA 改为 `all`；仅需 RDMA 的原有命令请加 `-p rdma`。
  `-i` 未指定 `-p` 时仍选择 TCP。
- 新增带 SHA256 校验的最新版/固定版下载安装，支持个人目录、离线安装及显式
  `--prepare-layout`；随程序安装文档。
- 安装时原子替换程序，允许旧监控进程继续运行；CI 增加安装测试与 Rocky
  Linux 8.10 用户态检查，容器检查不代替目标内核探针验证。
- 补充 Rocky Linux 8.10 中英文部署指南、升级说明和合成数据界面图；继续以
  Linux x86_64、兼容 NFSD 函数及正确布局为支持边界。

## v0.3.0

- Unify the public CLI and build/install/release output as `norma`.
- Include both collectors in one binary: native eBPF by default, bpftrace through explicit `--backend bpftrace`, with no automatic fallback.
- Document shared metrics, backend differences, deployment requirements and selection in English and Chinese.
- Replace obsolete distribution/version-focused guidance with capability-based NFSD/layout requirements.
- Use checked-in BPF objects for ordinary Go builds; add explicit `make generate` for BPF source updates.
- Use `norma` installation/cache paths and retain upstream notices.

## v0.2.0

- Replace exact-kernel executable restrictions with capability-based layout and function profiles.
- Add runtime BTF and a constrained header-offset layout route; keep executable builds independent of the target patch release.
- Share an iftop-style frontend between the historical native `nfstop` and bpftrace `nfsdtop` executables.
- Add rolling 1-minute and configurable-window averages, sample-window peaks and automatic units.
- Restore the original terminal after q/Ctrl+C; isolate bpftrace process cleanup.
- Simplify transport/interface/output flags; default to NFS/RDMA client view.
- Retain UID/GID/server/file grouping and raw operation counters/rates in JSON.
- Package open-source notices, documentation and build/release workflows.
- Compile Linux amd64 binaries without a cross-kernel runtime-support claim.
