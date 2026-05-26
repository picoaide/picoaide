PICOAIDE_VERSION ?= $(shell git describe --tags --always)
PROGRAM_VERSION := $(patsubst v%,%,$(PICOAIDE_VERSION))
SERVER_VERSION_LDFLAGS := -X github.com/picoaide/picoaide/internal/config.Version=$(PROGRAM_VERSION)

BUNDLE_DIR := internal/rootfs/bundle

.PHONY: test lint format check clean validate-release-version build release

test:
	go test ./internal/... -v -count=1

lint:
	@golangci-lint run ./... 2>/dev/null || go vet ./...

format:
	./format.sh

check: format lint test

clean:
	rm -f picoaide picoagent
	rm -rf dist/

validate-release-version:
	@case "$(PROGRAM_VERSION)" in \
		""|*[!0-9a-zA-Z._+-]*|.*|*.) \
			echo "发布版本号格式无效，例如: make release PICOAIDE_VERSION=v1.0.0 或 v1.0.0-rc.1"; \
			exit 1; \
			;; \
		*..*) \
			echo "发布版本号格式无效，例如: make release PICOAIDE_VERSION=v1.0.0 或 v1.0.0-rc.1"; \
			exit 1; \
			;; \
	esac

# ============================================================
# build: 编译 picoagent + Alpine rootfs + 嵌入 picoagent → 编译 picoaide
# ============================================================
build:
	@mkdir -p $(BUNDLE_DIR)
	@ARCH="$$(uname -m)"; \
	case "$$ARCH" in \
		x86_64) GOARCH="amd64" ;; \
		aarch64|arm64) GOARCH="arm64" ;; \
		*) echo "不支持的架构: $$ARCH"; exit 1 ;; \
	esac; \
	echo "  编译 picoagent ($$GOARCH)..."; \
	CGO_ENABLED=0 GOOS=linux GOARCH=$$GOARCH go build -o $(BUNDLE_DIR)/picoagent ./cmd/picoagent/; \
	echo "  构建 Alpine rootfs..."; \
	bash scripts/download-tools.sh "$(CURDIR)/$(BUNDLE_DIR)"; \
	echo "  编译 picoaide..."; \
	go build -o picoaide ./cmd/picoaide/; \
	echo "picoaide: $$(ls -lh picoaide | awk '{print $$5}')"

# ============================================================
# release: 交叉编译多架构二进制（版本号注入）
# ============================================================
release: validate-release-version
	@echo "构建发布版本 $(PROGRAM_VERSION)..."
	@mkdir -p dist $(BUNDLE_DIR)
	@for pair in "amd64" "arm64"; do \
		GOARCH="$$pair"; \
		echo "  编译 picoagent ($$GOARCH)..."; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$GOARCH go build \
			-o $(BUNDLE_DIR)/picoagent ./cmd/picoagent/; \
		echo "  构建 Alpine rootfs (仅 amd64 一次)..."; \
		if [ ! -f $(BUNDLE_DIR)/alpine-rootfs.tar.gz ]; then \
			bash scripts/download-tools.sh "$(CURDIR)/$(BUNDLE_DIR)"; \
		fi; \
		echo "  编译 picoaide (linux/$$GOARCH)..."; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$GOARCH go build \
			-ldflags "$(SERVER_VERSION_LDFLAGS)" \
			-o dist/picoaide-linux-$$GOARCH ./cmd/picoaide/; \
		rm -f $(BUNDLE_DIR)/picoagent; \
	done
	@echo "构建完成:"
	@ls -lh dist/
