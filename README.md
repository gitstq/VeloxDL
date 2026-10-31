<div align="center">

# ⚡ VeloxDL

**自适应并行下载加速器 · Adaptive Parallel Download Accelerator**

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green?style=flat-square)](LICENSE)
[![Zero Dependencies](https://img.shields.io/badge/dependencies-0-brightgreen?style=flat-square)](go.mod)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux-black?style=flat-square)](https://github.com/gitstq/VeloxDL/releases)

<table align="center">
  <tr>
    <td><b>简体中文</b></td>
    <td><a href="README.zh-TW.md"><b>繁體中文</b></a></td>
    <td><a href="README.en.md"><b>English</b></a></td>
  </tr>
</table>

</div>

---

## 🎯 一句话介绍

> 单条 TCP 连接跑不满你的带宽 —— VeloxDL 把文件切碎、撒到多条连接（甚至多个镜像）上并行拉取，并**实时感知吞吐变化自动增减并发**，让你的下载真正吃满带宽。

```bash
$ veloxdl -n 32 https://mirror.example.com/ubuntu-24.04.iso

⬇️  VeloxDL 1.0.0  https://mirror.example.com/ubuntu-24.04.iso  ->  ubuntu-24.04.iso
[████████████████████░░░░░░░░]  71.3%  1.4 GiB / 2.0 GiB   38.7 MiB/s  eta 17s  conns 32/32
```

---

## ✨ 核心特性

### 🚀 自适应并发（真正的差异化亮点）

大多数下载器的并发数是**写死的**——你填 16 它就永远 16。但真实链路的最优并发是随时间变化的：开局要慢启动，中途可能因为拥塞该降速，空闲时又该往上探。

VeloxDL 内置了一个类似 TCP 拥塞控制的调参器：**每 500ms 采样一次吞吐，提速超过 8% 就试探性加一条连接，掉速超过 12% 就退一条。** 你给的 `-n` 是天花板，不是固定值。

```
吞吐 ↑ 8%  →  conns + 1   （试探：链路还有余量吗？）
吞吐 ↓ 12% →  conns - 1   （回退：已经饱和，别再挤了）
```

### 🧩 碎片化分片，消灭"拖尾效应"

如果分片数恰好等于连接数，最慢的那条连接会让整个下载卡在最后的 1/N 上。VeloxDL 默认切出 **连接数 × 4** 个分片，并且**按剩余大小降序入队**——快的连接自动多领任务，收尾时只剩一堆小块，尾部空窗期被大幅压缩。

### 🌐 多镜像聚合

同一个文件有多个源？把它们都给 VeloxDL，不同分片会**轮流打到不同镜像**上。某个镜像挂了，失败分片会自动换源重试，全程无感。

```bash
veloxdl https://a.example/x.iso --mirror https://b.example/x.iso --mirror https://c.example/x.iso
```

> 安全兜底：镜像之间会**强制校验 Content-Length 与 ETag 是否一致**，不一致的镜像直接剔除——否则跨源拼接会产出字节错位的文件。

### 💾 可中断的断点续传

下载到一半 Ctrl+C？VeloxDL 会先把进度写进状态文件再退出。下次 `-c` 接着下，**不会浪费一个字节**。

- 状态文件采用 **临时文件 + rename 原子写入**，进程被 kill 也不会留下半截文件
- 续传前会做**四重校验**（URL / 文件大小 / 分片规划 / ETag），任一不符就老老实实重下
- **只有校验通过后才删除状态文件**，保证中断后一定能续上

### 🎛️ 真正生效的限速

常见的令牌桶实现在多分片并行时会被绕过：8 个分片同时发现自己有令牌，就同时放行，实际速率变成 8 倍。

VeloxDL 用**全局配额时间线**：每个分片要发数据前，先在时间线上预约一段 `n/rate` 的时长，排到队尾。无论多少分片并行，总吞吐都被严格压在设定值之下。

```bash
veloxdl --rate-limit 2M https://example.com/big.zip   # 只占 2 MiB/s，不打扰视频会议
```

### 🔐 完整性校验

并行分片 + 乱序落盘一旦出 bug，几乎必然表现为**文件内容损坏而不是程序报错**。所以 VeloxDL 支持下载后自动校验：

```bash
veloxdl --checksum sha256:9f86d081884c7d659a2feaa0c55ad015... https://example.com/f.bin
```

不匹配直接报错退出（退出码 1），绝不让损坏文件悄悄通过。

---

## 🚀 快速开始

### 环境要求

| 项目 | 要求 |
|------|------|
| **Go** | 1.21 或更高（仅从源码构建时需要） |
| **操作系统** | macOS / Windows / Linux |
| **架构** | amd64 / arm64 |
| **第三方依赖** | **无**（纯 Go 标准库实现） |

### 方式一：go install（推荐）

```bash
go install github.com/gitstq/VeloxDL/cmd/veloxdl@latest
```

安装后确认：

```bash
veloxdl --version
```

### 方式二：下载预编译二进制

前往 [Releases 页面](https://github.com/gitstq/VeloxDL/releases) 下载对应平台的可执行文件，解压后即可运行，无需任何运行时依赖。

```bash
# Linux / macOS 示例
tar -xzf veloxdl_linux_amd64.tar.gz
chmod +x veloxdl_linux_amd64
sudo mv veloxdl_linux_amd64 /usr/local/bin/veloxdl
```

### 方式三：从源码构建

```bash
git clone https://github.com/gitstq/VeloxDL.git
cd VeloxDL

# 只构建当前平台
make build

# 或：交叉编译全部平台（产物在 dist/）
make dist
```

### 第一个下载

```bash
veloxdl https://speed.hetzner.de/1GB.bin
```

---

## 📖 详细使用指南

### 命令参数

```
用法:
  veloxdl [选项] <URL> [URL...]

常用选项:
  -o <path>        输出文件路径（默认从 URL 推断）
  -n <num>         最大并发连接数（默认 16）
  -c               断点续传，从上次中断处继续
  --checksum <s>   期望校验和，如 sha256:<hex>，不匹配则报错退出
  --rate-limit <s> 限速，支持 500K / 2M / 10M 或纯字节数
  --retries <num>  单个分片的最大重试次数（默认 5）
  --timeout <dur>  单个分片请求超时（默认 60s）
  -H <header>      自定义请求头，可重复传入
  -A <ua>          自定义 User-Agent
  -q               静默模式，只输出最终文件路径
  --mirror <url>   附加镜像地址，可重复传入
  --version        打印版本号
```

### 典型场景

**① 日常加速下载**

```bash
veloxdl -n 16 -o dataset.tar.gz https://example.com/dataset.tar.gz
```

**② 多镜像聚合（带宽叠加）**

```bash
veloxdl -n 32 https://a.cdn/x.iso \
  --mirror https://b.cdn/x.iso \
  --mirror https://c.cdn/x.iso
```

**③ 中断后继续**

```bash
# 第一次（Ctrl+C 中断）
veloxdl -o big.iso https://example.com/big.iso
# 进度已保存，接着下
veloxdl -c -o big.iso https://example.com/big.iso
```

**④ 限速下载（不打扰其他应用）**

```bash
veloxdl --rate-limit 1M https://example.com/huge.zip
```

**⑤ 需要鉴权的资源**

```bash
veloxdl -H "Authorization: Bearer YOUR_TOKEN" https://api.example.com/export.zip
```

**⑥ 在脚本中使用**

```bash
OUT=$(VELOXDL_QUIET=1 veloxdl https://example.com/a.bin)
echo "下载完成: $OUT"
```

### 环境变量

所有命令行参数都有对应的环境变量（参数优先级更高），见 [`.env.example`](.env.example)：

| 环境变量 | 对应参数 | 默认值 |
|----------|----------|--------|
| `VELOXDL_MAX_CONNS` | `-n` | `16` |
| `VELOXDL_CHUNK_SIZE` | `--chunk` | `1048576` |
| `VELOXDL_RATE_LIMIT` | `--rate-limit` | 空（不限速） |
| `VELOXDL_TIMEOUT` | `--timeout` | `60s` |
| `VELOXDL_RETRIES` | `--retries` | `5` |
| `VELOXDL_USER_AGENT` | `-A` | `VeloxDL/1.0` |
| `VELOXDL_QUIET` | `-q` | `0` |

> 标准代理变量 `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` 由 Go 标准库直接支持，无需额外配置。

### 作为 Go 库使用

```bash
go get github.com/gitstq/VeloxDL
```

```go
package main

import (
	"context"
	"fmt"

	"github.com/gitstq/VeloxDL/pkg/veloxdl"
)

func main() {
	snap, err := veloxdl.Download(context.Background(), veloxdl.Options{
		URLs: []string{
			"https://mirror-a.example.com/big.iso",
			"https://mirror-b.example.com/big.iso", // 多镜像自动聚合
		},
		Output:     "big.iso",
		MaxConns:   32,
		RateLimit:  8 << 20, // 限速 8 MiB/s
		Resume:     true,    // 支持断点续传
		Checksum:   "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
	}, func(s veloxdl.Snapshot) {
		fmt.Printf("\r%.1f%%  %d/%d bytes", s.Percent(), s.Saved, s.Total)
	})

	if err != nil {
		panic(err)
	}
	fmt.Printf("\n完成，平均 %.2f MiB/s\n", snap.Overall/(1<<20))
}
```

### 运行效果

```
$ veloxdl -n 32 https://mirror.example.com/ubuntu-24.04.iso

⬇️  VeloxDL 1.0.0  https://mirror.example.com/ubuntu-24.04.iso（共 3 个源）  ->  ubuntu-24.04.iso
[████████░░░░░░░░░░░░░░░░░░░]  28.4%  573.2 MiB / 2.0 GiB   31.5 MiB/s  eta 46s  conns 12/32
[██████████████████░░░░░░░░░░]  61.9%  1.2 GiB / 2.0 GiB   36.2 MiB/s  eta 22s  conns 24/32
[████████████████████████████] 100.0%  2.0 GiB / 2.0 GiB   38.9 MiB/s  eta 0s  conns 32/32
✅ 完成 ubuntu-24.04.iso  2.0 GiB  100.0%  平均 33.4 MiB/s  用时 61s  并发 32  模式 分片并行
```

> 可以看到并发数从 12 逐步爬升到 32 —— 这就是自适应调参在工作。

---

## 💡 设计思路与迭代规划

### 为什么重新造一个下载器？

现成工具要么**并发写死**（aria2 的 `-x` 是固定值），要么**只做网卡聚合**（Plexo 需要同时连 Wi-Fi 和有线网，普通用户用不上），要么**依赖庞大**（动辄引入几十个模块）。

VeloxDL 想验证一件事：**用纯标准库、零依赖，能不能做出一个真正"会自己找最优并发"的下载器？** 答案是能。

### 关键技术选型

| 决策 | 选择 | 原因 |
|------|------|------|
| 语言 | **Go** | goroutine 天然适配高并发 IO；交叉编译开箱即用；产物无运行时依赖 |
| 依赖策略 | **零第三方依赖** | 只用标准库 → 可复现构建、无供应链风险、`go install` 秒装 |
| 落盘方式 | **预分配 + `WriteAt`** | 分片可乱序到达，各自写自己的偏移，互不干扰 |
| 并发控制 | **动态闸门** | 容量可运行时增减，不用杀 worker 重建 |
| 限速算法 | **全局时间线** | 解决多分片并行绕过令牌桶的经典问题 |

### 架构总览

```
cmd/veloxdl/          CLI 入口：参数解析、进度渲染、信号处理
  ├── pkg/veloxdl/    公共 Go API（薄封装，保证 CLI 与库行为一致）
  └── internal/
      ├── engine/     下载引擎
      │   ├── probe.go      资源探测（HEAD → Range GET 降级）
      │   ├── chunk.go      分片规划
      │   ├── queue.go      阻塞任务队列
      │   ├── gate.go       动态并发闸门
      │   ├── ratelimit.go  限速 + 吞吐采样
      │   ├── state.go      断点续传状态（原子写入）
      │   └── engine.go     调度主流程
      ├── progress/    进度条渲染
      └── verify/      校验和
```

### 已知的边界

- 断点续传要求服务端**支持 Range 且资源未变更**，否则会自动回退为全量重下（这是刻意的安全取舍）
- 自适应调参的阈值（8% / 12%）是针对常见家庭宽带调的，未来计划做成可配置
- 目前不支持 BitTorrent / 磁力链接

### 后续迭代计划

- [ ] 支持从文本文件批量导入 URL 队列
- [ ] 自适应调参阈值可配置（`--tune-aggressive` / `--tune-conservative`）
- [ ] 失败镜像自动降权，而不是简单轮转
- [ ] 下载历史记录与统计（按域名聚合速度）
- [ ] 支持 HTTP/3 (QUIC)

---

## 📦 打包与部署指南

VeloxDL 属于 **CLI 工具 / 库类项目**，因此：

1. **主推安装方式是 `go install`**，一条命令即可，无需任何 Release 附件
2. 同时提供**预编译二进制**（见 [Releases](https://github.com/gitstq/VeloxDL/releases)），给不想装 Go 环境的用户

### 交叉编译全部平台

```bash
./scripts/build.sh
# 或
make dist
```

产物矩阵：

| 操作系统 | 架构 | 产物名 |
|----------|------|--------|
| Linux | amd64 | `veloxdl_linux_amd64` |
| Linux | arm64 | `veloxdl_linux_arm64` |
| macOS | amd64 | `veloxdl_darwin_amd64` |
| macOS | arm64 | `veloxdl_darwin_arm64` |
| Windows | amd64 | `veloxdl_windows_amd64.exe` |
| Windows | arm64 | `veloxdl_windows_arm64.exe` |

构建脚本会自动生成 `dist/SHA256SUMS` 校验和清单。

### 构建参数说明

- `CGO_ENABLED=0`：纯静态链接，产物可直接在 Alpine 等精简镜像中运行
- `-trimpath`：移除本地路径信息，保证构建可复现
- `-ldflags "-s -w"`：剥离符号表与调试信息，显著减小体积

### 验证产物

```bash
cd dist && sha256sum -c SHA256SUMS
```

### 容器中使用

```dockerfile
FROM alpine:3.19
COPY veloxdl_linux_amd64 /usr/local/bin/veloxdl
RUN chmod +x /usr/local/bin/veloxdl
ENTRYPOINT ["veloxdl"]
```

---

## 🤝 贡献指南

我们欢迎任何形式的贡献！详见 [CONTRIBUTING.md](CONTRIBUTING.md)。

简而言之：

1. **Fork** 本仓库并创建特性分支（`git checkout -b feat/amazing-feature`）
2. 提交前运行 `make check`（格式化 + 静态检查 + 竞态测试）
3. 按 [Angular 提交规范](CONTRIBUTING.md#提交信息规范) 写提交信息
4. 发起 **Pull Request**

### 提交信息规范

```
<type>(<scope>): <subject>

feat(engine): 新增镜像自动降权策略
fix(state): 修复分片区间校验的边界问题
docs(readme): 补充容器部署示例
```

常用 type：`feat` · `fix` · `docs` · `refactor` · `test` · `chore` · `perf`

---

## 📄 开源协议

本项目基于 [MIT License](LICENSE) 开源。

```
Copyright (c) 2026 gitstq
```

你可以自由地使用、修改、分发本软件，包括商业用途，只需保留版权声明。详见 [LICENSE](LICENSE)。

---

<div align="center">

**⚡ VeloxDL** · 让每一兆带宽都跑满

Made with ❤️ and pure Go stdlib

</div>
