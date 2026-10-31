# VeloxDL Makefile
#
# 常用命令:
#   make            # 等同于 make build（只构建当前平台）
#   make build      # 构建当前平台二进制到 bin/
#   make test       # 运行单元测试
#   make race       # 运行带竞态检测的测试
#   make coverage   # 生成覆盖率报告
#   make dist       # 交叉编译全部平台到 dist/
#   make fmt        # 格式化代码
#   make vet        # 静态检查
#   make check      # fmt + vet + test（提交前自检）
#   make install    # 安装到 GOBIN
#   make clean      # 清理构建产物

BINARY      := veloxdl
PKG         := ./cmd/veloxdl
BIN_DIR     := bin
DIST_DIR    := dist
VERSION     ?= $(shell grep -oE 'Version[[:space:]]*=[[:space:]]*"[^"]+"' pkg/veloxdl/api.go | sed -E 's/.*"(.*)".*/\1/')
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)

.PHONY: all build test race coverage dist fmt vet check install clean help

all: build

## build: 构建当前平台二进制
build:
	@echo "🔨 构建 $(BINARY) $(VERSION)"
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(PKG)
	@echo "✅ 产物: $(BIN_DIR)/$(BINARY)"

## test: 运行单元测试
test:
	@echo "🧪 运行单元测试"
	go test ./...

## race: 运行带竞态检测的测试
race:
	@echo "🏁 运行竞态检测"
	go test -race ./...

## coverage: 生成覆盖率报告
coverage:
	@echo "📊 生成覆盖率报告"
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	go tool cover -func=coverage.out | tail -5

## dist: 交叉编译全部平台
dist:
	@./scripts/build.sh

## fmt: 格式化代码
fmt:
	gofmt -w ./cmd ./internal ./pkg
	@echo "✅ 格式化完成"

## vet: 静态检查
vet:
	go vet ./...
	@echo "✅ 静态检查通过"

## check: 提交前自检（格式化 + 静态检查 + 竞态测试）
check: fmt vet race
	@echo "🎉 全部检查通过"

## install: 安装到 GOBIN
install:
	@echo "📥 安装 $(BINARY) $(VERSION)"
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)
	@echo "✅ 已安装到 $$(go env GOPATH)/bin/$(BINARY)"

## clean: 清理构建产物
clean:
	rm -rf $(BIN_DIR) $(DIST_DIR) coverage.out coverage.html
	@echo "🧹 已清理"

## help: 显示帮助
help:
	@echo "VeloxDL 可用命令:"
	@grep -E '^## [a-zA-Z_-]+:' $(MAKEFILE_LIST) | sed 's/## /  /'
