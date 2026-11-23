# Build and test image of easycode-desktop: Go plus the GTK 4 (>= 4.10, so Debian trixie) and WebKitGTK 6 headers that Wails 3 links on
# Linux (cgo). The host needs only Docker (RNF-17).
FROM golang:1.26.10-trixie
RUN apt-get update \
 && apt-get install -y --no-install-recommends pkg-config libgtk-4-dev libwebkitgtk-6.0-dev \
 && rm -rf /var/lib/apt/lists/*
