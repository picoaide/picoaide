.PHONY: test test-go test-python test-js build lint format check clean release

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

release:
	@echo "构建全平台二进制..."
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -ldflags "-X github.com/picoaide/picoaide/internal/config.Version=$(shell git describe --tags --always)" -o dist/picoaide-linux-amd64 ./cmd/picoaide/
	GOOS=linux GOARCH=arm64 go build -ldflags "-X github.com/picoaide/picoaide/internal/config.Version=$(shell git describe --tags --always)" -o dist/picoaide-linux-arm64 ./cmd/picoaide/
	GOOS=darwin GOARCH=amd64 go build -ldflags "-X github.com/picoaide/picoaide/internal/config.Version=$(shell git describe --tags --always)" -o dist/picoaide-darwin-amd64 ./cmd/picoaide/
	GOOS=darwin GOARCH=arm64 go build -ldflags "-X github.com/picoaide/picoaide/internal/config.Version=$(shell git describe --tags --always)" -o dist/picoaide-darwin-arm64 ./cmd/picoaide/
	GOOS=windows GOARCH=amd64 go build -ldflags "-X github.com/picoaide/picoaide/internal/config.Version=$(shell git describe --tags --always)" -o dist/picoaide-windows-amd64.exe ./cmd/picoaide/
	cd picoaide-extension && zip -r ../dist/picoaide-extension.zip .
	@echo "构建完成，产物在 dist/ 目录"
