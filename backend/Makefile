# Every target runs inside pinned containers, so the host needs only Docker.
GO_IMAGE        ?= golang:1.26.10-bookworm
LINT_IMAGE      ?= golangci/golangci-lint:v2.15.0
VERSION         ?= dev
BINARY          ?= easycode-api
BINARIES        := easycode-api easycode-worker easycode-sandbox easycode-gateway easycode-installer easycode-agent-bridge
DOCKER_GO       := docker run --rm -u $(shell id -u):$(shell id -g) -e HOME=/tmp \
                   -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e GOLANGCI_LINT_CACHE=/cache/lint \
                   -v "$(CURDIR)":/src -w /src -v easycode-go-cache:/cache
TEST_NETWORK    ?=
# The sandbox tests (RNF-02, RNF-03, US-41) and the MARCO-3 end-to-end test drive the host Docker daemon
# through its socket, like easycode-sandbox does; without the socket they are skipped.
SANDBOX_TEST_IMAGE ?= easycode-sandbox-go:ci
# Security scan image of the Segurança (US-23): Opengrep, Trivy and Gitleaks with their offline database.
SECURITY_TEST_IMAGE ?= easycode-sandbox-security:ci
DOCKER_SOCKET   ?= /var/run/docker.sock
DOCKER_GID      := $(shell stat -c %g $(DOCKER_SOCKET) 2>/dev/null)
# The agent session test (ADR-23) binds a fake Claude Code from this directory (host path) and serves the
# wave model socket in a volume shared with the sandbox containers.
AGENT_PROXY_TEST_VOLUME ?= easycode-agent-proxy-test
TEST_AGENT      := -e "EASYCODE_TEST_HOST_DIR=$(CURDIR)" -v $(AGENT_PROXY_TEST_VOLUME):/run/easycode-agent-proxy-test \
                   -e EASYCODE_TEST_AGENT_PROXY_VOLUME=$(AGENT_PROXY_TEST_VOLUME) -e EASYCODE_TEST_AGENT_PROXY_DIR=/run/easycode-agent-proxy-test
# The recovery test of RNF-11 runs the backup and restore scripts of ../infra/backup (read-only mount).
# The realm template of easycode-installer is checked against the development realm (keycloakadmin tests).
TEST_REALM      := -v "$(CURDIR)/../infra/keycloak":/infra/keycloak:ro -e EASYCODE_TEST_DEV_REALM=/infra/keycloak/easycode-realm.json
TEST_DOCKER     := $(if $(DOCKER_GID),-v $(DOCKER_SOCKET):/var/run/docker.sock --group-add $(DOCKER_GID) -e EASYCODE_TEST_DOCKER_SOCKET=/var/run/docker.sock -e EASYCODE_TEST_SANDBOX_IMAGE=$(SANDBOX_TEST_IMAGE) -e EASYCODE_TEST_SECURITY_IMAGE=$(SECURITY_TEST_IMAGE) -v "$(CURDIR)/../infra/backup":/infra/backup:ro -e EASYCODE_TEST_BACKUP_SCRIPTS=/infra/backup $(TEST_AGENT),)
RUN_ENV         ?= --network host -e EASYCODE_DATABASE_URL -e EASYCODE_HTTP_ADDR -e EASYCODE_LOG_LEVEL

.PHONY: build test test-race test-integration lint run migrate image fmt tidy cache ci check-specs sync-specs sandbox-image load-test test-load

cache:
	@docker volume create easycode-go-cache >/dev/null
	@docker run --rm -v easycode-go-cache:/cache $(GO_IMAGE) chown $(shell id -u):$(shell id -g) /cache
	@docker volume create $(AGENT_PROXY_TEST_VOLUME) >/dev/null
	@docker run --rm -v $(AGENT_PROXY_TEST_VOLUME):/v $(GO_IMAGE) chown $(shell id -u):$(shell id -g) /v

build: cache
	$(DOCKER_GO) -e CGO_ENABLED=0 $(GO_IMAGE) sh -c '$(foreach b,$(BINARIES),go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/$(b) ./cmd/$(b) &&) go vet ./...'

# Set EASYCODE_TEST_DATABASE_URL to a disposable database to run the migrator integration test.
test: cache
	$(DOCKER_GO) -e EASYCODE_TEST_DATABASE_URL $(TEST_NETWORK) $(TEST_DOCKER) $(TEST_REALM) $(GO_IMAGE) go test -count=1 -timeout 30m ./...

test-race: cache
	$(DOCKER_GO) -e EASYCODE_TEST_DATABASE_URL $(TEST_NETWORK) $(TEST_DOCKER) $(TEST_REALM) $(GO_IMAGE) go test -race -count=1 -timeout 30m ./...

# Runs every test, integration included, against a throwaway PostgreSQL container.
test-integration: sandbox-image
	./scripts/with-postgres.sh $(MAKE) test

# RNF-09: the load test at the scale of assumption S-4 (100 projects, 20,000 waves, 50 people at once) against a
# throwaway PostgreSQL; it fails when a read of the screens passes 500 ms at p95 or the write 1 s. Out of ci: it
# needs a host without other load (it seeds for about a minute and runs for minutes).
load-test:
	./scripts/with-postgres.sh $(MAKE) test-load

test-load: cache
	$(DOCKER_GO) -e EASYCODE_TEST_DATABASE_URL -e EASYCODE_LOAD_TEST=1 $(TEST_NETWORK) $(GO_IMAGE) go test -count=1 -timeout 20m -run 'TestRNF09' -v ./internal/interfaces/http/

# Sandbox images used by the sandbox tests: Go with the warm build cache (ADR-07) and the security scans (US-23).
sandbox-image:
	docker build -q -t $(SANDBOX_TEST_IMAGE) ../infra/sandbox/go >/dev/null
	docker build -q -t $(SECURITY_TEST_IMAGE) ../infra/sandbox/security >/dev/null

# The binaries embed copies of domain specs (workflow, artifact schemas, permissions) and agent
# instructions, so builds are reproducible from this repository alone. The sources live in a separate
# specification workspace (specs/ and agentes/), found at SPECS_SOURCE. check-specs fails when a copy
# drifts from its source and is skipped when the workspace is absent; sync-specs refreshes the copies.
SPECS_DIR    := internal/infrastructure/specs/data
SPECS_SOURCE ?= ../..
HAS_SPECS    := $(shell test -d "$(SPECS_SOURCE)/specs/dominio" && test -d "$(SPECS_SOURCE)/agentes/instrucoes" && echo yes)

check-specs:
ifeq ($(HAS_SPECS),)
	@echo "specification workspace not found at $(SPECS_SOURCE): skipping check-specs"
else
	@cmp -s $(SPECS_SOURCE)/specs/dominio/workflow/workflow-basico.yaml $(SPECS_DIR)/workflow/workflow-basico.yaml || { echo "drift: workflow-basico.yaml (make sync-specs)"; exit 1; }
	@cmp -s $(SPECS_SOURCE)/specs/dominio/workflow/workflow-padrao.yaml $(SPECS_DIR)/workflow/workflow-padrao.yaml || { echo "drift: workflow-padrao.yaml (make sync-specs)"; exit 1; }
	@cmp -s $(SPECS_SOURCE)/specs/dominio/governanca/permissoes.yaml $(SPECS_DIR)/governanca/permissoes.yaml || { echo "drift: permissoes.yaml (make sync-specs)"; exit 1; }
	@cmp -s $(SPECS_SOURCE)/specs/dominio/governanca/gates.yaml $(SPECS_DIR)/governanca/gates.yaml || { echo "drift: gates.yaml (make sync-specs)"; exit 1; }
	@cmp -s $(SPECS_SOURCE)/specs/dominio/governanca/checagens.yaml $(SPECS_DIR)/governanca/checagens.yaml || { echo "drift: checagens.yaml (make sync-specs)"; exit 1; }
	@diff -rq $(SPECS_SOURCE)/specs/dominio/artefatos $(SPECS_DIR)/artefatos --exclude=README.md
	@diff -rq $(SPECS_SOURCE)/agentes/instrucoes $(SPECS_DIR)/agentes --exclude=README.md
	@diff -rq $(SPECS_SOURCE)/agentes/skills $(SPECS_DIR)/skills --exclude=README.md
	@cmp -s $(SPECS_SOURCE)/agentes/referencia/reserva-de-salas.md $(SPECS_DIR)/referencia/reserva-de-salas.md || { echo "drift: reserva-de-salas.md (make sync-specs)"; exit 1; }
	@echo "embedded specs in sync"
endif

sync-specs:
ifeq ($(HAS_SPECS),)
	$(error specification workspace not found at $(SPECS_SOURCE); set SPECS_SOURCE)
endif
	cp $(SPECS_SOURCE)/specs/dominio/workflow/workflow-basico.yaml $(SPECS_SOURCE)/specs/dominio/workflow/workflow-padrao.yaml $(SPECS_DIR)/workflow/
	cp $(SPECS_SOURCE)/specs/dominio/governanca/permissoes.yaml $(SPECS_SOURCE)/specs/dominio/governanca/gates.yaml $(SPECS_SOURCE)/specs/dominio/governanca/checagens.yaml $(SPECS_DIR)/governanca/
	cp $(SPECS_SOURCE)/specs/dominio/artefatos/*.schema.json $(SPECS_DIR)/artefatos/
	cp $(SPECS_SOURCE)/agentes/referencia/reserva-de-salas.md $(SPECS_DIR)/referencia/
	rm -rf $(SPECS_DIR)/skills && cp -r $(SPECS_SOURCE)/agentes/skills $(SPECS_DIR)/skills && rm $(SPECS_DIR)/skills/README.md
	for f in $(SPECS_SOURCE)/agentes/instrucoes/*.md; do case "$$f" in */README.md) ;; *) cp "$$f" $(SPECS_DIR)/agentes/ ;; esac; done

# What CI runs; needs only Docker. From the repository root: make -C backend ci
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
