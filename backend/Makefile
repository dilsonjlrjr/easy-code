# Every target runs inside pinned containers, so the host needs only Docker.
GO_IMAGE        ?= golang:1.26.10-bookworm
LINT_IMAGE      ?= golangci/golangci-lint:v2.15.0
VERSION         ?= dev
BINARY          ?= easycode-api
BINARIES        := easycode-api easycode-worker easycode-sandbox easycode-gateway
DOCKER_GO       := docker run --rm -u $(shell id -u):$(shell id -g) -e HOME=/tmp \
                   -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e GOLANGCI_LINT_CACHE=/cache/lint \
                   -v "$(CURDIR)":/src -w /src -v easycode-go-cache:/cache
TEST_NETWORK    ?=
RUN_ENV         ?= --network host -e EASYCODE_DATABASE_URL -e EASYCODE_HTTP_ADDR -e EASYCODE_LOG_LEVEL

.PHONY: build test test-race test-integration lint run migrate image fmt tidy cache ci

cache:
	@docker volume create easycode-go-cache >/dev/null
	@docker run --rm -v easycode-go-cache:/cache $(GO_IMAGE) chown $(shell id -u):$(shell id -g) /cache

build: cache
	$(DOCKER_GO) -e CGO_ENABLED=0 $(GO_IMAGE) sh -c '$(foreach b,$(BINARIES),go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/$(b) ./cmd/$(b) &&) go vet ./...'

# Set EASYCODE_TEST_DATABASE_URL to a disposable database to run the migrator integration test.
test: cache
	$(DOCKER_GO) -e EASYCODE_TEST_DATABASE_URL $(TEST_NETWORK) $(GO_IMAGE) go test -count=1 ./...

test-race: cache
	$(DOCKER_GO) -e EASYCODE_TEST_DATABASE_URL $(TEST_NETWORK) $(GO_IMAGE) go test -race -count=1 ./...

# Runs every test, integration included, against a throwaway PostgreSQL container.
test-integration:
	./scripts/with-postgres.sh $(MAKE) test

# What CI runs; needs only Docker. From the repo root: make -C project/backend ci
ci: lint test-integration build

lint: cache
	$(DOCKER_GO) $(LINT_IMAGE) golangci-lint run ./...

fmt: cache
	$(DOCKER_GO) $(GO_IMAGE) gofmt -w .

tidy: cache
	$(DOCKER_GO) $(GO_IMAGE) go mod tidy

# Runs a binary against the database in EASYCODE_DATABASE_URL, e.g. make run BINARY=easycode-worker.
run: build
	$(DOCKER_GO) $(RUN_ENV) $(GO_IMAGE) ./bin/$(BINARY)

migrate: build
	$(DOCKER_GO) $(RUN_ENV) -e EASYCODE_MIGRATION_DATABASE_URL $(GO_IMAGE) ./bin/easycode-api migrate

image:
	docker build --build-arg BINARY=$(BINARY) --build-arg VERSION=$(VERSION) -t $(BINARY):$(VERSION) .
