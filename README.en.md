<div align="center">

# ⚡ VeloxDL

**Adaptive Parallel Download Accelerator**

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green?style=flat-square)](LICENSE)
[![Zero Dependencies](https://img.shields.io/badge/dependencies-0-brightgreen?style=flat-square)](go.mod)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux-black?style=flat-square)](https://github.com/gitstq/VeloxDL/releases)

<table align="center">
  <tr>
    <td><a href="README.md"><b>简体中文</b></a></td>
    <td><a href="README.zh-TW.md"><b>繁體中文</b></a></td>
    <td><b>English</b></td>
  </tr>
</table>

</div>

---

## 🎯 In One Sentence

> A single TCP connection rarely saturates your bandwidth. VeloxDL splits a file into fragments, spreads them across multiple connections — even multiple mirrors — and **continuously senses throughput to auto-tune concurrency**, so your download actually uses the pipe you pay for.

```bash
$ veloxdl -n 32 https://mirror.example.com/ubuntu-24.04.iso

⬇️  VeloxDL 1.0.0  https://mirror.example.com/ubuntu-24.04.iso  ->  ubuntu-24.04.iso
[████████████████████░░░░░░░░]  71.3%  1.4 GiB / 2.0 GiB   38.7 MiB/s  eta 17s  conns 32/32
```

---

## ✨ Features

### 🚀 Adaptive Concurrency (the real differentiator)

Most downloaders take a **hard-coded** connection count: you pass 16, you get 16, forever. But the optimal concurrency for a real link changes over time — you need slow start at the beginning, you should back off when congestion appears, and you should probe upward when the link is idle.

VeloxDL ships a controller modeled on TCP congestion control: **it samples throughput every 500ms, adds a connection when throughput improves by more than 8%, and drops one when it degrades by more than 12%.** Your `-n` is the ceiling, not the value.

```
throughput ↑ 8%  →  conns + 1   (probe: is there headroom?)
throughput ↓ 12% →  conns - 1   (back off: saturated, stop crowding)
```

### 🧩 Fine-Grained Chunking Kills the "Tail Effect"

If chunk count equals connection count, the slowest connection strands the whole download on the final 1/N. VeloxDL defaults to **connections × 4** chunks and **enqueues them largest-remaining-first** — fast connections naturally claim more work, so only small fragments remain at the finish and the tail window shrinks dramatically.

### 🌐 Multi-Mirror Aggregation

Have the same file on several hosts? Hand them all to VeloxDL and different chunks **round-robin across mirrors**. If a mirror dies, failed chunks silently retry on another source.

```bash
veloxdl https://a.example/x.iso --mirror https://b.example/x.iso --mirror https://c.example/x.iso
```

> Safety net: mirrors are **strictly validated** for matching Content-Length and ETag. Mismatched mirrors are dropped outright — otherwise cross-source stitching would produce a byte-misaligned file.

### 💾 Interruptible Resume

Hit Ctrl+C halfway? VeloxDL flushes progress to a state file before exiting. Next time, `-c` picks up **without wasting a single byte**.

- State files use **temp-file + atomic rename**, so a killed process never leaves a half-written file
- Before resuming, a **four-way check** runs (URL / size / chunk plan / ETag); any mismatch means a clean full re-download
- The state file is deleted **only after checksum verification passes**, guaranteeing resume always remains possible

### 🎛️ Rate Limiting That Actually Works

A naive token bucket is trivially bypassed under parallel chunks: eight fragments each find a token available and all proceed at once, yielding 8× the intended rate.

VeloxDL uses a **global quota timeline**: before sending, each fragment reserves a slot of `n/rate` and queues at the tail. No matter how many fragments run in parallel, total throughput stays pinned below the limit.

```bash
veloxdl --rate-limit 2M https://example.com/big.zip   # cap at 2 MiB/s, leave room for your video call
```

### 🔐 Integrity Verification

A bug in parallel chunking with out-of-order writes almost always manifests as **silent file corruption, not a program error**. So VeloxDL verifies after download:

```bash
veloxdl --checksum sha256:9f86d081884c7d659a2feaa0c55ad015... https://example.com/f.bin
```

A mismatch fails hard (exit code 1) — a corrupt file never slips through quietly.

---

## 🚀 Quick Start

### Requirements

| Item | Requirement |
|------|-------------|
| **Go** | 1.21+ (only needed when building from source) |
| **OS** | macOS / Windows / Linux |
| **Arch** | amd64 / arm64 |
| **Dependencies** | **None** (pure Go standard library) |

### Option 1: go install (recommended)

```bash
go install github.com/gitstq/VeloxDL/cmd/veloxdl@latest
```

Verify:

```bash
veloxdl --version
```

### Option 2: Prebuilt binaries

Grab the archive for your platform from the [Releases page](https://github.com/gitstq/VeloxDL/releases). No runtime dependencies.

```bash
# Linux / macOS example
tar -xzf veloxdl_linux_amd64.tar.gz
chmod +x veloxdl_linux_amd64
sudo mv veloxdl_linux_amd64 /usr/local/bin/veloxdl
```

### Option 3: Build from source

```bash
git clone https://github.com/gitstq/VeloxDL.git
cd VeloxDL

# current platform only
make build

# or cross-compile every platform (output in dist/)
make dist
```

### Your first download

```bash
veloxdl https://speed.hetzner.de/1GB.bin
```

---

## 📖 Usage

### Flags

```
Usage:
  veloxdl [flags] <URL> [URL...]

Flags:
  -o <path>        output file path (inferred from URL by default)
  -n <num>         max concurrent connections (default 16)
  -c               resume from the previous interruption
  --checksum <s>   expected checksum, e.g. sha256:<hex>; fail on mismatch
  --rate-limit <s> rate limit, e.g. 500K / 2M / 10M, or raw bytes
  --retries <num>  max retries per chunk (default 5)
  --timeout <dur>  per-chunk request timeout (default 60s)
  -H <header>      custom request header, repeatable
  -A <ua>          custom User-Agent
  -q               quiet mode, print only the final file path
  --mirror <url>   additional mirror URL, repeatable
  --version        print version and exit
```

### Common Scenarios

**① Everyday acceleration**

```bash
veloxdl -n 16 -o dataset.tar.gz https://example.com/dataset.tar.gz
```

**② Multi-mirror aggregation**

```bash
veloxdl -n 32 https://a.cdn/x.iso \
  --mirror https://b.cdn/x.iso \
  --mirror https://c.cdn/x.iso
```

**③ Resume after interruption**

```bash
# first attempt (Ctrl+C to interrupt)
veloxdl -o big.iso https://example.com/big.iso
# progress is saved — continue
veloxdl -c -o big.iso https://example.com/big.iso
```

**④ Throttled download**

```bash
veloxdl --rate-limit 1M https://example.com/huge.zip
```

**⑤ Authenticated resources**

```bash
veloxdl -H "Authorization: Bearer YOUR_TOKEN" https://api.example.com/export.zip
```

**⑥ Scripting**

```bash
OUT=$(VELOXDL_QUIET=1 veloxdl https://example.com/a.bin)
echo "done: $OUT"
```

### Environment Variables

Every flag has a matching environment variable (flags take precedence), see [`.env.example`](.env.example):

| Variable | Flag | Default |
|----------|------|---------|
| `VELOXDL_MAX_CONNS` | `-n` | `16` |
| `VELOXDL_CHUNK_SIZE` | `--chunk` | `1048576` |
| `VELOXDL_RATE_LIMIT` | `--rate-limit` | empty (unlimited) |
| `VELOXDL_TIMEOUT` | `--timeout` | `60s` |
| `VELOXDL_RETRIES` | `--retries` | `5` |
| `VELOXDL_USER_AGENT` | `-A` | `VeloxDL/1.0` |
| `VELOXDL_QUIET` | `-q` | `0` |

> Standard proxy variables `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` are honored by the Go standard library out of the box.

### As a Go library

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
			"https://mirror-b.example.com/big.iso", // mirrors are aggregated automatically
		},
		Output:    "big.iso",
		MaxConns:  32,
		RateLimit: 8 << 20, // cap at 8 MiB/s
		Resume:    true,
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

## 💡 Design & Roadmap

### Why build another downloader?

Existing tools either **hard-code concurrency** (aria2's `-x` is a fixed value), **only do NIC bonding** (Plexo needs Wi-Fi *and* Ethernet simultaneously — impractical for most people), or **drag in huge dependency trees**.

VeloxDL set out to answer one question: **with zero third-party dependencies, can you build a downloader that actually finds the optimal concurrency on its own?** It can.

### Key Technical Decisions

| Decision | Choice | Why |
|----------|--------|-----|
| Language | **Go** | goroutines fit high-concurrency IO natively; effortless cross-compilation; dependency-free artifacts |
| Dependency policy | **Zero third-party** | stdlib only → reproducible builds, no supply-chain risk, instant `go install` |
| Disk writes | **Preallocate + `WriteAt`** | chunks arrive out of order and each writes its own offset without interference |
| Concurrency control | **Dynamic gate** | capacity changes at runtime, no need to kill and respawn workers |
| Rate limiting | **Global timeline** | solves the classic token-bucket bypass under parallel chunks |

### Architecture

```
cmd/veloxdl/          CLI entry: flag parsing, progress rendering, signal handling
  ├── pkg/veloxdl/    public Go API (thin wrapper keeps CLI and library behavior identical)
  └── internal/
      ├── engine/     download engine
      │   ├── probe.go      resource probing (HEAD → Range GET fallback)
      │   ├── chunk.go      chunk planning
      │   ├── queue.go      blocking task queue
      │   ├── gate.go       dynamic concurrency gate
      │   ├── ratelimit.go  rate limiting + throughput sampling
      │   ├── state.go      resume state (atomic writes)
      │   └── engine.go     scheduling pipeline
      ├── progress/    progress bar rendering
      └── verify/      checksums
```

### Known Boundaries

- Resume requires that the server **supports Range and the resource is unchanged**; otherwise it falls back to a clean full re-download (a deliberate safety trade-off)
- The auto-tuning thresholds (8% / 12%) are tuned for typical home broadband; making them configurable is planned
- BitTorrent / magnet links are not supported

### Roadmap

- [ ] Batch URL queue import from a text file
- [ ] Configurable auto-tuning thresholds (`--tune-aggressive` / `--tune-conservative`)
- [ ] Automatic downgrading of failing mirrors instead of simple round-robin
- [ ] Download history and per-domain speed statistics
- [ ] HTTP/3 (QUIC) support

---

## 📦 Packaging & Deployment

VeloxDL is a **CLI tool / library**, therefore:

1. **`go install` is the primary installation path** — one command, no release assets required
2. **Prebuilt binaries** are also published (see [Releases](https://github.com/gitstq/VeloxDL/releases)) for users without a Go toolchain

### Cross-compiling every platform

```bash
./scripts/build.sh
# or
make dist
```

Artifact matrix:

| OS | Arch | Artifact |
|----|------|----------|
| Linux | amd64 | `veloxdl_linux_amd64` |
| Linux | arm64 | `veloxdl_linux_arm64` |
| macOS | amd64 | `veloxdl_darwin_amd64` |
| macOS | arm64 | `veloxdl_darwin_arm64` |
| Windows | amd64 | `veloxdl_windows_amd64.exe` |
| Windows | arm64 | `veloxdl_windows_arm64.exe` |

The build script also emits `dist/SHA256SUMS`.

### Build Flags Explained

- `CGO_ENABLED=0`: fully static linking — the binary runs on slim images like Alpine
- `-trimpath`: strips local paths for reproducible builds
- `-ldflags "-s -w"`: removes symbol table and debug info, significantly shrinking size

### Verifying artifacts

```bash
cd dist && sha256sum -c SHA256SUMS
```

### Using in a container

```dockerfile
FROM alpine:3.19
COPY veloxdl_linux_amd64 /usr/local/bin/veloxdl
RUN chmod +x /usr/local/bin/veloxdl
ENTRYPOINT ["veloxdl"]
```

---

## 🤝 Contributing

Contributions of all kinds are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md).

In short:

1. **Fork** the repo and create a feature branch (`git checkout -b feat/amazing-feature`)
2. Run `make check` before committing (fmt + vet + race tests)
3. Follow the [Angular commit convention](CONTRIBUTING.md)
4. Open a **Pull Request**

### Commit Message Format

```
<type>(<scope>): <subject>

feat(engine): add automatic mirror downgrading
fix(state): fix boundary check in chunk validation
docs(readme): add container deployment example
```

Common types: `feat` · `fix` · `docs` · `refactor` · `test` · `chore` · `perf`

---

## 📄 License

Released under the [MIT License](LICENSE).

```
Copyright (c) 2026 gitstq
```

Use, modify, and distribute freely — including commercially — as long as the copyright notice is retained. See [LICENSE](LICENSE) for details.

---

<div align="center">

**⚡ VeloxDL** · Saturate every megabit you pay for

Made with ❤️ and pure Go stdlib

</div>
