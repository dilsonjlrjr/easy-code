# syntax=docker/dockerfile:1
# Builds one of the four binaries (ADR-02). Choose it with --build-arg BINARY=easycode-worker.
# Targets: the default (last) stage is distroless; "runtime-git" adds the git command line for the
# processes that keep the project repositories (worker writes, api serves the read-only clone, US-42).
ARG GO_IMAGE=golang:1.26.10-bookworm
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
ARG GIT_RUNTIME_IMAGE=debian:bookworm-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc

FROM ${GO_IMAGE} AS build
ARG BINARY=easycode-api
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/app ./cmd/${BINARY}
# Socket directories shared by easycode-gateway, easycode-sandbox and the worker, the platform key
# directories (ADR-13) and the repositories volume: a named volume mounted here starts with this
# ownership, so the non-root processes (uid 65532) write them; the files volume (attachments, US-03) too.
RUN mkdir -p /out/run/easycode-gateway /out/run/easycode-gateway-key /out/run/easycode-sealing /out/run/easycode-sandbox \
    /out/var/lib/easycode/repos /out/var/lib/easycode/files

FROM ${GIT_RUNTIME_IMAGE} AS runtime-git
RUN apt-get update && apt-get install -y --no-install-recommends git && rm -rf /var/lib/apt/lists/* \
 && groupadd -g 65532 nonroot && useradd -u 65532 -g 65532 -M -d /nonexistent -s /usr/sbin/nologin nonroot
COPY --from=build /out/app /app
COPY --from=build --chown=65532:65532 /out/run /run
COPY --from=build --chown=65532:65532 /out/var/lib/easycode /var/lib/easycode
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app"]

FROM ${RUNTIME_IMAGE}
COPY --from=build /out/app /app
COPY --from=build --chown=65532:65532 /out/run /run
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app"]
