#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env}"

warn() {
  printf 'WARN: %s\n' "$*" >&2
}

fail_msg() {
  printf 'ERROR: %s\n' "$*" >&2
}

env_value() {
  local key=$1
  local line value

  line="$(grep -E "^[[:space:]]*${key}=" "$ENV_FILE" 2>/dev/null | tail -n 1 || true)"
  if [ -z "$line" ]; then
    return 0
  fi
  value="${line#*=}"
  value="${value%%#*}"
  value="${value%$'\r'}"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  value="${value%\"}"
  value="${value#\"}"
  value="${value%\'}"
  value="${value#\'}"
  printf '%s' "$value"
}

is_loopback_host() {
  local host=$1
  case "$host" in
    ""|"localhost"|"127.0.0.1"|"::1"|"[::1]")
      return 0
      ;;
    127.*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

is_all_interface_host() {
  local host=$1
  case "$host" in
    "0.0.0.0"|"::"|"[::]"|"*")
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

is_local_origin_value() {
  local value=$1
  case "$value" in
    ""|*localhost*|*127.0.0.1*|*"::1"*|*'${FRONTEND_PORT}'*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

has_nonlocal_origin_value() {
  local value=$1
  local item

  IFS=',' read -ra items <<<"$value"
  for item in "${items[@]}"; do
    item="${item#"${item%%[![:space:]]*}"}"
    item="${item%"${item##*[![:space:]]}"}"
    if [ -n "$item" ] && ! is_local_origin_value "$item"; then
      return 0
    fi
  done

  return 1
}

has_broad_cidr() {
  local value=$1
  value="${value//[[:space:]]/}"
  case ",$value," in
    *,0.0.0.0/0,*|*,::/0,*|*,0/0,*|*,\*,*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

if [ ! -f "$ENV_FILE" ]; then
  fail_msg "missing env file: $ENV_FILE"
  exit 1
fi

bind_host="$(env_value BIND_HOST)"
bind_host="${bind_host:-127.0.0.1}"
public_bind_override="$(env_value MULTICA_SELFHOST_ALLOW_PUBLIC_BIND)"
public_url="$(env_value MULTICA_PUBLIC_URL)"
frontend_origin="$(env_value FRONTEND_ORIGIN)"
cors_allowed_origins="$(env_value CORS_ALLOWED_ORIGINS)"

errors=0
exposure_requested=false

if is_all_interface_host "$bind_host"; then
  exposure_requested=true
  if [ "$public_bind_override" != "1" ]; then
    fail_msg "BIND_HOST=0.0.0.0 is refused because it publishes raw Docker ports on every interface. Use a reverse proxy/tunnel, bind a specific LAN IP, or set MULTICA_SELFHOST_ALLOW_PUBLIC_BIND=1 after a security review."
    errors=$((errors + 1))
  else
    warn "explicit public bind override enabled; raw Docker ports may be reachable outside this host."
  fi
elif ! is_loopback_host "$bind_host"; then
  exposure_requested=true
  warn "non-loopback bind requested; preflight will require self-host hardening settings before Docker starts."
fi

if [ -n "$public_url" ] && has_nonlocal_origin_value "$public_url"; then
  exposure_requested=true
fi
if [ -n "$frontend_origin" ] && has_nonlocal_origin_value "$frontend_origin"; then
  exposure_requested=true
fi
if [ -n "$cors_allowed_origins" ] && has_nonlocal_origin_value "$cors_allowed_origins"; then
  exposure_requested=true
fi

if [ "$exposure_requested" = true ]; then
  postgres_password="$(env_value POSTGRES_PASSWORD)"
  database_url="$(env_value DATABASE_URL)"
  jwt_secret="$(env_value JWT_SECRET)"
  app_env="$(env_value APP_ENV)"
  dev_code="$(env_value MULTICA_DEV_VERIFICATION_CODE)"
  resend_api_key="$(env_value RESEND_API_KEY)"
  smtp_host="$(env_value SMTP_HOST)"
  smtp_tls_insecure="$(env_value SMTP_TLS_INSECURE)"
  trusted_proxies="$(env_value MULTICA_TRUSTED_PROXIES)"
  rate_limit_trusted_proxies="$(env_value RATE_LIMIT_TRUSTED_PROXIES)"
  allow_signup="$(env_value ALLOW_SIGNUP)"
  disable_workspace_creation="$(env_value DISABLE_WORKSPACE_CREATION)"

  if [ -z "$postgres_password" ] || [ "$postgres_password" = "multica" ]; then
    fail_msg "POSTGRES_PASSWORD still uses the example value; rotate it before non-loopback or public access."
    errors=$((errors + 1))
  fi
  if [[ "$database_url" == *":multica@"* ]]; then
    fail_msg "DATABASE_URL still embeds the example Postgres password; keep it in sync with the rotated POSTGRES_PASSWORD."
    errors=$((errors + 1))
  fi
  if [ -z "$jwt_secret" ] || [ "$jwt_secret" = "change-me-in-production" ]; then
    fail_msg "JWT_SECRET still uses the example value; generate a high-entropy secret before exposure."
    errors=$((errors + 1))
  fi
  if [ "$app_env" != "production" ]; then
    fail_msg "APP_ENV should be production before non-loopback or public access."
    errors=$((errors + 1))
  fi
  if [ -n "$dev_code" ]; then
    fail_msg "MULTICA_DEV_VERIFICATION_CODE must be empty before non-loopback or public access."
    errors=$((errors + 1))
  fi
  if [ -z "$resend_api_key" ] && [ -z "$smtp_host" ]; then
    fail_msg "no email provider is configured; generated login codes would be printed to backend logs."
    errors=$((errors + 1))
  fi
  if [ "$smtp_tls_insecure" = "true" ]; then
    fail_msg "SMTP_TLS_INSECURE=true disables certificate verification; do not use it for public or shared-network access."
    errors=$((errors + 1))
  fi
  if has_broad_cidr "$trusted_proxies"; then
    fail_msg "MULTICA_TRUSTED_PROXIES contains a broad CIDR; use only exact reverse proxy/CDN CIDRs."
    errors=$((errors + 1))
  fi
  if has_broad_cidr "$rate_limit_trusted_proxies"; then
    fail_msg "RATE_LIMIT_TRUSTED_PROXIES contains a broad CIDR; use only exact reverse proxy/CDN CIDRs."
    errors=$((errors + 1))
  fi
  if ! has_nonlocal_origin_value "$frontend_origin" && ! has_nonlocal_origin_value "$cors_allowed_origins"; then
    fail_msg "FRONTEND_ORIGIN or CORS_ALLOWED_ORIGINS must be set to the exact LAN/public browser origin."
    errors=$((errors + 1))
  fi
  if [ "$allow_signup" = "true" ]; then
    fail_msg "ALLOW_SIGNUP=true leaves account creation open; set ALLOW_SIGNUP=false after bootstrapping users."
    errors=$((errors + 1))
  fi
  if [ "$disable_workspace_creation" != "true" ]; then
    fail_msg "DISABLE_WORKSPACE_CREATION is not true; set it after bootstrapping the shared workspace."
    errors=$((errors + 1))
  fi
fi

if [ "$errors" -gt 0 ]; then
  printf 'self-host preflight failed with %s issue(s). Values were not printed; inspect %s locally.\n' "$errors" "$ENV_FILE" >&2
  exit 1
fi

echo "self-host preflight ok"
