# Single CI entry point for the platform. Any runner calls `make ci` from the repository root.
# Every step runs inside pinned containers, so the host needs only Docker and make (RNF-17).
# Paths may contain spaces: always keep them inside double quotes.

NODE_IMAGE       ?= node:24.22.3-bookworm-slim
PLAYWRIGHT_IMAGE ?= mcr.microsoft.com/playwright:v1.65.1-noble
PYTHON_IMAGE     ?= python:3.13.9-slim-bookworm
PY_DEPS          ?= jsonschema==4.10.3 attrs==26.1.0 pyrsistent==0.20.0 pyyaml==6.0.2

UID_GID   := $(shell id -u):$(shell id -g)
FRONTEND  := $(CURDIR)/frontend
# The specification workspace (specs/, docs/, agentes/) sits next to this repository when present.
SPECS_SOURCE ?= $(shell cd .. && pwd)
HAS_SPECS    := $(shell test -f "$(SPECS_SOURCE)/specs/dominio/ferramentas/validar_tudo.py" && echo yes)

DOCKER_NODE := docker run --rm -u $(UID_GID) -e HOME=/tmp -e CI=1 \
               -e npm_config_cache=/cache/npm -v easycode-npm-cache:/cache \
               -v "$(FRONTEND)":/app -w /app

.PHONY: ci backend-ci frontend-ci frontend-deps frontend-lint frontend-type-check frontend-test frontend-build \
        frontend-e2e specs npm-cache

ci: backend-ci frontend-ci frontend-e2e specs
	@echo "CI OK"

# Defined in backend/Makefile (lint, tests, build inside golang containers).
backend-ci:
	$(MAKE) -C backend ci

frontend-ci: frontend-lint frontend-type-check frontend-test frontend-build

npm-cache:
	@docker volume create easycode-npm-cache >/dev/null
	@docker run --rm -v easycode-npm-cache:/cache $(NODE_IMAGE) chown $(UID_GID) /cache

frontend-deps: npm-cache
	$(DOCKER_NODE) $(NODE_IMAGE) npm ci --no-audit --no-fund

frontend-lint: frontend-deps
	$(DOCKER_NODE) $(NODE_IMAGE) npm run lint

frontend-type-check: frontend-deps
	$(DOCKER_NODE) $(NODE_IMAGE) npm run type-check

frontend-test: frontend-deps
	$(DOCKER_NODE) $(NODE_IMAGE) npm run test

# The platform web and the installation wizard page (US-55).
frontend-build: frontend-deps
	$(DOCKER_NODE) $(NODE_IMAGE) npm run build-only
	$(DOCKER_NODE) $(NODE_IMAGE) npm run build-installer

# Playwright against the production build (playwright.config.ts starts `vite preview`).
frontend-e2e: frontend-deps
	docker run --rm --init --ipc=host -u $(UID_GID) -e HOME=/tmp -e CI=1 \
		-v "$(FRONTEND)":/work -w /work $(PLAYWRIGHT_IMAGE) npx playwright test

# Domain, requirements, architecture and data validators of the specification workspace, mounted read-only.
# Skipped when the workspace is absent (a clone of this repository alone).
# jsonschema stays on 4.10.x: the validators use RefResolver, which newer releases no longer resolve the same way.
specs:
ifeq ($(HAS_SPECS),)
	@echo "specification workspace not found at $(SPECS_SOURCE): skipping specs"
else
	docker run --rm -e PIP_ROOT_USER_ACTION=ignore -e PIP_DISABLE_PIP_VERSION_CHECK=1 \
		-v "$(SPECS_SOURCE)":/repo:ro -w /repo $(PYTHON_IMAGE) \
		sh -c 'pip install -q --no-cache-dir $(PY_DEPS) && python3 specs/dominio/ferramentas/validar_tudo.py'
endif

# easycode-desktop: Go and UI tests plus the Linux binary, in containers. Not part of `ci`: the first
# run builds an image with GTK 4 and WebKitGTK 6 and compiles them through cgo, which takes minutes.
.PHONY: desktop-ci
desktop-ci:
	$(MAKE) -C desktop ci

# Installation of the platform (US-55, ADR-22): bundled PostgreSQL and Keycloak, the one-time wizard and
# then the platform, all from install/compose.yaml. Guide: install/README.md. EASYCODE_BUNDLED=false on
# the first run uses external PostgreSQL and Keycloak.
INSTALL_COMPOSE := docker compose --env-file "$(CURDIR)/install/.env" -f "$(CURDIR)/install/compose.yaml" $(COMPOSE_EXTRA)
INSTALL_BUNDLED := $(if $(filter false,$(shell sed -n 's/^EASYCODE_BUNDLED=//p' "$(CURDIR)/install/.env" 2>/dev/null)),,--profile bundled)

.PHONY: install install-up install-down install-logs
install:
	COMPOSE_EXTRA="$(COMPOSE_EXTRA)" sh "$(CURDIR)/install/install.sh"

# Starts the installed platform again (after a reboot or an update of the images).
install-up:
	$(INSTALL_COMPOSE) $(INSTALL_BUNDLED) up -d --build

install-down:
	$(INSTALL_COMPOSE) $(INSTALL_BUNDLED) down

install-logs:
	$(INSTALL_COMPOSE) $(INSTALL_BUNDLED) logs -f --tail 100
