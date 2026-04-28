#!/usr/bin/env bash
# Deploy Multica to a single Debian VM with Docker Compose, Nginx, and Let's Encrypt.
#
# Usage:
#   sudo bash scripts/deploy-vm.sh <domain>
#   sudo bash scripts/deploy-vm.sh <domain> <resend_api_key> [resend_from_email]
#
set -euo pipefail

REPO_URL="${MULTICA_REPO_URL:-https://github.com/multica-ai/multica.git}"
APP_DIR="${MULTICA_APP_DIR:-/opt/multica}"
COMPOSE_FILE="docker-compose.selfhost.yml"

if [ -t 1 ] || [ -t 2 ]; then
  BOLD='\033[1m'
  GREEN='\033[0;32m'
  YELLOW='\033[0;33m'
  RED='\033[0;31m'
  CYAN='\033[0;36m'
  RESET='\033[0m'
else
  BOLD='' GREEN='' YELLOW='' RED='' CYAN='' RESET=''
fi

info() { printf "${BOLD}${CYAN}==> %s${RESET}\n" "$*"; }
ok() { printf "${BOLD}${GREEN}✓ %s${RESET}\n" "$*"; }
warn() { printf "${BOLD}${YELLOW}⚠ %s${RESET}\n" "$*" >&2; }
fail() { printf "${BOLD}${RED}✗ %s${RESET}\n" "$*" >&2; exit 1; }

usage() {
  cat <<EOF
Usage:
  sudo bash scripts/deploy-vm.sh <domain>
  sudo bash scripts/deploy-vm.sh <domain> <resend_api_key> [resend_from_email]

Example:
  sudo bash scripts/deploy-vm.sh multica.example.com
  sudo bash scripts/deploy-vm.sh multica.example.com re_abc123 onboarding@resend.dev
EOF
}

require_args() {
  if [ "$#" -lt 1 ] || [ "$#" -gt 3 ]; then
    usage
    exit 1
  fi

  DOMAIN="$1"
  if [ "$#" -ge 2 ]; then
    RESEND_API_KEY="$2"
    RESEND_FROM_EMAIL="${3:-${RESEND_FROM_EMAIL:-onboarding@resend.dev}}"
  else
    RESEND_API_KEY="${RESEND_API_KEY:-}"
    RESEND_FROM_EMAIL="${RESEND_FROM_EMAIL:-onboarding@resend.dev}"
  fi

  case "$DOMAIN" in
    http://*|https://*|*/*|*:*|'')
      fail "Pass only the hostname as <domain>, for example: multica.example.com"
      ;;
  esac

  case "$RESEND_FROM_EMAIL" in
    *@*) ;;
    *) fail "RESEND_FROM_EMAIL must look like an email address." ;;
  esac

  if [ -z "$RESEND_API_KEY" ]; then
    if [ -t 0 ]; then
      printf 'Resend API key: '
      IFS= read -r RESEND_API_KEY
    fi
  fi

  if [ -z "$RESEND_API_KEY" ]; then
    fail "RESEND_API_KEY is required. Pass it as the second argument, set it in the environment, or enter it at the prompt."
  fi
}

require_debian() {
  if [ ! -r /etc/os-release ]; then
    fail "This script supports Debian Linux on a VM."
  fi

  # shellcheck disable=SC1091
  . /etc/os-release
  if [ "${ID:-}" != "debian" ]; then
    fail "This script supports Debian Linux. Detected: ${PRETTY_NAME:-unknown OS}"
  fi
}

sudo_cmd() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  else
    sudo "$@"
  fi
}

docker_cmd() {
  if [ "$(id -u)" -eq 0 ]; then
    docker "$@"
  else
    sudo docker "$@"
  fi
}

deployment_user() {
  if [ "$(id -u)" -eq 0 ] && [ -n "${SUDO_USER:-}" ]; then
    printf '%s' "$SUDO_USER"
  else
    id -un
  fi
}

deployment_group() {
  local user
  user="$(deployment_user)"
  id -gn "$user"
}

env_value() {
  local key file
  key="$1"
  file="$2"

  if [ -f "$file" ]; then
    grep -E "^${key}=" "$file" | tail -n 1 | cut -d= -f2- || true
  fi
}

install_dependencies() {
  info "Installing system dependencies..."
  sudo_cmd apt-get update
  sudo_cmd env DEBIAN_FRONTEND=noninteractive apt-get upgrade -y
  sudo_cmd env DEBIAN_FRONTEND=noninteractive apt-get install -y \
    ca-certificates \
    certbot \
    curl \
    dnsutils \
    git \
    nginx \
    openssl \
    python3-certbot-nginx

  if ! command -v docker >/dev/null 2>&1; then
    info "Installing Docker..."
    curl -fsSL https://get.docker.com | sudo_cmd sh
  else
    ok "Docker is already installed"
  fi

  local user
  user="$(deployment_user)"
  if [ "$user" != "root" ]; then
    sudo_cmd usermod -aG docker "$user" || warn "Could not add $user to the docker group."
  fi

  sudo_cmd systemctl enable --now docker
  sudo_cmd systemctl enable --now nginx
  ok "Dependencies installed"
}

verify_dns() {
  info "Verifying DNS for $DOMAIN..."

  local vm_ip dns_ips
  vm_ip="$(curl -4fsSL https://api.ipify.org || true)"
  if [ -z "$vm_ip" ]; then
    fail "Could not determine this VM's public IPv4 address."
  fi

  dns_ips="$(dig +short A "$DOMAIN" | sed '/^$/d' || true)"
  if [ -z "$dns_ips" ]; then
    fail "No A record found for $DOMAIN. Point the domain to $vm_ip and wait for DNS propagation."
  fi

  if ! printf '%s\n' "$dns_ips" | grep -Fxq "$vm_ip"; then
    cat >&2 <<EOF
DNS is not ready for $DOMAIN.

VM public IP:
  $vm_ip

DNS A record currently resolves to:
$dns_ips

Update the A record to $vm_ip, disable Cloudflare proxying for this record, wait for propagation, then rerun this script.
EOF
    exit 1
  fi

  ok "$DOMAIN resolves to this VM ($vm_ip)"
}

clone_or_update_repo() {
  info "Preparing Multica repository in $APP_DIR..."

  local user group
  user="$(deployment_user)"
  group="$(deployment_group)"

  sudo_cmd mkdir -p "$APP_DIR"
  sudo_cmd chown "$user:$group" "$APP_DIR"

  if [ -d "$APP_DIR/.git" ]; then
    git -C "$APP_DIR" fetch --prune origin
    git -C "$APP_DIR" pull --ff-only
  elif [ -z "$(find "$APP_DIR" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
    git clone "$REPO_URL" "$APP_DIR"
  else
    fail "$APP_DIR is not empty and is not a git repository. Move it aside or set MULTICA_APP_DIR."
  fi

  ok "Repository ready"
}

write_env() {
  info "Writing production .env..."

  local app_url jwt_secret postgres_password env_file
  app_url="https://$DOMAIN"
  env_file="$APP_DIR/.env"

  jwt_secret="$(env_value JWT_SECRET "$env_file")"
  postgres_password="$(env_value POSTGRES_PASSWORD "$env_file")"

  if [ -z "$jwt_secret" ]; then
    jwt_secret="$(openssl rand -hex 32)"
  fi

  if [ -z "$postgres_password" ]; then
    postgres_password="$(openssl rand -hex 16)"
  fi

  if [ -f "$env_file" ]; then
    cp "$env_file" "$env_file.backup.$(date +%Y%m%d%H%M%S)"
    warn "Existing .env backed up. Preserving existing JWT_SECRET and POSTGRES_PASSWORD."
  fi

  cat > "$env_file" <<EOF
# Generated by scripts/deploy-vm.sh on $(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Database
POSTGRES_DB=multica
POSTGRES_USER=multica
POSTGRES_PASSWORD=$postgres_password
POSTGRES_PORT=5432
DATABASE_URL=postgres://multica:$postgres_password@postgres:5432/multica?sslmode=disable

# Server
APP_ENV=production
PORT=8080
JWT_SECRET=$jwt_secret
MULTICA_SERVER_URL=wss://$DOMAIN/ws
MULTICA_APP_URL=$app_url
FRONTEND_ORIGIN=$app_url
CORS_ALLOWED_ORIGINS=$app_url
ALLOWED_ORIGINS=$app_url

# Email (Resend)
RESEND_API_KEY=$RESEND_API_KEY
RESEND_FROM_EMAIL=$RESEND_FROM_EMAIL

# Google OAuth disabled by default
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
GOOGLE_REDIRECT_URI=$app_url/auth/callback
NEXT_PUBLIC_GOOGLE_CLIENT_ID=

# S3 / CloudFront disabled by default; local Docker volume stores uploads.
S3_BUCKET=
S3_REGION=us-west-2
CLOUDFRONT_KEY_PAIR_ID=
CLOUDFRONT_PRIVATE_KEY_SECRET=multica/cloudfront-signing-key
CLOUDFRONT_PRIVATE_KEY=
CLOUDFRONT_DOMAIN=
COOKIE_DOMAIN=
LOCAL_UPLOAD_DIR=/app/data/uploads
LOCAL_UPLOAD_BASE_URL=$app_url

# Frontend
FRONTEND_PORT=3000
NEXT_PUBLIC_API_URL=
NEXT_PUBLIC_WS_URL=
EOF

  chmod 600 "$env_file"
  sudo_cmd chown "$(deployment_user):$(deployment_group)" "$env_file"
  ok ".env generated"
}

start_compose() {
  info "Building and starting Docker Compose services..."
  cd "$APP_DIR"
  docker_cmd compose -f "$COMPOSE_FILE" up -d --build
  ok "Docker Compose services started"
}

wait_for_backend() {
  info "Waiting for backend health check..."

  local attempt
  for attempt in $(seq 1 80); do
    if curl -fsS http://localhost:8080/health >/dev/null 2>&1; then
      ok "Backend is healthy"
      return 0
    fi
    sleep 3
  done

  docker_cmd compose -f "$APP_DIR/$COMPOSE_FILE" ps || true
  fail "Backend did not become healthy. Check logs with: cd $APP_DIR && sudo docker compose -f $COMPOSE_FILE logs -f backend"
}

write_initial_nginx_config() {
  info "Writing temporary Nginx HTTP config for Certbot..."

  sudo_cmd tee "/etc/nginx/sites-available/multica" >/dev/null <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name $DOMAIN;

    location /.well-known/acme-challenge/ {
        root /var/www/html;
    }

    location / {
        return 200 'Multica certificate setup';
        add_header Content-Type text/plain;
    }
}
EOF

  sudo_cmd ln -sfn /etc/nginx/sites-available/multica /etc/nginx/sites-enabled/multica
  sudo_cmd rm -f /etc/nginx/sites-enabled/default
  sudo_cmd nginx -t
  sudo_cmd systemctl reload nginx
  ok "Temporary Nginx config loaded"
}

obtain_certificate() {
  info "Requesting Let's Encrypt certificate..."
  sudo_cmd certbot certonly \
    --nginx \
    --domain "$DOMAIN" \
    --non-interactive \
    --agree-tos \
    --email "$RESEND_FROM_EMAIL"
  ok "TLS certificate obtained"
}

write_production_nginx_config() {
  info "Writing production Nginx config..."

  sudo_cmd tee "/etc/nginx/sites-available/multica" >/dev/null <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name $DOMAIN;

    location /.well-known/acme-challenge/ {
        root /var/www/html;
    }

    location / {
        return 301 https://\$host\$request_uri;
    }
}

server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name $DOMAIN;

    ssl_certificate /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;

    client_max_body_size 50M;

    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;

    location /ws {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:8080;
    }

    location /auth/ {
        proxy_pass http://127.0.0.1:8080;
    }

    location /uploads/ {
        proxy_pass http://127.0.0.1:8080;
    }

    location = /health {
        proxy_pass http://127.0.0.1:8080;
    }

    location / {
        proxy_pass http://127.0.0.1:3000;
    }
}
EOF

  sudo_cmd nginx -t
  sudo_cmd systemctl reload nginx
  ok "Production Nginx config loaded"
}

print_summary() {
  cat <<EOF

${BOLD}${GREEN}Multica is live.${RESET}

App URL:
  https://$DOMAIN

Login:
  Enter your email address and use the code sent by Resend.

Verify:
  curl https://$DOMAIN/health
  curl -I https://$DOMAIN/api/config

Useful commands:
  cd $APP_DIR && sudo docker compose -f $COMPOSE_FILE logs -f
  cd $APP_DIR && sudo docker compose -f $COMPOSE_FILE restart backend
  cd $APP_DIR && git pull && sudo docker compose -f $COMPOSE_FILE up -d --build
  cd $APP_DIR && sudo docker compose -f $COMPOSE_FILE exec postgres pg_dump -U multica multica > backup.sql

Troubleshooting:
  cd $APP_DIR && sudo docker compose -f $COMPOSE_FILE ps
  sudo nginx -t
  sudo journalctl -u nginx --no-pager -n 100
EOF
}

main() {
  require_args "$@"
  require_debian
  install_dependencies
  verify_dns
  clone_or_update_repo
  write_env
  start_compose
  wait_for_backend
  write_initial_nginx_config
  obtain_certificate
  write_production_nginx_config
  print_summary
}

main "$@"
