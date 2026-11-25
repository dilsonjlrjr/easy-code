# syntax=docker/dockerfile:1
# Sandbox image of the security scans (ADR-05, ADR-07, US-23): Opengrep, Trivy and Gitleaks at fixed
# versions, each checked by its SHA-256, with everything they need offline baked in, because the sandbox
# never has network (RNF-02): the Trivy vulnerability database (permissoes.yaml "base pré-carregada na
# imagem") and an open-license Opengrep rule set at /opt/easycode/opengrep-rules. The worker asks for this
# image (logical id "security") only for the `scanner` category; easycode-sandbox runs it with the same
# hardened profile as every stack image (read-only root, tmpfs /tmp and /workspace, uid 10001, no network,
# resource limits of RNF-03). The image adds no service and no secret.
ARG BASE_IMAGE=debian:bookworm-20261104-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc

FROM ${BASE_IMAGE} AS fetch
ARG OPENGREP_VERSION=1.33.1
ARG OPENGREP_SHA256=46e8133cc490f9f50518061590ce2f6be5a08afaeda034471c0ceda4b3899808
ARG TRIVY_VERSION=0.76.2
ARG TRIVY_SHA256=bc0fc726e017b8927a62b2d3531d7a84ee01769daf5c99ed866dced54089fdbf
ARG GITLEAKS_VERSION=8.30.1
ARG GITLEAKS_SHA256=551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /fetch
RUN curl -fsSL -o opengrep "https://github.com/opengrep/opengrep/releases/download/v${OPENGREP_VERSION}/opengrep_manylinux_x86" \
 && echo "${OPENGREP_SHA256}  opengrep" | sha256sum -c - \
 && curl -fsSL -o trivy.tar.gz "https://github.com/aquasecurity/trivy/releases/download/v${TRIVY_VERSION}/trivy_${TRIVY_VERSION}_Linux-64bit.tar.gz" \
 && echo "${TRIVY_SHA256}  trivy.tar.gz" | sha256sum -c - \
 && curl -fsSL -o gitleaks.tar.gz "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz" \
 && echo "${GITLEAKS_SHA256}  gitleaks.tar.gz" | sha256sum -c - \
 && mkdir -p bin \
 && install -m 0755 opengrep bin/opengrep \
 && tar -xzf trivy.tar.gz -C bin trivy \
 && tar -xzf gitleaks.tar.gz -C bin gitleaks \
 && chmod 0755 bin/trivy bin/gitleaks

FROM ${BASE_IMAGE}
# git: `gitleaks git` reads the history when the workspace is a repository.
RUN apt-get update \
 && apt-get install -y --no-install-recommends git \
 && rm -rf /var/lib/apt/lists/* \
 && git config --system --add safe.directory '*'
COPY --from=fetch /fetch/bin/ /usr/local/bin/
# The vulnerability database (about 2 GB) is downloaded here, in the final stage, so it is stored once. It is
# the only thing that changes between builds of the same versions: its date shows in `trivy version` (the
# REL-SEG records it, varreduras-opensource.md).
COPY --from=fetch /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
# chmod in the same layer: a later chmod would copy the whole database into a new layer.
RUN trivy image --download-db-only --no-progress --cache-dir /opt/easycode/trivy \
 && chmod -R a+rX /opt/easycode
COPY --chmod=0755 rules /opt/easycode/opengrep-rules
# Offline by default, whatever flags the agent passes: no database or check update, no network lookup,
# scan cache in memory (the root file system is read-only), no telemetry or version check.
ENV TRIVY_CACHE_DIR=/opt/easycode/trivy \
    TRIVY_CACHE_BACKEND=memory \
    TRIVY_SKIP_DB_UPDATE=true \
    TRIVY_SKIP_JAVA_DB_UPDATE=true \
    TRIVY_SKIP_CHECK_UPDATE=true \
    TRIVY_SKIP_VERSION_CHECK=true \
    TRIVY_DISABLE_TELEMETRY=true \
    TRIVY_OFFLINE_SCAN=true \
    TRIVY_NO_PROGRESS=true \
    SEMGREP_SEND_METRICS=off \
    OPENGREP_RULES=/opt/easycode/opengrep-rules
# Smoke test at build time: the tools answer offline. Trivy scans are checked by the sandbox integration test
# under the read-only root (a scan here could copy the database into a new layer).
RUN mkdir -p /tmp/smoke && printf 'package main\n\nfunc main() {}\n' > /tmp/smoke/main.go \
 && opengrep --version \
 && opengrep scan --config /opt/easycode/opengrep-rules --error /tmp/smoke \
 && trivy version \
 && gitleaks version \
 && gitleaks dir --exit-code 1 /tmp/smoke \
 && rm -rf /tmp/smoke /root/.cache /root/.semgrep /root/.opengrep
USER 10001:10001
WORKDIR /workspace
