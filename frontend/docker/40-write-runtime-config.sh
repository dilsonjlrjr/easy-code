#!/bin/sh
# Writes /config.js from environment variables at container start, so one image serves any
# environment without a fixed address (RNF-17). Only non-secret values belong here. The sign-in
# is only the corporate SSO (ADR-12): there is no development identity to switch on.
set -eu
CONFIG_FILE=/usr/share/nginx/html/config.js
escaped_api_base_url=$(printf '%s' "${API_BASE_URL:-/api/v1}" | sed 's/\\/\\\\/g; s/"/\\"/g')
printf 'window.__EASY_CODE_CONFIG__ = { apiBaseUrl: "%s" }\n' \
  "$escaped_api_base_url" > "$CONFIG_FILE"
