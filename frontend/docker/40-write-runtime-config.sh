#!/bin/sh
# Writes /config.js from environment variables at container start, so one image serves any
# environment without a fixed address (RNF-17). Only non-secret values belong here.
# DEV_SIGN_IN=true offers the fixed development identity on the login page; only the `marco1`
# Compose profile sets it (ADR-12, ESP-02 ER-02-9).
set -eu
CONFIG_FILE=/usr/share/nginx/html/config.js
escaped_api_base_url=$(printf '%s' "${API_BASE_URL:-/api/v1}" | sed 's/\\/\\\\/g; s/"/\\"/g')
case "${DEV_SIGN_IN:-false}" in
  true) dev_sign_in=true ;;
  *) dev_sign_in=false ;;
esac
printf 'window.__EASY_CODE_CONFIG__ = { apiBaseUrl: "%s", devSignIn: %s }\n' \
  "$escaped_api_base_url" "$dev_sign_in" > "$CONFIG_FILE"
