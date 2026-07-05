#!/usr/bin/env bash
# Login script for e2e-blacktest-curl
# Usage: ./login.sh <username> <password> [backend_url]
# Example: ./login.sh admin admin123 http://localhost:8080

set -euo pipefail

USERNAME="${1:-}"
PASSWORD="${2:-}"
BACKEND_URL="${3:-http://localhost:8080}"
COOKIE_FILE="${COOKIE_FILE:-/tmp/e2e-cookies.txt}"

if [[ -z "$USERNAME" || -z "$PASSWORD" ]]; then
  echo "Usage: $0 <username> <password> [backend_url]"
  echo "Example: $0 admin admin123 http://localhost:8080"
  exit 1
fi

echo "Logging in to $BACKEND_URL as $USERNAME..."

RESPONSE=$(curl -s -X POST "$BACKEND_URL/api/admin/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"$USERNAME\",\"password\":\"$PASSWORD\"}" \
  -c "$COOKIE_FILE" \
  -w "\n%{http_code}")

HTTP_CODE=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [[ "$HTTP_CODE" != "200" ]]; then
  echo "Login failed! HTTP code: $HTTP_CODE"
  echo "Response: $BODY"
  exit 1
fi

echo "Login successful!"
echo "Cookie saved to: $COOKIE_FILE"
echo "Response: $BODY"
