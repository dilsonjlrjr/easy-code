# easycode-desktop (ADR-20). Every target runs inside pinned containers, so the host needs only Docker (RNF-17).
# The Go image adds the GTK 4 and WebKitGTK 6 headers that Wails 3 links on Linux (build/Dockerfile).
BUILD_IMAGE ?= easycode-desktop-build:ci
NODE_IMAGE  ?= node:24.22.3-bookworm-slim
VERSION     ?= dev
GOOS        ?= linux
GOARCH      ?= amd64

UID_GID   := $(shell id -u):$(shell id -g)
DOCKER_GO := docker run --rm -u $(UID_GID) -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod \
             -v "$(CURDIR)":/src -w /src -v easycode-go-cache:/cache
DOCKER_NODE := docker run --rm -u $(UID_GID) -e HOME=/tmp -e CI=1 -e npm_config_cache=/cache/npm \
               -v easycode-npm-cache:/cache -v "$(CURDIR)/ui":/app -w /app

.PHONY: ci image cache npm-cache tidy fmt-check vet test ui-deps ui-lint ui-type-check ui-test ui-build build

ci: fmt-check vet test ui-lint ui-type-check ui-test build
	@echo "DESKTOP CI OK"

image:
	docker build -q -t $(BUILD_IMAGE) build >/dev/null

cache:
	@docker volume create easycode-go-cache >/dev/null
	@docker run --rm -v easycode-go-cache:/cache $(BUILD_IMAGE) chown $(UID_GID) /cache

npm-cache:
	@docker volume create easycode-npm-cache >/dev/null
	@docker run --rm -v easycode-npm-cache:/cache $(NODE_IMAGE) chown $(UID_GID) /cache

tidy: image cache
	$(DOCKER_GO) $(BUILD_IMAGE) go mod tidy

fmt-check: image
	@out="$$($(DOCKER_GO) $(BUILD_IMAGE) gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet: image cache
	$(DOCKER_GO) $(BUILD_IMAGE) go vet ./...

# Unit tests of config, keychain (fake backend), device flow, REST and SSE clients and the app core, all
# against fake servers; race detector on.
test: image cache
	$(DOCKER_GO) $(BUILD_IMAGE) go test -race -count=1 ./internal/...

ui-deps: npm-cache
	$(DOCKER_NODE) $(NODE_IMAGE) npm ci --no-audit --no-fund

ui-lint: ui-deps
	$(DOCKER_NODE) $(NODE_IMAGE) npm run lint

ui-type-check: ui-deps
	$(DOCKER_NODE) $(NODE_IMAGE) npm run type-check

# Vitest: views, i18n catalogs and the axe accessibility scan of every view (RNF-19).
ui-test: ui-deps
	$(DOCKER_NODE) $(NODE_IMAGE) npm run test

ui-build: ui-deps
	$(DOCKER_NODE) $(NODE_IMAGE) npm run build

# Linux binary (cgo, WebKitGTK). macOS and Windows builds run on their own OS: see README.md.
build: ui-build image cache
	$(DOCKER_GO) -e CGO_ENABLED=1 -e GOOS=$(GOOS) -e GOARCH=$(GOARCH) $(BUILD_IMAGE) \
		go build -tags production -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/easycode-desktop .
