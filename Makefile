PICOAIDE_VERSION ?= $(shell git describe --tags --always)
PROGRAM_VERSION := $(patsubst v%,%,$(PICOAIDE_VERSION))
SERVER_VERSION_LDFLAGS := -X github.com/picoaide/picoaide/internal/config.Version=$(PROGRAM_VERSION)
# Chrome manifest 需要纯数字版本，截取主版本部分（如 "1.0.0-rc.1" → "1.0.0"）
EXTENSION_VERSION := $(shell echo "$(PROGRAM_VERSION)" | sed 's/[-].*//')

.PHONY: test test-go test-python test-js build lint format check clean release validate-release-version

test: test-go test-python test-js

test-go:
	go test ./internal/... -v -count=1

test-python:
	cd picoaide-desktop && python3 -m pytest tests/ -v

test-js:
	cd picoaide-extension && node --test 'tests/*.test.js'

build:
	go build -o picoaide ./cmd/picoaide/

lint:
	golangci-lint run ./...

format:
	./format.sh

check: format lint test

clean:
	rm -f picoaide
	rm -rf picoaide-desktop/dist/ picoaide-desktop/build/
	rm -f picoaide-extension.zip

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
	@case "$(EXTENSION_VERSION)" in \
		""|*[!0-9.]*|.*|*.*.*.*.*|*.) \
			echo "扩展版本号必须为纯数字，从 PROGRAM_VERSION 中提取失败: $(EXTENSION_VERSION)"; \
			exit 1; \
			;; \
		*..*) \
			echo "扩展版本号必须为纯数字"; \
			exit 1; \
			;; \
	esac

release: validate-release-version
	@echo "构建全平台二进制... (版本: $(PROGRAM_VERSION))"
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -ldflags "$(SERVER_VERSION_LDFLAGS)" -o dist/picoaide-linux-amd64 ./cmd/picoaide/
	GOOS=linux GOARCH=arm64 go build -ldflags "$(SERVER_VERSION_LDFLAGS)" -o dist/picoaide-linux-arm64 ./cmd/picoaide/
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(SERVER_VERSION_LDFLAGS)" -o dist/picoaide-darwin-amd64 ./cmd/picoaide/
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(SERVER_VERSION_LDFLAGS)" -o dist/picoaide-darwin-arm64 ./cmd/picoaide/
	GOOS=windows GOARCH=amd64 go build -ldflags "$(SERVER_VERSION_LDFLAGS)" -o dist/picoaide-windows-amd64.exe ./cmd/picoaide/
	sh scripts/package-extension.sh "$(EXTENSION_VERSION)" dist/picoaide-extension.zip
	tmpdir=$$(mktemp -d); \
	trap 'rm -rf "$$tmpdir"' EXIT; \
	cp -R picoaide-desktop/. "$$tmpdir/picoaide-desktop/"; \
	sh scripts/set-desktop-version.sh "$(PROGRAM_VERSION)" "$$tmpdir/picoaide-desktop"; \
	cd "$$tmpdir/picoaide-desktop" && pyinstaller --onefile --name picoaide-desktop main.py; \
	cp "$$tmpdir"/picoaide-desktop/dist/picoaide-desktop* "$(CURDIR)/dist/"
	@echo "构建完成，产物在 dist/ 目录"
