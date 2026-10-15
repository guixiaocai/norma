SHELL := /bin/bash
VERSION ?= v0.4.0
.DEFAULT_GOAL := build
.PHONY: build generate test test-install install release clean
build:
	VERSION=$(VERSION) ./tools/build.sh
generate:
	./tools/generate.sh
test:
	go test ./...
	./tools/test-install.sh
test-install:
	./tools/test-install.sh
install:
	./tools/install.sh
release: build
	VERSION=$(VERSION) ./tools/package.sh
clean:
	rm -rf bin dist
