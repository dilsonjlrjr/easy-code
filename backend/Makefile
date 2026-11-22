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

.PHONY: build test test-race test-integration lint run migrate image fmt tidy cache ci check-specs sync-specs

cache:
	@docker volume create easycode-go-cache >/dev/null
	@docker run --rm -v easycode-go-cache:/cache $(GO_IMAGE) chown $(shell id -u):$(shell id -g) /cache

build: cache
	$(DOCKER_GO) -e CGO_ENABLED=0 $(GO_IMAGE) sh -c '$(foreach b,$(BINARIES),go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/$(b) ./cmd/$(b) &&) go vet ./...'

# Set EASYCODE_TEST_DATABASE_URL to a disposable database to run the migrator integration test.
test: cache
	$(DOCKER_GO) -e EASYCODE_TEST_DATABASE_URL $(TEST_NETWORK) $(GO_IMAGE) go test -count=1 -timeout 20m ./...

test-race: cache
	$(DOCKER_GO) -e EASYCODE_TEST_DATABASE_URL $(TEST_NETWORK) $(GO_IMAGE) go test -race -count=1 -timeout 20m ./...

# Runs every test, integration included, against a throwaway PostgreSQL container.
test-integration:
	./scripts/with-postgres.sh $(MAKE) test

# The binaries embed copies of domain specs (workflow, artifact schemas, permissions) and agent
# instructions, so builds are reproducible from project/backend alone. check-specs fails when a copy
# drifts from its source in the repository; sync-specs refreshes the copies.
SPECS_DIR := internal/infrastructure/specs/data

check-specs:
	@cmp -s ../../specs/dominio/workflow/workflow-basico.yaml $(SPECS_DIR)/workflow/workflow-basico.yaml || { echo "drift: workflow-basico.yaml (make sync-specs)"; exit 1; }
	@cmp -s ../../specs/dominio/governanca/permissoes.yaml $(SPECS_DIR)/governanca/permissoes.yaml || { echo "drift: permissoes.yaml (make sync-specs)"; exit 1; }
	@diff -rq ../../specs/dominio/artefatos $(SPECS_DIR)/artefatos --exclude=README.md
	@diff -rq ../../agentes/instrucoes $(SPECS_DIR)/agentes --exclude=README.md
	@cmp -s ../../agentes/referencia/reserva-de-salas.md $(SPECS_DIR)/referencia/reserva-de-salas.md || { echo "drift: reserva-de-salas.md (make sync-specs)"; exit 1; }
	@echo "embedded specs in sync"

sync-specs:
	cp ../../specs/dominio/workflow/workflow-basico.yaml $(SPECS_DIR)/workflow/
	cp ../../specs/dominio/governanca/permissoes.yaml $(SPECS_DIR)/governanca/
	cp ../../specs/dominio/artefatos/*.schema.json $(SPECS_DIR)/artefatos/
	cp ../../agentes/referencia/reserva-de-salas.md $(SPECS_DIR)/referencia/
	for f in ../../agentes/instrucoes/*.md; do case "$$f" in */README.md) ;; *) cp "$$f" $(SPECS_DIR)/agentes/ ;; esac; done

# What CI runs; needs only Docker. From the repo root: make -C project/backend ci
ci: check-specs lint test-integration build

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
