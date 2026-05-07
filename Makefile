.PHONY: test test-go test-python test-js build

test: test-go test-python test-js

test-go:
	go test ./internal/... -v -count=1

test-python:
	cd picoaide-desktop && python3 -m pytest tests/ -v

test-js:
	cd picoaide-extension && node --test 'tests/*.test.js'

build:
	go build -o picoaide ./cmd/picoaide/
