# 🤝 贡献指南

感谢你愿意为 VeloxDL 贡献代码！这份文档会帮你在几分钟内跑通本地开发环境。

---

## 🛠 本地开发

### 前置条件

- **Go 1.21 或更高版本**（[下载地址](https://go.dev/dl/)）
- Git

### 起步

```bash
git clone https://github.com/gitstq/VeloxDL.git
cd VeloxDL

# 拉取依赖（本项目零第三方依赖，这一步会很快）
go mod download

# 构建当前平台二进制
make build

# 跑一遍测试
make test
```

### 常用命令

| 命令 | 说明 |
|------|------|
| `make build` | 构建当前平台二进制到 `bin/` |
| `make test` | 运行单元测试 |
| `make race` | 运行带竞态检测的测试 |
| `make coverage` | 生成覆盖率报告（`coverage.html`） |
| `make dist` | 交叉编译全部平台到 `dist/` |
| `make fmt` | 格式化代码 |
| `make vet` | 静态检查 |
| `make check` | `fmt` + `vet` + `race`，提交前请跑一遍 |
| `make install` | 安装到 `GOBIN` |
| `make clean` | 清理构建产物 |

---

## 🧪 测试要求

**提交前必须保证 `make check` 全绿。** CI 会跑同样的检查，红了的 PR 无法合并。

新增功能时请补充对应测试：

| 场景 | 建议做法 |
|------|----------|
| 分片 / 区间计算 | 表驱动测试，覆盖边界值（0、1、非整倍数） |
| 网络相关逻辑 | 用 `net/http/httptest` 起本地服务器，不要依赖外网 |
| 并发逻辑 | 必须能通过 `-race`；可参考 `TestDownloadConcurrentSafe` |
| 文件写入 | 用 `t.TempDir()`，不要用固定路径 |

### 本地端到端自测

项目自带一个测试服务器，可以模拟不同的服务端行为：

```bash
# 准备测试文件
mkdir -p /tmp/vx && head -c 50M /dev/urandom > /tmp/vx/sample.bin

# 启动（支持 Range 的正常服务器）
go run ./test/server -dir /tmp/vx -port 8099

# 另开一个终端，下载验证
./bin/veloxdl -n 16 -o /tmp/out.bin http://127.0.0.1:8099/sample.bin
cmp /tmp/vx/sample.bin /tmp/out.bin && echo "✅ 内容一致"
```

模拟特殊场景：

```bash
# 不支持 Range 的老旧服务器（应自动降级为单流）
go run ./test/server -dir /tmp/vx -port 8100 -no-range

# 高延迟链路（验证自适应并发的爬升过程）
go run ./test/server -dir /tmp/vx -port 8101 -delay 20ms
```

---

## 📝 提交信息规范

采用 **Angular 提交规范**：

```
<type>(<scope>): <subject>

<body>

<footer>
```

### type（必填）

| type | 说明 |
|------|------|
| `feat` | 新增功能 |
| `fix` | 修复缺陷 |
| `docs` | 文档变更 |
| `refactor` | 重构（既不新增功能也不修复缺陷） |
| `perf` | 性能优化 |
| `test` | 补充或修改测试 |
| `chore` | 构建流程 / 工具链变更 |

### scope（可选）

`engine` · `cli` · `progress` · `verify` · `docs` · `deps`

### 示例

```
feat(engine): 新增镜像自动降权策略

失败次数超过阈值的镜像会被降权，避免持续把分片调度到故障源上。

Closes #42
```

```
fix(state): 修复分片区间校验的边界问题

当分片 End 恰好等于 size-1 时会被误判为越界，导致续传失败。
```

---

## 🔀 Pull Request 流程

1. **Fork** 本仓库并基于 `main` 创建分支：

   ```bash
   git checkout -b feat/your-feature
   ```

2. 完成开发后自检：

   ```bash
   make check
   ```

3. 提交（遵循上面的规范）：

   ```bash
   git commit -m "feat(engine): 新增镜像自动降权策略"
   ```

4. 推送并发起 PR：

   ```bash
   git push origin feat/your-feature
   ```

5. 在 PR 描述中说明：**改了什么 / 为什么改 / 如何验证**

### PR 会被检查什么

- ✅ `gofmt` 无差异
- ✅ `go vet` 无告警
- ✅ `go test -race` 全绿
- ✅ 提交信息符合规范
- ✅ 新增逻辑有对应测试

---

## 💡 代码风格

- 所有导出标识符必须有**中文注释**（本项目面向中文开发者）
- 注释解释**为什么**，而不是复述代码在做什么
- 错误处理必须**包裹上下文**：`fmt.Errorf("读取分片失败: %w", err)`
- 优先使用标准库，引入任何依赖前请先开 Issue 讨论

### 目录约定

```
cmd/        → 可执行程序入口
pkg/        → 对外公开 API（可被其他项目 import）
internal/   → 内部实现（Go 编译器会阻止外部 import）
test/       → 测试辅助工具
scripts/    → 构建与发布脚本
```

---

## 🐛 反馈问题

提交 Issue 时请包含：

| 项目 | 内容 |
|------|------|
| 版本 | `veloxdl --version` 的输出 |
| 系统 | 操作系统与架构 |
| 复现步骤 | 完整命令行 |
| 期望 / 实际 | 你以为会发生什么 / 实际发生了什么 |
| 日志 | 去掉敏感信息后的完整输出 |

> ⚠️ 贴日志前请记得**删掉 URL 里的签名参数、Token 等敏感内容**。

---

<div align="center">

再次感谢你的贡献 🙏

</div>
