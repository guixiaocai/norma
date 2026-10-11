# norma：NFS 服务端 I/O 监控

[English](README.md) | [简体中文](README.zh-CN.md) | [下载 Release](https://github.com/guixiaocai/norma/releases)

**像 iftop 一样，在终端中查看每个客户端对 NFS 服务端的读写吞吐，支持 RDMA、TCP 和 UDP。**

`norma` 用于定位共享存储的客户端 I/O、比较突发流量与滚动平均。名称取自
**NFS over RDMA**，对应默认传输方式；也支持 TCP、UDP NFS。

现在只有一个可执行文件和一个命令：**`norma`**。默认使用内嵌的原生 eBPF
后端，`--backend bpftrace` 显式选择 bpftrace 后端。两者共用参数、界面和
指标定义，统计 NFSv3/v4 在 NFSD VFS 层成功完成的读写字节与操作次数。

## 项目亮点

- **按客户端定位共享存储 I/O。** 查看完成的读写数据量，包括服务端缓存命中
  的读取；支持 RDMA、TCP、UDP，客户端 IP/CIDR、服务端网卡本机地址筛选，
  以及客户端、服务端、文件、UID、GID 分组。
- **一个命令，两种采集后端。** 原生后端无需在目标机器安装 bpftrace；已有
  bpftrace 环境也可显式选择另一条采集路径。部署差异见下表。
- **按内核能力适配。** 从运行时 BTF 解析字段偏移和已识别的 NFSD 函数签名，
  或通过 `--layout` 提供经过审查的匹配布局，无需因布局数据变化重编译程序。
- **同时看当前、均值、峰值和累计量。** 提供 1 分钟与可配置窗口平均、窗口内
  单次采样峰值及会话累计量；均值包含空闲时间，暂停显示时仍持续采样。
- **交互查看与脚本采集兼顾。** 自动缩放单位、字节/比特切换、退出时恢复
  终端；支持普通文本、JSON Lines 和限定采集时长，JSON 保留字节与操作指标。

## 安装与运行

在 **NFS 服务端** 下载并解压 `norma-vX.Y.Z-linux-amd64` Release。
包内包含 `bin/norma`、安装工具、文档和许可证。

```bash
sudo ./tools/install.sh
sudo norma                         # 默认原生 eBPF、NFS/RDMA、客户端视图
sudo norma --backend native        # 显式选择默认的原生后端
sudo norma -p tcp                  # 普通 TCP NFS
sudo norma --backend bpftrace      # 另一后端，需要 bpftrace >= 0.16
```

NFS 服务需已经运行。两种后端都要求 Linux x86_64、BPF 加载与 kprobe/返回
探针挂接权限，以及符合要求的 NFSD 函数签名和布局数据。程序自动尝试运行时
BTF；若无法解析所需探针，可提供与运行内核匹配、经过审查的布局文件：

```bash
sudo norma --layout /path/to/layout.json
sudo norma --backend bpftrace --layout /path/to/layout.json
```

部署到新内核前请阅读 [兼容要求](docs/compatibility.md)。切换后端不能解决
未知 NFSD 函数语义或错误的结构偏移。程序**不会自动回退到另一后端**。

## 两种后端：异同、优劣与选择

| 对比项 | 原生 eBPF（默认） | bpftrace（`--backend bpftrace`） |
|---|---|---|
| 共同能力 | NFSv3/v4 完成字节和操作次数；RDMA/TCP/UDP 与地址筛选；全部分组；相同界面、窗口、文本和 JSON 输出 | 相同 |
| 目标机器部署 | 一个内嵌 BPF 对象的 `norma` 可执行文件 | 同一个 `norma`，另需安装 `bpftrace >= 0.16` 及其正常工作的运行依赖 |
| 运行时编译工具 | 不需要 Go 编译器、clang 或 libbpf 动态库 | 启动时由 bpftrace 编译生成的脚本，其版本和运行环境需支持用到的探针与功能 |
| 采集路径 | Go 直接挂接探针、读取累计 BPF map | Go 管理 bpftrace 子进程并解析它输出的累计文本帧 |
| 启动与额外开销 | 无需启动编译子进程、解析文本帧 | 增加脚本编译、子进程启动和文本帧处理 |
| 文件视图 | 设备号/inode，附可选父目录/文件名提示；提示可能截断，重命名后可能过时 | 设备号/inode，无路径名提示 |
| 诊断信息 | 内存读取失败、map 容量不足、在途记录问题、操作失败、文件名提示丢失 | 保留 bpftrace 启动与运行错误，报告操作失败和在途记录覆盖 |
| 主要优势 | 目标机器依赖少，适合日常监控及批量部署 | 适合已有 bpftrace 运维环境，可用其编译与探针错误排查该采集路径 |
| 主要限制 | 受内嵌采集器及其已支持的函数语义约束，失败时需检查诊断与布局 | 依赖目标机器的 bpftrace 环境，并增加子进程；不会扩大两者共用的布局兼容范围 |

**日常使用建议先选原生后端**，尤其是希望直接复制 Release 到多台服务端时。
目标机器已有合适的 bpftrace 环境，或需要排查 bpftrace 采集路径时，可选择
bpftrace 后端。某后端失败后，先查看错误，再显式尝试另一后端。

项目尚未通过基准测试证明某后端具有更低 CPU 占用或更高吞吐；表中的开销
区别来自实现方式。两者都使用容量有限的 map，快照均非全局原子快照，内核
未送达的返回探针事件无法补回。bpftrace 的排队快照若被合并，累计总量保留，
但峰值时间分辨率会降低。实现细节见 [架构说明](docs/architecture.md)。

## 常用命令

| 命令 | 含义 |
|---|---|
| `sudo norma` | 默认原生 eBPF、NFS/RDMA、每客户端、1 秒采样 |
| `sudo norma --backend bpftrace` | 显式使用 bpftrace，其他参数通用 |
| `sudo norma -p tcp` | 普通 TCP NFS |
| `sudo norma -i eno2` | 默认选择 TCP NFS，按 eno2 的本机 IP 筛选 |
| `sudo norma -p rdma -i ib0` | 按 ib0 本机地址筛选 NFS/RDMA |
| `sudo norma -p all` | TCP、UDP、RDMA 所有 NFS 传输 |
| `sudo norma -w 10m -b` | 1 分钟与 10 分钟平均、10 分钟峰值，以 bit/s 显示 |
| `sudo norma -F 40.0.0.0/16` | 按客户端网段筛选，也接受单个 IP |
| `sudo norma -g files` | 按文件的设备号和 inode 查看 |
| `sudo norma -g users` | 按 UID 查看；`groups` 按 GID，`servers` 按服务端地址 |
| `sudo norma --backend bpftrace -g users -p tcp` | bpftrace 后端按 UID 查看 TCP NFS |
| `sudo norma -t -s 120` | 文本输出，120 秒后退出 |
| `sudo norma --json -s 60` | JSON Lines，包含字节、B/s、操作计数与 ops/s |

`-i` 按网卡的本机 IP 筛选 NFS 服务请求，并非抓取该网卡上所有协议的流量。
省略参数默认 RDMA；`-i` 且未指定 `-p` 时选择 TCP。普通 NFS 即使通过
IPoIB 传输，仍应选 TCP。

## 界面与统计

默认每个客户端显示 READ、WRITE 两行：READ 表示客户端从服务端读取，
WRITE 表示客户端写入服务端。

| 列 | 定义 |
|---|---|
| NOW | 最近一个采样间隔的速度，默认 1 秒，可用 `-r` 修改 |
| AVG 1m | 已观察的过去 1 分钟平均速度，包含无 I/O 的时间 |
| AVG 5m | 可配置窗口平均速度，`-w` 最大支持 24 小时 |
| PEAK 5m | 同一窗口中最高的单次采样速度 |
| TOTAL | 从本次启动起累计完成的数据量 |

均值按实际时间加权。刚启动未满窗口时只除以已观察的时间，不假装已有完整
历史。峰值是采样峰值，不是瞬时硬件峰值。指标定义见 [metrics](docs/metrics.md)。

默认自动切换 B/s、KiB/s、MiB/s、GiB/s；`B` 切换字节/比特，`-b` 以比特启动。
`o` 或 `--ops` 显示操作次数与 ops/s；`s` 切换排序；`t` 切换双向/读/写/合计；
`p` 暂停显示但继续采样；方向键滚动；`q` 或 Ctrl+C 退出并恢复原终端画面和
输入模式。`-t` 或重定向时输出无终端转义序列的普通文本，`-N` 异步查询名称，
默认显示数字地址。

统计的是 NFSD 服务端成功完成的逻辑读写字节，包含服务端缓存命中的读取，
不包含 RPC/RDMA 协议头、线上重传或物理磁盘 I/O，也不保证客户端已收到
回复或数据已持久化。本机作为客户端访问其他 NFS 服务端的 I/O 不在范围内。

## 构建与发布

普通构建只需 Go >= 1.25.3 和 make。仓库包含已生成的 BPF 对象，不修改
BPF 源码时无需安装 clang：

```bash
make build
./bin/norma --version
make release VERSION=v0.3.0        # 打包还需 tar、zip
```

`make`/`make build` 只生成 **`bin/norma`**，一个静态 Go 可执行文件内包含两种
后端。目标机器使用原生后端无需编译工具；bpftrace 后端另需安装 bpftrace。

修改 BPF 源码后，在构建机安装 clang/LLVM、libbpf 开发头文件和 gcc，并重新
生成 BPF 对象与绑定：

```bash
# Ubuntu 构建依赖示例：
sudo apt-get install clang llvm libbpf-dev gcc make
make generate
make build
```

推送版本标签后，GitHub Actions 构建并发布 Linux amd64 的 `.tar.gz`、`.zip`
与 SHA256；发布步骤见 [publishing](docs/publishing.md)。构建和 CLI 检查不等于
目标内核运行验证，探针挂接、NFS 工作负载计数及跨内核行为需在部署环境验证。

## 许可证与上游

新增代码采用 MIT；原生 BPF 为 MIT OR GPL-2.0；bpftrace 后端为 BSD-2-Clause。
[NOTICE](NOTICE) 和 `licenses/` 保留上游声明与依赖许可证。本项目是下游项目，
上游链接见 NOTICE。
