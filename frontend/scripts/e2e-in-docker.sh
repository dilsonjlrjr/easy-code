#!/bin/sh
# Runs the Playwright tests inside the official image, for hosts without browsers installed.
# Uses the project's node_modules (run `npm ci` first) and the host user, so reports stay
# owned by you. The image tag matches the pinned @playwright/test version.
set -eu
PLAYWRIGHT_IMAGE="mcr.microsoft.com/playwright:v1.65.1-noble"
cd "$(dirname "$0")/.."
exec docker run --rm --init --ipc=host \
  --user "$(id -u):$(id -g)" \
  -e CI=1 -e HOME=/tmp \
  -v "$PWD":/work -w /work \
  "$PLAYWRIGHT_IMAGE" \
  npx playwright test "$@"
