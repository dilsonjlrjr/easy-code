# syntax=docker/dockerfile:1
# Sandbox image of the Go stack (ADR-07): the Go toolchain with a warm build cache, so build and tests
# run in seconds without network. easycode-sandbox copies /opt/easycode/seed to the tmpfs /tmp of
# each container (GOCACHE=/tmp/gocache) because the root file system is read-only. The container runs
# as uid 10001 with no network; the image adds no service and no secret.
ARG GO_IMAGE=golang:1.26.10-bookworm@sha256:5de7c99eb78ca5e515a4c340235d0955ec5964a5071c492c15503d7c267da473
FROM ${GO_IMAGE}
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-mod=mod
COPY warm /tmp/warm
RUN cd /tmp/warm \
 && GOCACHE=/opt/easycode/seed/gocache go vet ./... \
 && GOCACHE=/opt/easycode/seed/gocache go build ./... \
 && GOCACHE=/opt/easycode/seed/gocache go test -count=1 ./... \
 && GOCACHE=/opt/easycode/seed/gocache go test -count=1 -json ./... >/dev/null \
 && rm -rf /tmp/warm /root/.cache \
 && chmod -R a+rX /opt/easycode/seed
USER 10001:10001
WORKDIR /workspace
