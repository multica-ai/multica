#!/usr/bin/env bash
# One-time preparation for the self-hosted Multica server:
#   1. bootstrap the CDK toolkit in the target region
#   2. generate and store the two secrets the server needs
#
# The secrets are generated here and stored encrypted in SSM Parameter Store.
# They are never printed, never put in a CloudFormation template, and never
# committed. Nothing but the instance role can read them back.
#
# Usage: ./scripts/selfhost-setup.sh
set -euo pipefail

cd "$(dirname "$0")/.."

PARAM_PREFIX="${MULTICA_SELFHOST_PARAM_PREFIX:-/multica/self-host}"

command -v aws >/dev/null || { echo "aws CLI is required" >&2; exit 1; }
command -v openssl >/dev/null || { echo "openssl is required to generate the secrets" >&2; exit 1; }

account="$(aws sts get-caller-identity --query Account --output text)"
region="${AWS_REGION:-$(aws configure get region)}"
if [ -z "${region}" ]; then
  echo "No region configured. Set AWS_REGION or run 'aws configure'." >&2
  exit 1
fi

echo "Account: ${account}"
echo "Region:  ${region}"
echo

echo "==> Bootstrapping the CDK toolkit"
npx cdk bootstrap "aws://${account}/${region}"

generate_parameter() {
  local name="$1" description="$2"
  local path="${PARAM_PREFIX}/${name}"

  if aws ssm get-parameter --region "${region}" --name "${path}" >/dev/null 2>&1; then
    echo "==> ${path} already exists, leaving it alone"
    return
  fi

  echo "==> ${path} — ${description}"
  aws ssm put-parameter --region "${region}" --name "${path}" --type SecureString \
    --value "$(openssl rand -hex 32)" >/dev/null
  echo "    generated and stored"
}

generate_parameter jwt-secret "signs session tokens"
generate_parameter postgres-password "the database password"

echo
echo "Optional: email delivery for login codes."
echo "Without it the backend prints each code to its own log, which"
echo "./scripts/selfhost-apply.sh --code reads for you."
read -rp "Store a Resend API key now? (y/N) " reply
case "${reply}" in
  y | Y)
    read -rsp "  Resend API key (input hidden): " resend_key
    echo
    if [ -n "${resend_key}" ]; then
      aws ssm put-parameter --region "${region}" --name "${PARAM_PREFIX}/resend-api-key" \
        --type SecureString --value "${resend_key}" --overwrite >/dev/null
      echo "  stored"
    fi
    ;;
  *) echo "  skipped" ;;
esac

echo
echo "Done. Deploy with:"
echo "  ./scripts/selfhost-deploy.sh you@example.com,teammate@example.com"
