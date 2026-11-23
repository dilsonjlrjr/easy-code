# Single CI entry point for the platform (docs/entrega/DoD.md). Any runner calls `make -C project ci`.
# Every step runs inside pinned containers, so the host needs only Docker and make (RNF-17).
# Paths may contain spaces: always keep them inside double quotes.

NODE_IMAGE       ?= node:24.22.3-bookworm-slim
PLAYWRIGHT_IMAGE ?= mcr.microsoft.com/playwright:v1.65.1-noble
PYTHON_IMAGE     ?= python:3.13.9-slim-bookworm
PY_DEPS          ?= jsonschema==4.10.3 attrs==26.1.0 pyrsistent==0.20.0 pyyaml==6.0.2

UID_GID   := $(shell id -u):$(shell id -g)
FRONTEND  := $(CURDIR)/frontend
REPO_ROOT := $(shell cd .. && pwd)

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

frontend-build: frontend-deps
	$(DOCKER_NODE) $(NODE_IMAGE) npm run build-only

# Playwright against the production build (playwright.config.ts starts `vite preview`).
frontend-e2e: frontend-deps
	docker run --rm --init --ipc=host -u $(UID_GID) -e HOME=/tmp -e CI=1 \
		-v "$(FRONTEND)":/work -w /work $(PLAYWRIGHT_IMAGE) npx playwright test

# Domain, requirements, architecture and data validators; the repository is mounted read-only.
# jsonschema stays on 4.10.x: the validators use RefResolver, which newer releases no longer resolve the same way.
specs:
	docker run --rm -e PIP_ROOT_USER_ACTION=ignore -e PIP_DISABLE_PIP_VERSION_CHECK=1 \
		-v "$(REPO_ROOT)":/repo:ro -w /repo $(PYTHON_IMAGE) \
		sh -c 'pip install -q --no-cache-dir $(PY_DEPS) && python3 specs/dominio/ferramentas/validar_tudo.py'

# easycode-desktop (ADR-20): Go and UI tests plus the Linux binary, in containers. Not part of `ci`: the first
# run builds an image with GTK 4 and WebKitGTK 6 and compiles them through cgo, which takes minutes.
.PHONY: desktop-ci
desktop-ci:
	$(MAKE) -C desktop ci
