SHELL := /bin/bash
VERSION ?= v0.3.0
.DEFAULT_GOAL := build
.PHONY: build generate test install release clean
build:
	VERSION=$(VERSION) ./tools/build.sh
generate:
	./tools/generate.sh
test:
	go test ./...
install:
	./tools/install.sh
release: build
	VERSION=$(VERSION) ./tools/package.sh
clean:
	rm -rf bin dist
