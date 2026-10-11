# NFSmon：NFS 服务端流量监控

`nfstop` 与 `nfsdtop` 使用同一套类似 iftop 的终端界面。前者直接挂接 eBPF
程序，后者调用 bpftrace；默认按计算节点显示 **NFS over RDMA** 的读写速度。

## 安装与运行

Release 包含 `bin/nfstop`、`bin/nfsdtop`、安装工具、说明和许可证。

```bash
sudo ./tools/install.sh
sudo nfstop
sudo nfsdtop
```

你当前的 Rocky 8 缺少可用模块 BTF 时，先从运行内核的头文件生成布局数据：

```bash
sudo dnf install -y gcc make "kernel-devel-$(uname -r)"
sudo ./tools/prepare-layout.sh
sudo nfstop

# nfsdtop 还需要 bpftrace，nfstop 不需要：
sudo dnf install -y bpftrace
sudo nfsdtop
```

安装后也可执行 `/usr/local/share/nfsmon/tools/prepare-layout.sh`。
升级 EL8 内核后重新生成布局即可，**无需重编译两个二进制，也无需更换 CX3/CX4
驱动**。程序本身不再限定 `4.18.0-553.el8_10`。对应的头文件必须属于正在运行
的内核，这是结构偏移的数据要求，不是软件只支持一个版本。

## 常用命令

两套工具参数完全一致，把 `nfstop` 替换为 `nfsdtop` 即可。

| 命令 | 含义 |
|---|---|
| `sudo nfstop` | 默认 NFS/RDMA、每客户端、1 秒采样 |
| `sudo nfstop -p tcp` | 普通 TCP NFS |
| `sudo nfstop -i eno2` | 默认转为 TCP NFS，筛选本机 eno2 上的 IP |
| `sudo nfstop -p rdma -i ib0` | 指定 ib0 本机地址的 NFS/RDMA |
| `sudo nfstop -p all` | 所有 NFS 传输，包括 TCP、UDP、RDMA |
| `sudo nfstop -w 10m` | 1 分钟与 10 分钟平均，10 分钟峰值 |
| `sudo nfstop -b` | 自动 bit/s、kbit/s、Mbit/s、Gbit/s |
| `sudo nfstop -F 40.0.0.0/16` | 只看该客户端网段；也接受单个 IP |
| `sudo nfstop -g files` | 按文件的设备号和 inode 查看 |
| `sudo nfstop -g users` | 按 UID 查看；groups 按 GID，servers 按服务端地址 |
| `sudo nfstop -t -s 120` | 文本输出，120 秒后退出 |
| `sudo nfstop --json` | JSON Lines，原始字节与 B/s，包含操作计数和 ops/s |

`-i` 是按网卡的本机 IP 筛选 NFS 服务请求，并非抓取该网卡上所有协议的流量。
省略所有参数就是 RDMA；`-i` 且未指定 `-p` 时选择 TCP。普通 NFS 即使通过
IPoIB 传输，仍应选 TCP。历史环境变量 `NFSMON_TRANSPORT` 仍保留兼容，显式
`-p` 优先。

## 界面与统计

默认每个客户端显示 READ、WRITE 两行，列为：

| 列 | 定义 |
|---|---|
| NOW | 最近一个采样间隔的速度 |
| AVG 1m | 过去 1 分钟平均速度 |
| AVG 5m | 过去 5 分钟平均速度，窗口可用 `-w` 修改 |
| PEAK 5m | 同一窗口中最高的单次采样速度 |
| TOTAL | 从本次启动起累计完成的数据量 |

READ 是计算节点从服务端读取；WRITE 是计算节点写入服务端。平均值按实际
经过时间加权，包含无 I/O 的时间。刚启动未满窗口时只除以已观察的时间，不
假装已有完整的 5 分钟历史。峰值是默认 1 秒采样的峰值，不是无法观测的瞬时
硬件峰值。`-r 2s` 可改采样间隔。

默认单位随数值自动切换 B/s、KiB/s、MiB/s、GiB/s；`B` 键切换字节/比特。
`o` 键或启动参数 `--ops` 可显示完成的操作次数与 ops/s；排序仍按数据吞吐。
`s` 切换排序；`t` 切换双向/读/写/合计；`p` 暂停显示但继续采样；方向键滚动。

**Ctrl+C 或 q 退出后恢复进入程序前的终端画面和输入模式。** 使用独立的终端
备用屏幕，不执行 `clear`，不清除 scrollback，不覆盖原来的命令输出。`-t` 或
重定向时输出普通文本；终端若不支持备用屏幕，建议使用 `-t`。

统计的是 NFSD 服务端成功完成的逻辑读写字节，包含服务端缓存命中的读取。
localhost 作为客户端访问 whale 的挂载不会直接混入统计。它不是物理磁盘 I/O、
IB 端口线上流量，也不包含协议头和重传开销。

## 适配范围

主要范围是 **x86_64 Rocky/Alma/RHEL 8 的标准 4.18 内核系列**，同系列补丁
更新通过本机头文件重新生成布局。具有完整 BTF 类型与兼容函数签名的其他
内核走自动适配；并不宣称所有 5.x/6.x 内核均已验证可用。

软件按字段、函数签名、探针能力判断，不按某个固定 `uname -r` 拒绝运行。
完整范围和扩展方法见 [docs/compatibility.md](docs/compatibility.md)。
IPv4/IPv6 地址解析包含在两个后端中；本次未做现场验证。

## GitHub 开源与 Release

源代码包可以直接作为 GitHub 仓库根目录，包含 README、许可证、上游声明、
贡献说明、问题模板、构建及 Release 工作流。当前版本号为 `v0.2.0`。

```bash
make build
make release VERSION=v0.2.0
```

推送 `v0.2.0` 这样的标签后，GitHub Actions 自动编译两套工具，创建 Release，
上传 Linux amd64 的 `.tar.gz`、`.zip` 和 SHA256。详细首次发布步骤见
[docs/publishing.md](docs/publishing.md)。Go 模块使用本地 `nfsmon` 名称，不冒用
上游仓库路径；公布仓库后可按文档改为实际 GitHub 模块路径。

`nfstop` 是嵌入 eBPF 对象的静态 Go 可执行文件；`nfsdtop` 是静态界面和启动器，
**仍需要目标机器安装 bpftrace >=0.16**。没有把 bpftrace 伪装成独立静态工具。

本次按要求不执行测试、挂接探针或生成负载，只编译交付二进制。工作流也只
构建与发布；兼容范围是实现范围，不能写成跨版本测试通过记录。
