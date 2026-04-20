#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env}"
APP_ID="${2:-${FEISHU_APP_ID:-}}"
APP_SECRET="${3:-${FEISHU_APP_SECRET:-}}"
REDIRECT_URI="${4:-${FEISHU_REDIRECT_URI:-}}"
PUBLIC_APP_ID="${5:-${NEXT_PUBLIC_FEISHU_APP_ID:-$APP_ID}}"

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE"
  echo "Create .env from .env.example first."
  exit 1
fi

if [ -z "$APP_ID" ] || [ -z "$APP_SECRET" ] || [ -z "$REDIRECT_URI" ] || [ -z "$PUBLIC_APP_ID" ]; then
  echo "Usage: $0 <env-file> <app-id> <app-secret> <redirect-uri> [public-app-id]"
  echo "You can also provide values through FEISHU_APP_ID / FEISHU_APP_SECRET / FEISHU_REDIRECT_URI / NEXT_PUBLIC_FEISHU_APP_ID."
  exit 1
fi

update_key() {
  local key="$1"
  local value="$2"
  local tmp_file
  tmp_file="$(mktemp)"

  if grep -q "^${key}=" "$ENV_FILE"; then
    awk -v key="$key" -v value="$value" 'BEGIN { updated = 0 } {
      if ($0 ~ "^" key "=") {
        print key "=" value
        updated = 1
      } else {
        print $0
      }
    } END {
      if (!updated) {
        print key "=" value
      }
    }' "$ENV_FILE" > "$tmp_file"
  else
    cat "$ENV_FILE" > "$tmp_file"
    printf '\n%s=%s\n' "$key" "$value" >> "$tmp_file"
  fi

  mv "$tmp_file" "$ENV_FILE"
}

update_key "FEISHU_APP_ID" "$APP_ID"
update_key "FEISHU_APP_SECRET" "$APP_SECRET"
update_key "FEISHU_REDIRECT_URI" "$REDIRECT_URI"
update_key "NEXT_PUBLIC_FEISHU_APP_ID" "$PUBLIC_APP_ID"

echo "Updated Feishu configuration in $ENV_FILE"
echo "Restart backend and frontend to apply the new Feishu settings."
echo "If NEXT_PUBLIC_FEISHU_APP_ID changed and the button still does not appear, rebuild or redeploy the frontend bundle before restarting the frontend again."
