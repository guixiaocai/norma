# norma v0.4.0

A redesigned terminal dashboard and a shorter deployment path, with Rocky Linux
8.10 x86_64 as the primary target.

![Dashboard with synthetic demo data](https://raw.githubusercontent.com/guixiaocai/norma/v0.4.0/docs/images/dashboard.png)

*Real dashboard rendering with synthetic demonstration data, not production
measurements.*

- **Clear dashboard regions:** header/context, READ and WRITE summaries, session
  overview, scrollable entity table, collector health and keyboard controls.
  Width/height adapt to smaller terminals; `--no-color` or nonempty `NO_COLOR`
  disables color. At least 120 columns shows every metric column.
- **Preview before deployment:** `norma --demo` runs the real frontend with
  deterministic synthetic data, without root, NFSD or a live collector.
- **Read-only deployment checks:** `sudo norma --doctor` reports platform,
  active NFSD, layout and symbol requirements, plus bpftrace dependencies when
  selected. It returns nonzero for failed prerequisites and provides EL8 layout
  guidance. It does not load/attach BPF or validate real workload counts.
- **Simpler installation:** the new root `install.sh` downloads the latest stable
  or a pinned release and verifies its archive against `SHA256SUMS` before
  extraction. Both online and local installers support a custom prefix and
  explicit `--prepare-layout`. Local installation works offline. No automatic
  package installation or NFS service change is performed.
  Atomic binary replacement allows upgrades while an existing monitor runs.
- **Broader default visibility:** plain `sudo norma` now selects all supported
  NFS transports (RDMA/TCP/UDP). **Upgrade action:** add `-p rdma` to commands that
  must remain RDMA-only. `-i` without explicit `-p` continues to select TCP.
- **Documentation included:** English/Chinese quick starts, dedicated Rocky
  Linux 8.10 guides, offline/user-prefix setup, matching-kernel layout
  preparation and a synthetic dashboard image.

Native eBPF remains the default; bpftrace is explicitly selected with
`--backend bpftrace`. Both share the metric and layout contract, with no automatic
fallback. The native runtime does not need Go, clang, bpftrace or a libbpf shared
library; optional EL8 layout preparation needs gcc, make and matching
kernel-devel. Prebuilt releases remain Linux amd64 only. Build/CLI tests do not
establish runtime probe correctness across Rocky kernels or NICs.

## 中文

v0.4.0 重做终端仪表盘并简化部署，主要面向 Rocky Linux 8.10 x86_64。

- **界面分区：** 运行信息、读写概览、会话统计、可滚动明细、健康状态和快捷键
  独立展示；窗口较小时精简布局，至少 120 列可显示全部统计列。支持
  `--no-color` 和非空 `NO_COLOR`。
- **无需部署即可预览：** `norma --demo` 使用合成数据运行真实界面，无需 root、
  NFS 服务或实际采集；文档中的图片也明确标记为演示。
- **只读检查：** `sudo norma --doctor` 检查平台、活动 NFSD、布局、符号及所选
  后端依赖，必要检查失败时返回非零状态，并给出 EL8 布局准备提示。它不实际
  加载/挂接 BPF，也不代替真实工作负载验证。
- **简化安装：** 新增根目录下载脚本，支持最新稳定版、固定版本、SHA256 校验、
  个人目录；本地安装支持离线，两者均可显式 `--prepare-layout`。不会自动
  安装系统包或修改 NFS 服务；原子替换可执行文件，升级时旧进程可继续运行。
- **默认监控全部 NFS 传输：** `sudo norma` 从 RDMA 默认改为 `all`。
  **升级注意：** 原有 RDMA 专用命令请补 `-p rdma`；只指定 `-i` 仍选择 TCP。
- **完善文档：** 增加 Rocky Linux 8.10 中英文指南、离线/个人目录安装、匹配
  内核布局准备和界面图，文档随程序安装。

默认仍为原生 eBPF，bpftrace 需用 `--backend bpftrace` 明确选择，不自动回退。
原生运行时无需编译工具，EL8 可选布局准备需要 gcc、make 和匹配的 kernel-devel。
发布架构仍为 Linux amd64；构建和 CLI 测试不表示已经在所有目标内核/网卡上
验证探针或工作负载计数。
