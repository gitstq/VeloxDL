<div align="center">

# ⚡ VeloxDL

**自適應並行下載加速器 · Adaptive Parallel Download Accelerator**

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green?style=flat-square)](LICENSE)
[![Zero Dependencies](https://img.shields.io/badge/dependencies-0-brightgreen?style=flat-square)](go.mod)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux-black?style=flat-square)](https://github.com/gitstq/VeloxDL/releases)

<table align="center">
  <tr>
    <td><a href="README.md"><b>简体中文</b></a></td>
  <td><b>繁體中文</b></td>
    <td><a href="README.en.md"><b>English</b></a></td>
  </tr>
</table>

</div>

---

## 🎯 一句話介紹

> 單一 TCP 連線跑不滿你的頻寬 —— VeloxDL 把檔案切碎、散到多條連線（甚至多個鏡像）上並行拉取，並**即時感知吞吐變化、自動增減併發**，讓你的下載真正吃滿頻寬。

```bash
$ veloxdl -n 32 https://mirror.example.com/ubuntu-24.04.iso

⬇️  VeloxDL 1.0.0  https://mirror.example.com/ubuntu-24.04.iso  ->  ubuntu-24.04.iso
[████████████████████░░░░░░░░]  71.3%  1.4 GiB / 2.0 GiB   38.7 MiB/s  eta 17s  conns 32/32
```

---

## ✨ 核心特色

### 🚀 自適應併發（真正的差異化亮點）

大多數下載器的併發數是**寫死的**——你填 16 它就永遠 16。但真實鏈路的最佳併發是隨時間變化的：開局要慢啟動，中途可能因擁塞該降速，空閒時又該往上探。

VeloxDL 內建了一個類似 TCP 壅塞控制的調參器：**每 500ms 採樣一次吞吐，提速超過 8% 就試探性加一條連線，掉速超過 12% 就退一條。** 你給的 `-n` 是天花板，不是固定值。

```
吞吐 ↑ 8%  →  conns + 1   （試探：鏈路還有餘量嗎？）
吞吐 ↓ 12% →  conns - 1   （回退：已經飽和，別再擠了）
```

### 🧩 碎片化分片，消滅「拖尾效應」

如果分片數恰好等於連線數，最慢的那條連線會讓整個下載卡在最後的 1/N 上。VeloxDL 預設切出 **連線數 × 4** 個分片，而且**按剩餘大小降序入列**——快的連線自動多領任務，收尾時只剩一堆小塊，尾部空窗期被大幅壓縮。

### 🌐 多鏡像聚合

同一個檔案有多個來源？把它們都交給 VeloxDL，不同分片會**輪流打到不同鏡像**上。某個鏡像掛了，失敗分片會自動換源重試，全程無感。

```bash
veloxdl https://a.example/x.iso --mirror https://b.example/x.iso --mirror https://c.example/x.iso
```

> 安全兜底：鏡像之間會**強制校驗 Content-Length 與 ETag 是否一致**，不一致的鏡像直接剔除——否則跨源拼接會產出位元組錯位的檔案。

### 💾 可中斷的斷點續傳

下載到一半按下 Ctrl+C？VeloxDL 會先把進度寫進狀態檔再退出。下次 `-c` 接著下，**不會浪費任何一個位元組**。

- 狀態檔採用 **暫存檔 + rename 原子寫入**，行程被 kill 也不會留下半截檔案
- 續傳前會做**四重校驗**（URL / 檔案大小 / 分片規劃 / ETag），任一不符就老實重下
- **只有校驗通過後才刪除狀態檔**，保證中斷後一定能續上

### 🎛 真正生效的限速

常見的令牌桶實作在多分片並行時會被繞過：八個分片同時發現自己有令牌，就同時放行，實際速率變成八倍。

VeloxDL 用**全域配額時間軸**：每個分片要送資料前，先在時間軸上預約一段 `n/rate` 的時長，排到隊尾。無論多少分片並行，總吞吐都被嚴格壓在設定值之下。

```bash
veloxdl --rate-limit 2M https://example.com/big.zip   # 只佔 2 MiB/s，不打擾視訊會議
```

### 🔐 完整性校驗

並行分片 + 亂序落盤一旦出 bug，幾乎必然表現為**檔案內容損壞而不是程式報錯**。因此 VeloxDL 支援下載後自動校驗：

```bash
veloxdl --checksum sha256:9f86d081884c7d659a2feaa0c55ad015... https://example.com/f.bin
```

不匹配直接報錯退出（退出碼 1），絕不讓損壞檔案悄悄通過。

---

## 🚀 快速開始

### 環境需求

| 項目 | 需求 |
|------|------|
| **Go** | 1.21 或更高（僅從原始碼建置時需要） |
| **作業系統** | macOS / Windows / Linux |
| **架構** | amd64 / arm64 |
| **第三方依賴** | **無**（純 Go 標準函式庫實作） |

### 方式一：go install（推薦）

```bash
go install github.com/gitstq/VeloxDL/cmd/veloxdl@latest
```

安裝後確認：

```bash
veloxdl --version
```

### 方式二：下載預編譯執行檔

前往 [Releases 頁面](https://github.com/gitstq/VeloxDL/releases) 下載對應平台的執行檔，解壓後即可執行，無需任何執行環境依賴。

```bash
# Linux / macOS 範例
tar -xzf veloxdl_linux_amd64.tar.gz
chmod +x veloxdl_linux_amd64
sudo mv veloxdl_linux_amd64 /usr/local/bin/veloxdl
```

### 方式三：從原始碼建置

```bash
git clone https://github.com/gitstq/VeloxDL.git
cd VeloxDL

# 只建置目前平台
make build

# 或：交叉編譯全部平台（產品在 dist/）
make dist
```

### 第一次下載

```bash
veloxdl https://speed.hetzner.de/1GB.bin
```

---

## 📖 詳細使用指南

### 命令參數

```
用法:
  veloxdl [選項] <URL> [URL...]

常用選項:
  -o <path>        輸出檔案路徑（預設從 URL 推斷）
  -n <num>         最大併發連線數（預設 16）
  -c               斷點續傳，從上次中斷處繼續
  --checksum <s>   期望校驗和，如 sha256:<hex>，不匹配則報錯退出
  --rate-limit <s> 限速，支援 500K / 2M / 10M 或純位元組數
  --retries <num>  單一分片的最大重試次數（預設 5）
  --timeout <dur>  單一分片請求逾時（預設 60s）
  -H <header>      自訂請求標頭，可重複傳入
  -A <ua>          自訂 User-Agent
  -q               靜默模式，只輸出最終檔案路徑
  --mirror <url>   附加鏡像位址，可重複傳入
  --version        列印版本號
```

### 常見情境

**① 日常加速下載**

```bash
veloxdl -n 16 -o dataset.tar.gz https://example.com/dataset.tar.gz
```

**② 多鏡像聚合（頻寬疊加）**

```bash
veloxdl -n 32 https://a.cdn/x.iso \
  --mirror https://b.cdn/x.iso \
  --mirror https://c.cdn/x.iso
```

**③ 中斷後續傳**

```bash
# 第一次（Ctrl+C 中斷）
veloxdl -o big.iso https://example.com/big.iso
# 進度已存檔，接著下
veloxdl -c -o big.iso https://example.com/big.iso
```

**④ 限速下載（不干擾其他應用）**

```bash
veloxdl --rate-limit 1M https://example.com/huge.zip
```

**⑤ 需要驗證的資源**

```bash
veloxdl -H "Authorization: Bearer YOUR_TOKEN" https://api.example.com/export.zip
```

**⑥ 在指令碼中使用**

```bash
OUT=$(VELOXDL_QUIET=1 veloxdl https://example.com/a.bin)
echo "下載完成: $OUT"
```

### 環境變數

所有命令列參數都有對應的環境變數（參數優先級較高），詳見 [`.env.example`](.env.example)：

| 環境變數 | 對應參數 | 預設值 |
|----------|----------|--------|
| `VELOXDL_MAX_CONNS` | `-n` | `16` |
| `VELOXDL_CHUNK_SIZE` | `--chunk` | `1048576` |
| `VELOXDL_RATE_LIMIT` | `--rate-limit` | 空（不限速） |
| `VELOXDL_TIMEOUT` | `--timeout` | `60s` |
| `VELOXDL_RETRIES` | `--retries` | `5` |
| `VELOXDL_USER_AGENT` | `-A` | `VeloxDL/1.0` |
| `VELOXDL_QUIET` | `-q` | `0` |

> 標準代理變數 `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` 由 Go 標準函式庫直接支援，無需額外設定。

### 作為 Go 函式庫使用

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
			"https://mirror-b.example.com/big.iso", // 多鏡像自動聚合
		},
		Output:    "big.iso",
		MaxConns:  32,
		RateLimit: 8 << 20, // 限速 8 MiB/s
		Resume:    true,    // 支援斷點續傳
		Checksum:  "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
	}, func(s veloxdl.Snapshot) {
		fmt.Printf("\r%.1f%%  %d/%d bytes", s.Percent(), s.Saved, s.Total)
	})

	if err != nil {
		panic(err)
	}
	fmt.Printf("\ndone, avg %.2f MiB/s\n", snap.Overall/(1<<20))
}
```

---

## 💡 設計思路與迭代規劃

### 為什麼要再造一個下載器？

現成工具要嘛**併發寫死**（aria2 的 `-x` 是固定值），要嘛**只做網卡聚合**（Plexo 要求同時連 Wi-Fi 與有線網路，一般使用者用不上），要嘛**相依套件龐大**（動輒引入數十個模組）。

VeloxDL 想驗證一件事：**只用標準函式庫、零相依，能不能做出一個真正「會自己找最佳併發」的下載器？** 答案是能。

### 關鍵技術選型

| 決策 | 選擇 | 原因 |
|------|------|------|
| 語言 | **Go** | goroutine 天生適配高併發 IO；交叉編譯開箱即用；產物無執行環境依賴 |
| 相依策略 | **零第三方相依** | 只用標準函式庫 → 可重現建置、無供應鏈風險、`go install` 秒裝 |
| 落磁碟方式 | **預分配 + `WriteAt`** | 分片可亂序抵達，各自寫自己的偏移，互不干擾 |
| 併發控制 | **動態閘門** | 容量可在執行期增減，不必砍掉 worker 重建 |
| 限速演算法 | **全域時間軸** | 解決多分片並行繞過令牌桶的經典問題 |

### 架構總覽

```
cmd/veloxdl/          CLI 入口：參數解析、進度渲染、訊號處理
  ├── pkg/veloxdl/    公開 Go API（薄封裝，保證 CLI 與函式庫行為一致）
  └── internal/
      ├── engine/     下載引擎
      │   ├── probe.go      資源探測（HEAD → Range GET 降級）
      │   ├── chunk.go      分片規劃
      │   ├── queue.go      阻塞任務佇列
      │   ├── gate.go       動態併發閘門
      │   ├── ratelimit.go  限速 + 吞吐採樣
      │   ├── state.go      斷點續傳狀態（原子寫入）
      │   └── engine.go     排程主流程
      ├── progress/    進度條渲染
      └── verify/      校驗和
```

### 已知邊界

- 斷點續傳要求伺服端**支援 Range 且資源未變更**，否則會自動退回完整重下（這是刻意的安全取捨）
- 自適應調參的閾值（8% / 12%）是針對常見家用寬頻調整的，未來計畫做成可設定
- 目前不支援 BitTorrent / 磁力連結

### 後續迭代計畫

- [ ] 支援從文字檔批次匯入 URL 佇列
- [ ] 自適應調參閾值可設定（`--tune-aggressive` / `--tune-conservative`）
- [ ] 失敗鏡像自動降權，而非單純輪替
- [ ] 下載歷史紀錄與統計（依網域聚合速度）
- [ ] 支援 HTTP/3 (QUIC)

---

## 📦 打包與部署指南

VeloxDL 屬於 **CLI 工具 / 函式庫類專案**，因此：

1. **主要安裝方式是 `go install`**，一條指令即可，無需任何 Release 附件
2. 同時提供**預編譯執行檔**（見 [Releases](https://github.com/gitstq/VeloxDL/releases)），給不想裝 Go 環境的使用者

### 交叉編譯全部平台

```bash
./scripts/build.sh
# 或
make dist
```

產物矩陣：

| 作業系統 | 架構 | 產物名稱 |
|----------|------|----------|
| Linux | amd64 | `veloxdl_linux_amd64` |
| Linux | arm64 | `veloxdl_linux_arm64` |
| macOS | amd64 | `veloxdl_darwin_amd64` |
| macOS | arm64 | `veloxdl_darwin_arm64` |
| Windows | amd64 | `veloxdl_windows_amd64.exe` |
| Windows | arm64 | `veloxdl_windows_arm64.exe` |

建置腳本會自動產生 `dist/SHA256SUMS` 校驗和清單。

### 建置參數說明

- `CGO_ENABLED=0`：純靜態連結，產物可直接在 Alpine 等精簡映像中執行
- `-trimpath`：移除本機路徑資訊，保證建置可重現
- `-ldflags "-s -w"`：剝離符號表與除錯資訊，顯著縮小體積

### 驗證產物

```bash
cd dist && sha256sum -c SHA256SUMS
```

### 在容器中使用

```dockerfile
FROM alpine:3.19
COPY veloxdl_linux_amd64 /usr/local/bin/veloxdl
RUN chmod +x /usr/local/bin/veloxdl
ENTRYPOINT ["veloxdl"]
```

---

## 🤝 貢獻指南

歡迎任何形式的貢獻！詳見 [CONTRIBUTING.md](CONTRIBUTING.md)。

簡而言之：

1. **Fork** 本專案並建立特性分支（`git checkout -b feat/amazing-feature`）
2. 提交前執行 `make check`（格式化 + 靜態檢查 + 競態測試）
3. 依照 [Angular 提交規範](CONTRIBUTING.md#提交資訊規範) 撰寫提交資訊
4. 發起 **Pull Request**

### 提交資訊規範

```
<type>(<scope>): <subject>

feat(engine): 新增鏡像自動降權策略
fix(state): 修正分片區間校驗的邊界問題
docs(readme): 補充容器部署範例
```

常用 type：`feat` · `fix` · `docs` · `refactor` · `test` · `chore` · `perf`

---

## 📄 開源授權

本專案基於 [MIT License](LICENSE) 開源。

```
Copyright (c) 2026 gitstq
```

你可以自由地使用、修改、散佈本軟體，包含商業用途，只需保留版權聲明。詳見 [LICENSE](LICENSE)。

---

<div align="center">

**⚡ VeloxDL** · 讓每一兆頻寬都跑滿

Made with ❤️ and pure Go stdlib

</div>
