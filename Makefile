BINARY  := nanopanel
DIST    := dist
DEPLOY  := deploy
GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
SHA256  ?= $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo "shasum -a 256")

LDFLAGS := -s -w \
	-X nanopanel/internal/version.Version=$(VERSION) \
	-X nanopanel/internal/version.Commit=$(COMMIT) \
	-X nanopanel/internal/version.Date=$(DATE)

.PHONY: help fmt fmt-check vet test check build build-all dev clean

help:
	@echo "NanoPanel 常用命令"
	@echo "  make check       格式检查 + vet + 单元测试"
	@echo "  make build       编译当前平台到 dist/"
	@echo "  make build-all   交叉编译 linux amd64/arm64/armv7 并同步到 deploy/"
	@echo "  make dev         生成 dev/ 配置并启动开发版 (:18080)"
	@echo "  make clean       清理构建产物与 dev 配置"

fmt:
	$(GO) fmt ./...

fmt-check:
	@test -z "$$($(GO)fmt -l .)" || { echo "以下文件未格式化:"; $(GO)fmt -l .; exit 1; }

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

check: fmt-check vet test

build:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BINARY) .

# 产物命名必须与 deploy/install.sh 中的 nanopanel-<ARCH> 一致。
build-all:
	@mkdir -p $(DIST) $(DEPLOY)
	@set -e; for spec in "amd64:amd64" "arm64:arm64" "armv7:arm"; do \
		name=$${spec%%:*}; arch=$${spec##*:}; \
		echo "  -> linux/$$arch => $(BINARY)-$$name"; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch GOARM=7 \
			$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BINARY)-linux-$$name .; \
		cp $(DIST)/$(BINARY)-linux-$$name $(DEPLOY)/$(BINARY)-$$name; \
		(cd $(DEPLOY) && $(SHA256) $(BINARY)-$$name > $(BINARY)-$$name.sha256); \
	done
	@echo "完成：$(DEPLOY)/ 下已生成三个架构的二进制与校验和"

dev:
	@mkdir -p dev
	@$(GO) run . -init -c dev/config.json -username admin -password devonly -port 18080 >/dev/null
	@echo "开发版: http://127.0.0.1:18080  用户名 admin  密码 devonly"
	@$(GO) run . -dev -c dev/config.json

clean:
	rm -rf $(DIST) dev
	rm -f $(DEPLOY)/$(BINARY)-amd64 $(DEPLOY)/$(BINARY)-arm64 $(DEPLOY)/$(BINARY)-armv7
	rm -f $(DEPLOY)/$(BINARY)-*.sha256
