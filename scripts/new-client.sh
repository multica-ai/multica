#!/usr/bin/env bash
# scripts/new-client.sh — provision a new 3J Tracker client instance.
#
# Generates a ready-to-deploy .env snippet with strong random secrets,
# signup locked (ALLOW_SIGNUP=false), workspace creation disabled, and
# an initial ALLOWED_EMAILS list so only invited users can log in.
#
# Usage:
#   make new-client
#   # or directly:
#   bash scripts/new-client.sh
#
# Interactive prompts:
#   CLIENT_SLUG    short identifier, e.g. "acme" (used in container names)
#   ALLOWED_EMAILS comma-separated list of emails pre-approved at launch
#   RESEND_API_KEY Resend API key (or leave blank to configure later)
#   RESEND_FROM    from-address for outbound email
#
# The generated .env file is written to:
#   deploy/<CLIENT_SLUG>.env
# Copy it to your server and start:
#   docker compose -f docker-compose.selfhost.yml --env-file deploy/<CLIENT_SLUG>.env up -d

set -euo pipefail

command -v openssl >/dev/null 2>&1 || { echo "ERROR: openssl is required but not found."; exit 1; }

echo ""
echo "==> 3J Tracker — new client provisioning"
echo ""

# ---- Interactive input ----
read -rp "  Client slug (lowercase, no spaces, e.g. 'acme'): " CLIENT_SLUG
if [[ -z "$CLIENT_SLUG" || "$CLIENT_SLUG" =~ [^a-z0-9_-] ]]; then
    echo "ERROR: slug must be lowercase alphanumeric / dash / underscore only."
    exit 1
fi

read -rp "  Allowed emails (comma-separated, e.g. alice@acme.com,bob@acme.com): " ALLOWED_EMAILS
if [[ -z "$ALLOWED_EMAILS" ]]; then
    echo "WARNING: no allowed emails provided — no one will be able to log in until ALLOWED_EMAILS is set."
fi

read -rp "  Resend API key (leave blank to set later): " RESEND_API_KEY

RESEND_FROM_DEFAULT="noreply@tracker.3jtech.app"
read -rp "  From email [${RESEND_FROM_DEFAULT}]: " RESEND_FROM
RESEND_FROM="${RESEND_FROM:-$RESEND_FROM_DEFAULT}"

read -rp "  Public frontend URL (e.g. https://tracker.acme.com): " FRONTEND_URL
if [[ -z "$FRONTEND_URL" ]]; then
    FRONTEND_URL="http://localhost:3000"
    echo "  (defaulting to $FRONTEND_URL — update before going live)"
fi

# ---- Generate secrets ----
JWT_SECRET=$(openssl rand -hex 32)
POSTGRES_PASSWORD=$(openssl rand -hex 24)

# ---- Write env file ----
OUTDIR="deploy"
mkdir -p "$OUTDIR"
OUTFILE="$OUTDIR/${CLIENT_SLUG}.env"

cat > "$OUTFILE" <<EOF
# 3J Tracker — client: ${CLIENT_SLUG}
# Generated: $(date -u +%Y-%m-%dT%H:%M:%SZ)
# DO NOT commit this file — it contains secrets.

# --- Secrets (generated, do not share) ---
JWT_SECRET=${JWT_SECRET}
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}

# --- Email ---
RESEND_API_KEY=${RESEND_API_KEY}
RESEND_FROM_EMAIL=${RESEND_FROM}

# --- App ---
APP_ENV=production
FRONTEND_ORIGIN=${FRONTEND_URL}
CORS_ALLOWED_ORIGINS=${FRONTEND_URL}
COOKIE_DOMAIN=$(echo "$FRONTEND_URL" | sed 's|https\?://||' | cut -d/ -f1)
MULTICA_PUBLIC_URL=${FRONTEND_URL}

# --- Signup / access control ---
# Signups locked: only ALLOWED_EMAILS can log in.
ALLOW_SIGNUP=false
DISABLE_WORKSPACE_CREATION=true
ALLOWED_EMAILS=${ALLOWED_EMAILS}
ALLOWED_EMAIL_DOMAINS=

# --- Optional ---
REDIS_URL=
OPENAI_API_KEY=
GITHUB_APP_SLUG=
GITHUB_WEBHOOK_SECRET=
EOF

chmod 600 "$OUTFILE"

echo ""
echo "==> Done!"
echo ""
echo "  Env file written to: ${OUTFILE}"
echo ""
echo "  Next steps:"
echo "    1. Copy ${OUTFILE} to your server as .env"
echo "    2. Optionally set RESEND_API_KEY if left blank above"
echo "    3. Start:  docker compose -f docker-compose.selfhost.yml --env-file .env up -d"
echo "    4. Create the first workspace by logging in as one of the ALLOWED_EMAILS"
echo "    5. Invite additional users from the Settings → Members page"
echo ""
echo "  See docs/client-provisioning.md for full setup guide."
echo ""
