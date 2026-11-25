#!/bin/sh
# Writes /config.js from environment variables at container start, so one image serves any
# environment without a fixed address (RNF-17). Only non-secret values belong here. The sign-in
# is only the corporate SSO (ADR-12): there is no development identity to switch on.
#
# It also writes the security headers of the SPA (ADR-19, ESP-11 ER-11-2) from
# /etc/nginx/csp-policy.txt: frame-src only the content origin (EASYCODE_CONTENT_ORIGIN, none when
# unset), connect-src the same origin plus the api origin when API_BASE_URL is absolute (the template
# also lists the public price lists the price table reads: models.dev, LiteLLM, OpenRouter), and a
# nonce per response ($request_id, substituted into index.html by nginx).
set -eu
CONFIG_FILE=/usr/share/nginx/html/config.js
HEADERS_FILE=/etc/nginx/conf.d/security-headers.inc
POLICY_TEMPLATE=/etc/nginx/csp-policy.txt

api_base_url="${API_BASE_URL:-/api/v1}"
escaped_api_base_url=$(printf '%s' "$api_base_url" | sed 's/\\/\\\\/g; s/"/\\"/g')
printf 'window.__EASY_CODE_CONFIG__ = { apiBaseUrl: "%s" }\n' \
  "$escaped_api_base_url" > "$CONFIG_FILE"

# An origin is scheme://host[:port] and nothing else, so a value cannot add a directive.
origin_pattern='^https?://[A-Za-z0-9.-]+(:[0-9]{1,5})?$'

content_origin="${EASYCODE_CONTENT_ORIGIN:-}"
content_origin="${content_origin%/}"
frame_src="'none'"
if [ -n "$content_origin" ]; then
  if ! printf '%s' "$content_origin" | grep -Eq "$origin_pattern"; then
    echo "EASYCODE_CONTENT_ORIGIN must be scheme://host[:port], got: $content_origin" >&2
    exit 1
  fi
  frame_src="$content_origin"
fi

connect_src="'self'"
case "$api_base_url" in
  http://* | https://*)
    api_origin=$(printf '%s' "$api_base_url" | sed -E 's#^(https?://[^/]+).*#\1#')
    if ! printf '%s' "$api_origin" | grep -Eq "$origin_pattern"; then
      echo "API_BASE_URL has an invalid origin: $api_base_url" >&2
      exit 1
    fi
    connect_src="'self' $api_origin"
    ;;
esac

policy=$(sed -e 's/{nonce}/$request_id/g' -e "s#{frame_src}#$frame_src#g" \
  -e "s#{connect_src}#$connect_src#g" "$POLICY_TEMPLATE")

cat > "$HEADERS_FILE" <<EOF
add_header Content-Security-Policy "$policy" always;
add_header X-Frame-Options "DENY" always;
add_header X-Content-Type-Options "nosniff" always;
add_header Referrer-Policy "no-referrer" always;
EOF
