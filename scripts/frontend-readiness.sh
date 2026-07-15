#!/usr/bin/env bash

# CEREBRO-PATCH(check-frontend-readiness): gate E2E on the actual login UI so
# a cold, stale, or unrelated dev server cannot be mistaken for this checkout.
frontend_login_ready() {
  local frontend_origin=$1
  curl -sf --max-time 10 "${frontend_origin}/login" |
    grep -Fq 'Sign in to Multica'
}
