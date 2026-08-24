GO_CACHE := $(CURDIR)/.gocache
BIN_DIR ?= $(CURDIR)/dist/bin
GO_BUILD := env GOCACHE=$(GO_CACHE) go build
GO_TEST := env GOCACHE=$(GO_CACHE) go test

.PHONY: build build-site build-agent build-server build-feishu test test-go test-site docs-locales e2e browser-regression open-source-check release-snapshot verify-release clean

build: build-site build-agent build-server build-feishu

build-site:
	npm run build

build-agent:
	mkdir -p $(BIN_DIR)
	$(GO_BUILD) -o $(BIN_DIR)/paper-agent ./examples/paper-agent/cmd/paper-agent

build-server:
	mkdir -p $(BIN_DIR)
	$(GO_BUILD) -o $(BIN_DIR)/paper-agent-server ./examples/paper-agent/cmd/paper-agent-server

build-feishu:
	mkdir -p $(BIN_DIR)
	$(GO_BUILD) -o $(BIN_DIR)/feishu-adapter ./examples/paper-agent/cmd/feishu-adapter

test: docs-locales test-site test-go

docs-locales:
	npm run docs:locales

test-site:
	npm --prefix ai-agent-roadmap-site test

test-go:
	$(GO_TEST) ./examples/paper-agent/...

e2e: build-agent build-server
	bash scripts/e2e.sh

browser-regression: build-server
	bash scripts/browser-regression.sh

open-source-check:
	bash scripts/open-source-check.sh

release-snapshot: build-agent build-server build-feishu
	bash scripts/package-release.sh

verify-release:
	bash scripts/verify-release.sh

clean:
	rm -rf $(CURDIR)/dist $(GO_CACHE)
