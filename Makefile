SHELL := /bin/bash
VERSION ?= v0.5.0
.DEFAULT_GOAL := build
.PHONY: build generate test test-install install release rpm clean
build:
	VERSION=$(VERSION) ./tools/build.sh
generate:
	./tools/generate.sh
test:
	go test ./...
	./tools/test-install.sh
	./tools/test-lifecycle.sh
test-install:
	./tools/test-install.sh
	./tools/test-lifecycle.sh
install:
	./tools/install.sh
release: build
	VERSION=$(VERSION) ./tools/package.sh
rpm: build
	VERSION=$(VERSION) ./tools/package-rpm.sh
clean:
	rm -rf bin dist
