PICOAIDE_VERSION ?= $(shell git describe --tags --always)
PROGRAM_VERSION := $(patsubst v%,%,$(PICOAIDE_VERSION))
SERVER_VERSION_LDFLAGS := -X github.com/picoaide/picoaide/internal/config.Version=$(PROGRAM_VERSION)

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
	rm -rf dist/
	rm -rf picoaide-desktop/dist/ picoaide-desktop/build/

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

release: validate-release-version
	@echo "构建服务端二进制... (版本: $(PROGRAM_VERSION))"
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -ldflags "$(SERVER_VERSION_LDFLAGS)" -o dist/picoaide-linux-amd64 ./cmd/picoaide/
	GOOS=linux GOARCH=arm64 go build -ldflags "$(SERVER_VERSION_LDFLAGS)" -o dist/picoaide-linux-arm64 ./cmd/picoaide/
	@echo "服务端构建完成，产物在 dist/ 目录"
