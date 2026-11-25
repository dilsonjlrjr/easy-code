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
	$(DOCKER_GO) $(BUILD_IMAGE) go test -race -count=1 ./internal/... .

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

# The product icon (generated from project/brand by `make -C project icons`) reaches each system this way:
# - macOS: the bundle icon (build/darwin/icons.icns) is the app, Dock and notification icon; notifications
#   also need the bundle identifier of build/darwin/Info.plist, so the app runs as EasyCode.app.
# - Windows: rsrc_windows_*.syso embeds build/windows/icon.ico in the .exe (taskbar, toasts); `make syso`
#   regenerates them after an icon change.
# - Linux: `make install-linux` puts the desktop entry and the hicolor icons in the user's data folder.
# The window, tray and notification images are embedded in the binary (main.go).
PLIST_VERSION ?= 0.1.0
DATA_HOME     ?= $(or $(XDG_DATA_HOME),$(HOME)/.local/share)

.PHONY: package-darwin syso install-linux

# Runs on macOS (Apple Silicon), with Go 1.26, Node 24 and the Xcode Command Line Tools. Ad-hoc signed:
# Gatekeeper asks once until the app is signed with a Developer ID.
package-darwin:
	cd ui && npm ci --no-audit --no-fund && npm run build
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -tags production -trimpath \
		-ldflags "-s -w -X main.version=$(VERSION)" -o bin/easycode-desktop-darwin-arm64 .
	rm -rf bin/EasyCode.app
	mkdir -p bin/EasyCode.app/Contents/MacOS bin/EasyCode.app/Contents/Resources
	cp bin/easycode-desktop-darwin-arm64 bin/EasyCode.app/Contents/MacOS/easycode-desktop
	cp build/darwin/icons.icns bin/EasyCode.app/Contents/Resources/icons.icns
	sed 's/>VERSION</>$(PLIST_VERSION)</' build/darwin/Info.plist > bin/EasyCode.app/Contents/Info.plist
	codesign --force --deep --sign - bin/EasyCode.app
	cd bin && ditto -c -k --keepParent EasyCode.app EasyCode-macos-arm64.zip

syso: image cache
	$(DOCKER_GO) $(BUILD_IMAGE) sh -c 'for arch in amd64 arm64; do \
		go run github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0 generate syso -arch $$arch \
		-icon build/windows/icon.ico -manifest build/windows/easycode-desktop.exe.manifest \
		-out rsrc_windows_$$arch.syso || exit 1; done'

install-linux:
	install -Dm755 bin/easycode-desktop "$(HOME)/.local/bin/easycode-desktop"
	install -Dm644 build/linux/br.easycode.desktop.desktop "$(DATA_HOME)/applications/br.easycode.desktop.desktop"
	cd build/linux/icons && find hicolor -name '*.png' -exec install -Dm644 {} "$(DATA_HOME)/icons/{}" \;
	-gtk-update-icon-cache -q -t "$(DATA_HOME)/icons/hicolor" 2>/dev/null
	-update-desktop-database -q "$(DATA_HOME)/applications" 2>/dev/null
