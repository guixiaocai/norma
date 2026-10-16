# 部署管理与在线排查（v0.5）

[English](operations.md) | [中文 README](../README.zh-CN.md)

**主要部署目标是 Rocky Linux 8.10 x86_64。** 请安装在实际提供 NFS 导出的
服务端；在客户端安装不能通过本机探针查看远端服务端。原生运行不需要目标机器
安装 Go、clang、bpftrace 或 libbpf 动态库。是否支持仍取决于内核能力和 NFSD
布局，详见 [兼容要求](compatibility.md)。

## Rocky、AlmaLinux、RHEL 优先使用 RPM

RPM 适合统一分发、查询文件和版本、升级与卸载。Linux x86_64 包中包含静态
程序、运行辅助脚本、节点配置示例、中英文文档和许可证。在 NFS 服务端安装
已经通过 SHA256 校验的发布包：

```bash
sudo dnf install ./norma-0.5.0-1.el8.x86_64.rpm
rpm -q norma
rpm -ql norma
norma --demo
sudo norma --doctor
sudo norma
```

RPM 安装路径为 `/usr/bin/norma` 和 `/usr/share/norma`。Doctor 报告缺少布局时，
请按下文 [检查服务端并准备布局](#检查服务端并准备布局) 操作。

升级：`sudo dnf upgrade ./norma-新版本-1.el8.x86_64.rpm`；回滚：
`sudo dnf downgrade ./norma-旧版本-1.el8.x86_64.rpm`。隔离环境请保留旧 RPM。
`sudo dnf remove norma` 删除包管理的文件，保留布局缓存和运维人员生成的输出。
RPM 与 tar 安装不要覆盖混用；切换前用原安装方式卸载。包面向 EL8 用户态，
Rocky、AlmaLinux 和 RHEL 仍需满足相同的 NFSD 内核/布局兼容条件。

## tar 安装的升级、回滚与卸载

在线和本地安装均支持 `--prefix`，`DESTDIR` 继续用于打包暂存。新安装器在
`<prefix>/share/norma/.install/manifest.tsv` 保存相对路径、权限和 SHA256
清单；升级前保存完整受管理内容的快照，包括程序、辅助脚本和文档。

```bash
curl -fsSL https://raw.githubusercontent.com/guixiaocai/norma/main/install.sh -o install-norma.sh
sudo bash install-norma.sh --version v0.5.0
norma --version
norma --demo
norma-manage list
sudo norma-manage rollback 快照ID
sudo norma-manage uninstall
```

省略 `--version` 时选取最新稳定版。脚本在解压前用 `SHA256SUMS` 校验发布包，
支持 curl 或 wget，默认安装到 `/usr/local`。选项见 `bash install-norma.sh --help`。
`--demo` 用合成数据预览真实界面，无需 root、NFSD 或探针，按 `q` 退出。
[README](../README.zh-CN.md#界面与统计) 中有明确标记的演示图。

从旧 v0.4 tar 安装升级时，首次加 `--adopt-legacy`，让安装器识别并记录旧 norma
文件，再进行替换；遇到无关文件不会直接接管。每次回滚前也先保存当前版本，
便于再次恢复。程序通过原子替换升级，正在运行的旧进程继续使用已打开的程序；
重启监控后使用新版本。管理操作会加锁，但多个文件的替换不是文件系统级事务。

默认卸载保留布局缓存、安装快照以及用户自行保存的 JSON/日志。受管理但被本地
修改的文件也会保留。卸载或回滚到没有管理器的旧版本后，可从新解压的 v0.5
发布包恢复管理：

```bash
bash ./tools/manage.sh --prefix /usr/local list
sudo bash ./tools/manage.sh --prefix /usr/local rollback 快照ID
```

`uninstall --purge-data` 另外清理 norma 安装快照，以及系统安装前缀对应的内置
`layout-*.json` 缓存；不会搜索并删除用户指定的 `--layout` 文件、重定向保存的
JSON 或其他文件。监控的滚动统计保存在内存中，程序不会自动把监控历史写盘。

安装到个人目录：

```bash
bash install-norma.sh --version v0.5.0 --prefix "$HOME/.local"
# 本地发布包可用：./tools/install.sh --prefix "$HOME/.local"
export PATH="$HOME/.local/bin:$PATH"
norma --demo
sudo "$HOME/.local/bin/norma" --doctor
sudo "$HOME/.local/bin/norma"
```

该前缀的安装和演示无需 root，实际采集仍需 BPF/探针权限。sudo 的 PATH 可能
不包含个人目录，因此使用完整路径；需要准备布局时还应明确程序位置：

```bash
sudo env NORMA_BIN="$HOME/.local/bin/norma" "$HOME/.local/share/norma/tools/prepare-layout.sh"
```

## 一次准备离线部署资料

在联网机器下载指定版本，并把部署资料放进一个目录：

```bash
./tools/bundle-offline.sh --version v0.5.0 \
  --kernel 4.18.0-553.el8_10.x86_64 --format rpm \
  --output ./norma-offline-v0.5.0
```

省略 `--format rpm` 时选择 tar 包。目录中包括已校验的发布文件、上游校验清单、
文档、离线安装入口，以及所填目标内核的依赖清单。内核参数应取目标服务器
`uname -r` 的完整结果，不能填下载机器的内核。EL8 布局辅助依赖列出 gcc、make
及精确版本的 kernel-devel；已有可用 BTF/布局时，原生运行无需这些构建包。

该入口不会自动下载依赖 RPM。进入隔离网络前，请从目标发行版正常软件源准备
相应系统包及其依赖，再整体搬运目录并遵循内置说明。目录不包含适用于任意内核
的布局，也不会自动配置或启动 NFS 服务。

```bash
cd norma-offline-v0.5.0
sha256sum -c BUNDLE-SHA256SUMS
sudo bash install-offline.sh
```

离线入口会再次验证目录。RPM 模式禁用远程软件源，应提前在本地准备缺失依赖；
tar 模式接受本地安装器同样的 `--prefix`、`--adopt-legacy` 和 `--prepare-layout`。

简单手动搬运 tar 包时，从同一个 Release 下载发布包及 `SHA256SUMS`，先校验
再解压安装：

```bash
sha256sum --ignore-missing -c SHA256SUMS
tar -xzf norma-v0.5.0-linux-amd64.tar.gz
cd norma-v0.5.0-linux-amd64
sudo ./tools/install.sh
```

所下载归档显示 `OK` 后才继续。本地安装脚本无需联网；布局准备还需预先提供
下述匹配的系统包。

## 检查服务端并准备布局

`sudo norma --doctor` 检查系统/架构、运行内核、活动 NFSD 线程、BTF/缓存/指定
布局、kallsyms 可读时所需符号，以及所选 bpftrace 版本。必要检查失败返回非零
状态；root 身份与 lockdown 为提示项。它不会加载 BPF、挂接探针、安装系统包
或更改服务，检查通过不代替实机挂接与工作负载核对。Doctor 输出普通检查单，
不能与 `--json` 或 `--demo` 同用。

NFS 服务端应已配置并提供导出。受支持的 `4.18.0-*.el8*` 内核缺少可用 BTF 时，
安装 gcc、make 和 **与正在运行内核完全一致** 的开发头文件，再准备布局：

```bash
sudo dnf install gcc make "kernel-devel-$(uname -r)"
sudo /usr/share/norma/tools/prepare-layout.sh       # RPM 安装
# 默认 tar 前缀请改用 /usr/local/share/norma/tools/prepare-layout.sh
sudo norma --doctor
```

辅助脚本把字段偏移常量编译成未链接对象，生成
`/var/cache/norma/layout-<当前内核版本>.json`，不会加载内核模块、更改驱动或
重编译 norma。其固定 EL8 函数模型会拒绝不匹配的头文件和其他内核系列。软件源
没有匹配 kernel-devel 时，应从正常 Rocky 软件源流程取得精确版本，或在获准
更新并重启到新内核后使用其开发包，不要代用其他版本。内核更新后若仍需此方案，
应重新生成布局。

依赖就绪后，tar 安装也可明确加 `--prepare-layout`：
`sudo bash install-norma.sh --version v0.5.0 --prepare-layout`，或在发布包内执行
`sudo ./tools/install.sh --prepare-layout`。脚本不自动安装系统包。已有经过审查的
布局时可明确检查和使用：

```bash
sudo norma --doctor --layout /path/to/layout.json
sudo norma --layout /path/to/layout.json
sudo norma -p tcp                  # TCP 视图
sudo norma -p rdma -i ib0           # RDMA 视图，按 ib0 本机地址筛选
sudo norma --no-color              # 无彩色仪表盘
sudo norma -t -s 60                # 一分钟普通文本
sudo norma --json -s 60            # 一分钟 JSON Lines
```

v0.4 起默认传输为 `all`，原 RDMA 专用命令请加 `-p rdma`。`-i eno2` 未指定
`-p` 时选择 TCP，显式 `-p` 优先。网卡筛选使用本机 IP 而非物理入站接口；IPoIB
上的普通 TCP NFS 仍是 TCP。可选 `--backend bpftrace` 需要 bpftrace >= 0.16
及相同的兼容布局，程序不会自动回退。

## 从异常客户端连续排查

运行 `sudo norma`，用上下键选中客户端，Enter 进入该客户端的文件统计，
`u` 在文件与 UID 明细间切换，Esc 返回。读写行、平均操作大小及所选客户端的
helper 耗时/失败信息帮助定位当前负载。`First` 是本次监控首次观察到活动的
采样时间；无法据此推断 norma 启动前负载何时开始。

| 按键 | 操作 |
|---|---|
| `g` | 切换客户端、文件、UID、GID、服务端、传输和节点组 |
| `/`，Enter | 输入 IP/CIDR 或 `file=名称片段` 并应用 |
| Esc | 取消编辑、从下钻返回，或清除视图筛选 |
| `T` | 切换 all、TCP、RDMA、UDP |
| 传输行上 Enter | 展开该传输的客户端 |
| `o` | 切换字节速度和成功操作次数/s |
| `t` | 双向、只读、只写、合计 |
| `s` | 按当前、均值、峰值、累计量或名称排序 |
| `d` | 在小终端打开所选实体诊断页；d/Esc 返回 |

排序跟随当前指标和方向：READ ops/s 按读操作速度排序，WRITE 累计按写累计排序，
合计峰值取同一采样中读写合计的最大值。切换分组、筛选和下钻会复用已保留的
原始维度，不卸载探针、不清空本次累计。采集保留全部传输，运行中的 `T` 是视图
筛选；因此能够查看切换前的样本。客户端和网卡地址筛选也在用户态应用。

客户端视图同屏显示传输分项；两个采集后端的聚合键均保留真实传输类型。
READ/WRITE 平均操作大小为成功字节累计量÷成功操作次数；吞吐占比相对于当前
筛选总体，并跟随显示指标和读写方向。文件可靠标识仍是 **设备号＋inode**，
父目录/文件名只是有长度限制的可选提示，bpftrace 不提供文件名提示。提示可能
截断、重命名后过时，也不能解决硬链接、inode 重用或完整导出路径识别。

## 用集群名称和角色显示节点

```bash
sudo norma --config /path/to/nodes.json
sudo norma --config /path/to/nodes.json -g nodegroups
```

配置格式见 [examples/nodes.json](../examples/nodes.json)：

```json
{"nodes":{"192.0.2.21":{"name":"compute01","group":"compute"},
          "192.0.2.23":{"name":"login01","group":"login"}}}
```

也可用 `NORMA_CONFIG` 指定配置。客户端名称保留原 IP，名称相同不会自动合并；
只有 `nodegroups` 视图明确按角色汇总。`-N` DNS 名称也保留 IP，配置别名优先。

## 正确解读耗时、错误及主机概览

两个后端在受支持的 NFSD VFS helper 入口/返回取时间，按客户端、服务端和传输
保留读写调用数、失败数及耗时直方图。所选客户端面板显示会话均值、P95、P99
和最大值，JSON 保留累计纳秒和原始桶。分位数是 **直方图上界**；第 i 桶上界
为 2^i 微秒，末桶饱和并以观察到的最大值为界，不是精确的逐操作分位数。
耗时包含失败的 helper 返回；成功字节/操作次数不包含失败。非零 NFS 返回状态
计为失败，即使该客户端没有成功字节，也能看到其错误。

这些时间测量 NFSD VFS helper 内的耗时，包括该区间中的调度等待；不等同于
端到端 RPC、网络/RDMA 时延或持久化磁盘延迟。均值、分位数和最大值为会话
累计统计，与吞吐滚动窗口分开。客户端直方图不能分摊给某个文件或 UID，相关
明细视图明确显示该维度暂无 helper 诊断。

调用数、桶和最大值是实时读取的。桶计数与调用数不一致，或所需最大值缺失时，
相应分位数/最大值标记不可用，JSON 提供有效性字段；后续一致快照可恢复显示。
bpftrace 顺序打印各 map，持续负载下可能更常出现这种情况；不能把不完整直方图
解释为零耗时。

主机概览读取 CPU 忙碌百分比、NFSD 线程数和最繁忙的完整块设备/非回环网卡
速度。CPU 空闲包含 iowait，guest 时间不重复计入；磁盘扇区按 Linux 的 512
字节单位计算。网络 JSON 的 READ/WRITE 字段对应 RX/TX。这些是整机指标，
可能受无关负载或虚拟设备影响，最繁忙设备也可能随采样变化；数据不可读时
保持缺失，演示模式不混入真实主机数据。

## 常见问题

| 现象 | 处理方式 |
|---|---|
| 找不到命令 | RPM 用 `/usr/bin/norma`，默认 tar 用 `/usr/local/bin/norma`，个人目录用完整路径。 |
| 缺少 BTF/布局或头文件不匹配 | 对照 `uname -r` 检查 kernel-devel，运行受支持的 EL8 辅助脚本，再执行 `--doctor`。 |
| NFSD 未就绪 | 确认在实际 NFS 服务端运行，并检查现有服务和导出。 |
| BPF/探针权限错误 | 检查所需权限及主机追踪/安全策略；doctor 不测试实际挂接。 |
| 没有客户端明细 | 产生真实客户端 I/O 并核对传输/地址筛选；未发 RPC 的客户端缓存命中不会显示。 |
| 界面拥挤 | 加宽终端，用 `d` 查看所选实体诊断，或使用 `--no-color`、`-t`、`--json`。 |
| 不支持的 NFSD 签名 | 使用受支持 BTF 或审查过的兼容布局；换后端不能改变函数语义要求。 |

## 容量限制和后续探针

内核 map、快照及用户态历史均有边界，快照不是全局原子读。请检查健康计数，
识别属性读取失败、明细/诊断容量不足及丢失记录；未送达的返回探针无法补回。
独立客户端汇总可在明细 map 满后继续提供信息，但下钻只能展示明细 map
实际保留下来的数据。

大量文件或长窗口可能触发历史采样容量保护。所需历史区间不完整时，受影响的
均值/峰值显示为不可用，JSON 给出有效性标记，健康区域提示；当前速度、累计量
和首次观察时间继续保留，不把部分历史当作完整窗口统计。

GETATTR、LOOKUP、READDIR、CREATE、REMOVE 属于下一批探针。本版先完成
耗时与错误诊断，不把这些元数据操作标记成已经测量；后续每类都需审查函数语义、
兼容范围、失败行为及实际工作负载计数。完整导出路径及重命名/硬链接处理也留待
后续实现。统计边界详见 [指标说明](metrics.md) 与 [架构说明](architecture.md)。
