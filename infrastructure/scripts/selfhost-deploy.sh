#!/usr/bin/env bash
# Deploys the self-hosted Multica server.
#
# Usage:
#   ./scripts/selfhost-deploy.sh you@example.com,teammate@example.com
#   ./scripts/selfhost-deploy.sh you@example.com -c domainName=multica.example.com
#
# The first argument is the signup allowlist, and it is required: port 443 is
# open to the internet, so without it anyone who finds the URL could create an
# account. Anything after it is passed through to `cdk deploy`.
#
# Approval stays at CDK's default, so a change that widens a security group or
# an IAM policy stops and asks.
set -euo pipefail

cd "$(dirname "$0")/.."

if [ $# -lt 1 ]; then
  echo "Usage: $0 <allowed-emails> [cdk args...]" >&2
  echo "   e.g. $0 you@example.com,teammate@example.com" >&2
  exit 1
fi

allowed_emails="$1"
shift

region="${AWS_REGION:-$(aws configure get region)}"
echo "Account: $(aws sts get-caller-identity --query Account --output text)"
echo "Region:  ${region}"
echo "Signup allowlist: ${allowed_emails}"
echo

context=(-c selfHost=true -c "allowedEmails=${allowed_emails}")

echo "==> Changes"
npx cdk diff Multica-SelfHost "${context[@]}" "$@"

echo
read -rp "Deploy these changes? (y/N) " reply
case "${reply}" in
  y | Y) ;;
  *)
    echo "Cancelled."
    exit 0
    ;;
esac

npx cdk deploy Multica-SelfHost --progress events "${context[@]}" "$@"

echo
echo "First boot pulls three container images and asks Let's Encrypt for a"
echo "certificate, so give it a few minutes before the URL answers."
echo "Watch it with:  ./scripts/selfhost-apply.sh --logs"
