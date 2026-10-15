# Rocky Linux 8.10 部署指南

[English](rocky-linux-8.10.md) | [中文 README](../README.zh-CN.md)

本版本主要面向 **Rocky Linux 8.10 x86_64**。请在实际提供 NFS 导出的服务端
安装；在客户端安装不能通过本机 NFSD 探针查看远端服务端。预编译原生后端在
目标机器运行时无需 Go、clang、bpftrace 或 libbpf 动态库。

兼容性仍由内核能力、NFSD 函数语义和布局数据决定，不能仅凭发行版名称判断。
本文不表示已经在每个 Rocky 内核或每种网卡上完成实机验证，详见
[兼容要求](compatibility.md)。

## 1. 安装并预览

```bash
curl -fsSL https://raw.githubusercontent.com/guixiaocai/norma/main/install.sh -o install-norma.sh
sudo bash install-norma.sh
norma --version
norma --demo
```

安装脚本选取 GitHub 最新稳定版，下载 Linux amd64 发布包与 `SHA256SUMS`，
校验后安装到 `/usr/local`。下载工具支持 curl 或 wget；没有 curl 时可用
`wget -O install-norma.sh URL` 下载脚本。`bash install-norma.sh --help` 查看选项。
固定版本可执行：

```bash
sudo bash install-norma.sh --version v0.4.0
```

`--demo` 用确定性的合成数据展示真实仪表盘，不加载 BPF，不需要 root，也不
需要正在运行的 NFS 服务。按 `q` 退出。

![合成数据界面演示](images/dashboard.png)

*这是合成数据演示图，不是生产环境实测结果。*

## 2. 检查当前服务端

```bash
sudo norma --doctor
```

此命令只读检查环境和布局准备情况；必要检查失败时返回非零退出状态。它不会
加载 BPF、挂接探针、修改导出、安装系统包或启动服务。检查通过并不证明内核
安全策略一定允许探针挂接，也不代替真实工作负载的计数核对。

检查内容包括系统/架构、运行内核、活动 NFSD 工作线程、BTF/缓存/指定文件的
布局解析，以及 `/proc/kallsyms` 可读时所需 NFSD 符号是否存在；选用 bpftrace
时也检查其版本。root 身份与可见的内核 lockdown 状态为提示项。建议通过 sudo
运行，以接近真实采集时的环境；仅有非 root 提示不会导致检查失败。Doctor 输出
普通文本检查单，不可与 `--json` 或 `--demo` 同时使用。

NFS 服务端应已完成配置并提供导出。运行时 BTF 能解析所需 NFSD 函数及字段时，
可直接运行监控；如果报告缺少可用的布局信息，请按下一步准备 EL8 布局。

## 3. 必要时准备 EL8 布局

对于受支持的 `4.18.0-*.el8*` 内核，缺少可用 BTF 时安装 **与当前运行内核
完全匹配** 的开发头文件、gcc 和 make：

```bash
sudo dnf install -y gcc make "kernel-devel-$(uname -r)"
sudo /usr/local/share/norma/tools/prepare-layout.sh
sudo norma --doctor
```

辅助脚本只把字段偏移常量编译成未链接对象，再生成
`/var/cache/norma/layout-<当前内核版本>.json`。它不会构建或加载内核模块、
更改网卡驱动或重新编译 norma。脚本采用固定、受支持的 NFSD 函数模型，不能
为任意内核自动生成兼容实现；头文件不匹配或不属于其 EL8 范围时会拒绝执行。

安装好上述依赖后，也可明确要求安装脚本同时准备布局：

```bash
sudo bash install-norma.sh --version v0.4.0 --prepare-layout
# 或在解压后的发布包目录中执行：
sudo ./tools/install.sh --prepare-layout
```

安装脚本不会自动安装系统包。若当前软件源找不到完全匹配的 `kernel-devel`，
请按正常的 Rocky 软件源管理流程取得匹配包，或在获准更新并重启到新内核后
使用对应开发包，不要用其他内核版本的头文件代替。内核更新后，若仍需此布局
方案，应重新运行辅助脚本。

已有经过审查的布局文件时，可明确指定：

```bash
sudo norma --doctor --layout /path/to/layout.json
sudo norma --layout /path/to/layout.json
```

## 4. 开始采集

```bash
sudo norma                         # 所有受支持的 NFS 传输
sudo norma -p tcp                  # 仅 TCP
sudo norma -p rdma                 # 仅 RDMA
sudo norma -i eno2                 # TCP，按 eno2 的本机地址筛选
sudo norma -p rdma -i ib0           # RDMA，按 ib0 的本机地址筛选
sudo norma --no-color              # 无彩色界面
sudo norma -t -s 60                # 采集一分钟普通文本
sudo norma --json -s 60            # 采集一分钟 JSON Lines
```

v0.4.0 默认传输由 RDMA 改为 `all`；原有 RDMA 专用脚本请补上 `-p rdma`。
仅指定 `-i` 仍默认选择 TCP，显式 `-p` 优先。网卡筛选依据本机 IP 地址，并非
物理入站网卡；通过 IPoIB 运行的普通 TCP NFS 仍应选 TCP。

默认后端是原生 eBPF。已有 bpftrace 环境可运行
`sudo norma --backend bpftrace`，需 bpftrace >= 0.16，也需要相同的兼容布局与
探针语义。程序不会自动切换后端。

## 离线安装与个人目录安装

在联网机器从 **同一个** [Release](https://github.com/guixiaocai/norma/releases/tag/v0.4.0)
下载 `norma-v0.4.0-linux-amd64.tar.gz` 和 `SHA256SUMS`，复制到服务端：

```bash
sha256sum --ignore-missing -c SHA256SUMS
tar -xzf norma-v0.4.0-linux-amd64.tar.gz
cd norma-v0.4.0-linux-amd64
sudo ./tools/install.sh
sudo norma --doctor
```

确认所下载归档的校验结果为 `OK` 后再解压安装。本地安装脚本不需要联网；
离线生成布局时，还需通过现有软件包分发流程准备 gcc、make 和匹配的
kernel-devel 包。

不希望写入系统目录时：

```bash
bash install-norma.sh --version v0.4.0 --prefix "$HOME/.local"
# 本地发布包可用：./tools/install.sh --prefix "$HOME/.local"
export PATH="$HOME/.local/bin:$PATH"
norma --demo
sudo "$HOME/.local/bin/norma" --doctor
sudo "$HOME/.local/bin/norma"
```

安装到该目录和运行演示均无需 root；实际采集仍需要 BPF/探针权限。sudo 的
PATH 可能不包含个人目录，因此上例使用完整路径。此安装前缀的布局准备还应明确
提供可执行文件路径：

```bash
sudo env NORMA_BIN="$HOME/.local/bin/norma" "$HOME/.local/share/norma/tools/prepare-layout.sh"
```

## 常见问题

| 现象 | 处理方式 |
|---|---|
| 找不到 norma 命令 | 检查安装前缀，尝试 `/usr/local/bin/norma` 或个人目录中的完整路径。 |
| 缺少布局/BTF | 安装匹配的 kernel-devel、gcc、make，运行 EL8 辅助脚本，再执行 `--doctor`。 |
| 头文件版本不一致 | 对照 `uname -r` 检查开发包，不要复用其他内核的布局。 |
| NFSD 未就绪 | 确认在实际 NFS 服务端运行，并检查其已有 NFS 服务及导出。 |
| BPF/探针权限错误 | 使用所需权限并检查主机的追踪/安全策略；`--doctor` 不实际测试挂接。 |
| 界面没有明细 | 从客户端产生真实读写，并检查传输/地址筛选；未发 RPC 的客户端缓存命中不会显示。 |
| 窗口拥挤 | 加宽终端显示完整列；按需使用 `--no-color`、`-t` 或 `--json`。 |
| 不支持的 NFSD 签名 | 使用受支持的运行时 BTF 或经过审查的兼容布局；换后端不能改变共同的函数语义要求。 |

界面统计 NFSD 成功完成的逻辑字节，包括服务端缓存读取，不等同于线上流量或
物理磁盘吞吐。统计边界与采集限制见 [指标说明](metrics.md) 和
[架构说明](architecture.md)。
