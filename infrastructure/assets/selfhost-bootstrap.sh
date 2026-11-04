#!/bin/bash
# Bootstrap for the self-hosted Multica server (Amazon Linux 2023, arm64).
#
# Rendered by lib/bootstrap.ts, which substitutes the marked placeholders below
# at synth time. cloud-init runs it as root on first boot; output lands in
# /var/log/cloud-init-output.log and is shipped to CloudWatch Logs.
#
# It brings up the repository's own docker-compose.selfhost.yml — PostgreSQL,
# the Go backend and the Next.js web app — behind Caddy, which terminates TLS
# on the public interface and forwards to the loopback ports Compose publishes.
# The compose file is deployed as a CDK asset, so the stack runs exactly the
# file that is in this repository rather than whatever is on a branch today.
#
# PostgreSQL's data directory is on a separate EBS volume mounted at
# /var/lib/multica. That volume is retained when the stack is deleted and is
# snapshotted daily, so the database outlives this instance.
set -euo pipefail

REGION="@@REGION@@"
PARAM_PREFIX="@@PARAM_PREFIX@@"
JWT_SECRET_ID="@@JWT_SECRET_ID@@"
DB_PASSWORD_ID="@@DB_PASSWORD_ID@@"
COMPOSE_URL="@@COMPOSE_URL@@"
DOMAIN_NAME="@@DOMAIN_NAME@@"
PUBLIC_IP="@@PUBLIC_IP@@"
IMAGE_TAG="@@IMAGE_TAG@@"
ALLOWED_EMAILS="@@ALLOWED_EMAILS@@"
DO_NOT_TRACK_VALUE="@@DO_NOT_TRACK@@"
LOG_GROUP="@@LOG_GROUP@@"
CADDY_VERSION="@@CADDY_VERSION@@"
COMPOSE_VERSION="@@COMPOSE_VERSION@@"
DATA_DEVICE_NAME="@@DATA_DEVICE_NAME@@"

APP_DIR="/opt/multica"
DATA_DIR="/var/lib/multica"

log() { echo "[multica-selfhost] $*"; }
trap 'log "FAILED on line ${LINENO}"' ERR

# With no Elastic IP the stack cannot pass the address in — an instance's
# public IP is an attribute of the instance, and its own user data cannot
# reference it without a circular dependency. Metadata knows it at boot.
if [ -z "${PUBLIC_IP}" ]; then
  imds_token="$(curl -fsS -X PUT http://169.254.169.254/latest/api/token \
    -H 'X-aws-ec2-metadata-token-ttl-seconds: 300')"
  PUBLIC_IP="$(curl -fsS -H "X-aws-ec2-metadata-token: ${imds_token}" \
    http://169.254.169.254/latest/meta-data/public-ipv4)"
  log "Auto-assigned public IP is ${PUBLIC_IP}"
fi

# sslip.io resolves <ip>.sslip.io to <ip>, so a deployment with no domain of
# its own still gets a real hostname and therefore a real certificate.
if [ -n "${DOMAIN_NAME}" ]; then
  PUBLIC_HOST="${DOMAIN_NAME}"
else
  PUBLIC_HOST="${PUBLIC_IP}.sslip.io"
fi
PUBLIC_URL="https://${PUBLIC_HOST}"
log "Public URL will be ${PUBLIC_URL}"

log "Installing base packages"
dnf -y install docker jq amazon-cloudwatch-agent

log "Installing the Docker Compose plugin ${COMPOSE_VERSION}"
install -d -m 0755 /usr/libexec/docker/cli-plugins
curl -fsSL \
  "https://github.com/docker/compose/releases/download/${COMPOSE_VERSION}/docker-compose-linux-aarch64" \
  -o /usr/libexec/docker/cli-plugins/docker-compose
chmod 0755 /usr/libexec/docker/cli-plugins/docker-compose

systemctl enable --now docker

# ---------------------------------------------------------------------------
# Database volume
#
# The attachment is a separate EBS volume so that replacing this instance does
# not take the database with it. NVMe renames devices, so find it by the
# attachment name EC2 reports rather than by a /dev path.
# ---------------------------------------------------------------------------
log "Preparing the data volume"
# On Nitro the volume is an NVMe device; AL2023's udev rules also create the
# requested name as a symlink to it, and `test -b` follows symlinks. The wait
# is because the attachment can land after cloud-init has already started.
data_device=""
for _ in $(seq 1 30); do
  if [ -b "${DATA_DEVICE_NAME}" ]; then
    data_device="$(readlink -f "${DATA_DEVICE_NAME}")"
    break
  fi
  sleep 2
done
if [ -z "${data_device}" ]; then
  log "data volume ${DATA_DEVICE_NAME} never appeared"
  exit 1
fi
log "Data volume is ${data_device}"

# blkid exits non-zero on an unformatted device, which is exactly how a brand
# new volume looks. Only an empty device is formatted; a volume carrying an
# existing database is mounted as-is.
if ! blkid "${data_device}" >/dev/null 2>&1; then
  log "Formatting ${data_device} (first boot for this volume)"
  mkfs.ext4 -L multica-data "${data_device}"
fi

install -d -m 0755 "${DATA_DIR}"
data_uuid="$(blkid -s UUID -o value "${data_device}")"
if ! grep -q "${data_uuid}" /etc/fstab; then
  printf 'UUID=%s %s ext4 defaults,nofail 0 2\n' "${data_uuid}" "${DATA_DIR}" >>/etc/fstab
fi
mount -a
install -d -m 0700 "${DATA_DIR}/postgres"
install -d -m 0755 "${DATA_DIR}/uploads"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
# Two stores, one rule: what CloudFormation generates lives in Secrets Manager,
# what a human supplies lives in Parameter Store. Nobody ever sees or types the
# JWT secret or the database password, so the stack generates them and a first
# deploy needs nothing created by hand.
log "Reading the generated secrets"
read_secret() {
  aws secretsmanager get-secret-value --region "${REGION}" --secret-id "$1" \
    --query SecretString --output text
}

jwt_secret="$(read_secret "${JWT_SECRET_ID}")"
postgres_password="$(read_secret "${DB_PASSWORD_ID}")"

# Email is genuinely optional: without it the backend prints each login code to
# its own log, which scripts/selfhost-apply.sh --code reads. get-parameters (not
# get-parameter) reports a missing name instead of failing the call.
read_param() {
  value="$(aws ssm get-parameters --region "${REGION}" \
    --names "${PARAM_PREFIX}/$1" --with-decryption \
    --query 'Parameters[0].Value' --output text)"
  if [ "${value}" = "None" ]; then
    value=""
  fi
  printf '%s' "${value}"
}

resend_api_key="$(read_param resend-api-key)"
resend_from_email="$(read_param resend-from-email)"

# Resend refuses any From address on a domain the account has not verified,
# and the backend's own default is noreply@multica.ai — a domain a self-hoster
# does not own, so the key alone would send nothing and leave no code in the
# log either. onboarding@resend.dev needs no domain setup, but Resend delivers
# it only to the address the account was opened with: enough to sign in as the
# owner, not enough to invite anyone.
if [ -n "${resend_api_key}" ]; then
  if [ -z "${resend_from_email}" ]; then
    resend_from_email="onboarding@resend.dev"
    log "No resend-from-email parameter; sending as ${resend_from_email}, which"
    log "Resend delivers only to the address its account was opened with."
    log "Verify a domain and set ${PARAM_PREFIX}/resend-from-email to invite others."
  else
    log "Sending login codes as ${resend_from_email}"
  fi
else
  log "No resend-api-key parameter; login codes will go to the backend log"
fi

install -d -m 0750 "${APP_DIR}"
log "Downloading the compose file"
aws s3 cp "${COMPOSE_URL}" "${APP_DIR}/docker-compose.yml" --region "${REGION}"

# The compose file publishes to 127.0.0.1 only and takes every setting from the
# environment. Written 0600: it holds the JWT secret and the database password.
log "Writing ${APP_DIR}/.env"
umask 077
cat >"${APP_DIR}/.env" <<ENVFILE
MULTICA_IMAGE_TAG=${IMAGE_TAG}
POSTGRES_DB=multica
POSTGRES_USER=multica
POSTGRES_PASSWORD=${postgres_password}
JWT_SECRET=${jwt_secret}

# Single-domain layout: Caddy serves the web app at / and routes /ws to the
# backend, so the browser talks to one origin and the web container reaches the
# backend over the compose network.
FRONTEND_ORIGIN=${PUBLIC_URL}
CORS_ALLOWED_ORIGINS=${PUBLIC_URL}
MULTICA_APP_URL=${PUBLIC_URL}
MULTICA_PUBLIC_URL=${PUBLIC_URL}
MULTICA_DAEMON_SERVER_URL=${PUBLIC_URL}
REMOTE_API_URL=http://backend:8080
NEXT_PUBLIC_API_URL=
NEXT_PUBLIC_WS_URL=

# Caddy runs on the host and reaches the containers through the published
# loopback ports, so the backend sees the Docker bridge gateway as the peer.
MULTICA_TRUSTED_PROXIES=127.0.0.1/32,172.16.0.0/12

APP_ENV=production
ALLOW_SIGNUP=true
ALLOWED_EMAILS=${ALLOWED_EMAILS}
DO_NOT_TRACK=${DO_NOT_TRACK_VALUE}
RESEND_API_KEY=${resend_api_key}
RESEND_FROM_EMAIL=${resend_from_email}
ENVFILE
chmod 0600 "${APP_DIR}/.env"
umask 022

# PostgreSQL and uploads must land on the data volume rather than in a Docker
# named volume on the root disk, which a replaced instance would not carry.
log "Pinning the data volumes"
cat >"${APP_DIR}/docker-compose.override.yml" <<OVERRIDE
services:
  postgres:
    volumes:
      - ${DATA_DIR}/postgres:/var/lib/postgresql/data
  backend:
    volumes:
      - ${DATA_DIR}/uploads:/app/data/uploads
OVERRIDE

# ---------------------------------------------------------------------------
# Reverse proxy
#
# Copied from the single-domain example in SELF_HOSTING_ADVANCED.md. The /ws
# matcher lists /ws and /ws/* rather than /ws*, which would also swallow
# workspace URLs like /ws-foo, and flush_interval -1 stops Caddy buffering
# WebSocket frames.
# ---------------------------------------------------------------------------
log "Installing Caddy ${CADDY_VERSION}"
curl -fsSL \
  "https://github.com/caddyserver/caddy/releases/download/v${CADDY_VERSION}/caddy_${CADDY_VERSION}_linux_arm64.tar.gz" \
  -o /tmp/caddy.tar.gz
tar -xzf /tmp/caddy.tar.gz -C /tmp caddy
install -m 0755 /tmp/caddy /usr/local/bin/caddy
rm -f /tmp/caddy /tmp/caddy.tar.gz

id -u caddy >/dev/null 2>&1 ||
  useradd --system --home-dir /var/lib/caddy --create-home --shell /usr/sbin/nologin caddy
install -d -m 0755 /etc/caddy

cat >"/etc/caddy/Caddyfile" <<CADDYFILE
${PUBLIC_HOST} {
    @multica_ws path /ws /ws/*
    handle @multica_ws {
        reverse_proxy localhost:8080 {
            flush_interval -1
        }
    }

    reverse_proxy localhost:3000
}
CADDYFILE

cat >/etc/systemd/system/caddy.service <<'UNIT'
[Unit]
Description=Caddy
Documentation=https://caddyserver.com/docs/
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
User=caddy
Group=caddy
ExecStart=/usr/local/bin/caddy run --environ --config /etc/caddy/Caddyfile
ExecReload=/usr/local/bin/caddy reload --config /etc/caddy/Caddyfile --force
TimeoutStopSec=5s
LimitNOFILE=1048576
PrivateTmp=true
ProtectSystem=full
# Binding 80 and 443 as a non-root user.
AmbientCapabilities=CAP_NET_BIND_SERVICE
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

# ---------------------------------------------------------------------------
# Start
# ---------------------------------------------------------------------------
# Container stdout goes to CloudWatch alongside the bootstrap log. Without a
# mail provider the backend prints the sign-in code to stdout and nowhere
# else, so this is the only way to read it without a shell on the instance.
# Tailing the json logs rather than switching the containers to the awslogs
# driver keeps `docker compose logs` working for anyone who does have one.
log "Configuring the CloudWatch agent"
install -d -m 0755 /opt/aws/amazon-cloudwatch-agent/etc
cat >/opt/aws/amazon-cloudwatch-agent/etc/multica.json <<CWAGENT
{
  "agent": { "run_as_user": "root" },
  "logs": {
    "logs_collected": {
      "files": {
        "collect_list": [
          {
            "file_path": "/var/log/cloud-init-output.log",
            "log_group_name": "${LOG_GROUP}",
            "log_stream_name": "{instance_id}/bootstrap",
            "timezone": "UTC"
          },
          {
            "file_path": "/var/lib/docker/containers/*/*-json.log",
            "log_group_name": "${LOG_GROUP}",
            "log_stream_name": "{instance_id}/containers",
            "timezone": "UTC"
          }
        ]
      }
    }
  }
}
CWAGENT
/opt/aws/amazon-cloudwatch-agent/bin/amazon-cloudwatch-agent-ctl \
  -a fetch-config -m ec2 -s -c file:/opt/aws/amazon-cloudwatch-agent/etc/multica.json

log "Starting Multica"
cd "${APP_DIR}"
docker compose pull
docker compose up -d

# Compose is started by a unit as well, so a reboot brings the stack back
# before anyone has to log in and do it by hand.
cat >/etc/systemd/system/multica.service <<'UNIT'
[Unit]
Description=Multica self-hosted stack
Requires=docker.service
After=docker.service network-online.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=true
WorkingDirectory=/opt/multica
ExecStart=/usr/bin/docker compose up -d
ExecStop=/usr/bin/docker compose down

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable multica.service
systemctl enable --now caddy.service

log "Bootstrap complete: ${PUBLIC_URL}"
