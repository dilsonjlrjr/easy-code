# syntax=docker/dockerfile:1
# easycode-installer (ADR-22): the Go binary and the wizard page (frontend, `vite build --mode installer`).
# Build context: the repository root (install/compose.yaml sets it).
ARG NODE_IMAGE=node:24.22.3-alpine
ARG GO_IMAGE=golang:1.26.10-bookworm
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

FROM ${NODE_IMAGE} AS web
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ .
RUN npm run build-installer

FROM ${GO_IMAGE} AS build
ARG VERSION=dev
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/ .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/app ./cmd/easycode-installer
# The configuration volume starts with this ownership, so the non-root processes (uid 65532) write
# and read it.
RUN mkdir -p /out/etc/easycode

FROM ${RUNTIME_IMAGE}
COPY --from=build /out/app /app
COPY --from=web /app/dist-installer /web
COPY --from=build --chown=65532:65532 /out/etc/easycode /etc/easycode
ENV EASYCODE_INSTALLER_WEB_DIR=/web \
    EASYCODE_INSTALLER_CONFIG_DIR=/etc/easycode
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app"]
