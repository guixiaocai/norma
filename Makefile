SHELL := /bin/bash
VERSION ?= v0.2.0
.PHONY: build install release clean
build:
	VERSION=$(VERSION) ./tools/build.sh
install:
	./tools/install.sh
release: build
	VERSION=$(VERSION) ./tools/package.sh
clean:
	rm -rf bin dist
