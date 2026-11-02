#!/usr/bin/env bash
# Day-to-day operations on the running self-hosted server, over Session
# Manager. No SSH, no open port.
#
#   ./scripts/selfhost-apply.sh            re-run the bootstrap in place
#   ./scripts/selfhost-apply.sh --logs     tail the bootstrap log
#   ./scripts/selfhost-apply.sh --code     print the latest login code
#   ./scripts/selfhost-apply.sh --status   container and proxy status
#
# Why a re-run rather than an instance replacement: the database volume is
# attached to this instance, so the stack deliberately does not replace it when
# the bootstrap script changes. `cdk deploy` updates the launch template; this
# takes the new script from there and runs it on the live machine. The script
# is idempotent — it reformats nothing that already holds a filesystem.
set -euo pipefail

cd "$(dirname "$0")/.."

STACK="${MULTICA_SELFHOST_STACK:-Multica-SelfHost}"
region="${AWS_REGION:-$(aws configure get region)}"

stack_output() {
  aws cloudformation describe-stacks --region "${region}" --stack-name "${STACK}" \
    --query "Stacks[0].Outputs[?OutputKey=='$1'].OutputValue" --output text
}

instance="$(stack_output InstanceId)"
if [ -z "${instance}" ] || [ "${instance}" = "None" ]; then
  echo "Could not read InstanceId from stack ${STACK} in ${region}." >&2
  exit 1
fi

run_remote() {
  local description="$1" command="$2" command_id
  command_id="$(aws ssm send-command --region "${region}" \
    --instance-ids "${instance}" \
    --document-name AWS-RunShellScript \
    --comment "${description}" \
    --parameters "commands=[\"${command}\"]" \
    --query Command.CommandId --output text)"

  aws ssm wait command-executed --region "${region}" \
    --command-id "${command_id}" --instance-id "${instance}" 2>/dev/null || true

  aws ssm get-command-invocation --region "${region}" \
    --command-id "${command_id}" --instance-id "${instance}" \
    --query '[StandardOutputContent,StandardErrorContent]' --output text
}

case "${1:-}" in
  --logs)
    run_remote "bootstrap log" "tail -n 200 /var/log/cloud-init-output.log"
    ;;
  --code)
    # Only relevant when no Resend key is configured: the backend prints the
    # verification code to its own log instead of emailing it.
    run_remote "login code" \
      "cd /opt/multica && docker compose logs --tail 400 backend | grep -i 'verification code' | tail -5"
    ;;
  --status)
    run_remote "status" \
      "cd /opt/multica && docker compose ps && systemctl is-active caddy multica"
    ;;
  "")
    echo "Re-running the bootstrap on ${instance}."
    echo "Containers restart, so the app is briefly unavailable."
    read -rp "Continue? (y/N) " reply
    case "${reply}" in
      y | Y) ;;
      *) echo "Cancelled."; exit 0 ;;
    esac

    launch_template="$(aws ec2 describe-instances --region "${region}" \
      --instance-ids "${instance}" \
      --query 'Reservations[0].Instances[0].Tags[?Key==`aws:ec2launchtemplate:id`].Value' \
      --output text)"
    if [ -z "${launch_template}" ] || [ "${launch_template}" = "None" ]; then
      echo "Could not find the launch template for ${instance}." >&2
      exit 1
    fi

    user_data="$(aws ec2 describe-launch-template-versions --region "${region}" \
      --launch-template-id "${launch_template}" --versions '$Latest' \
      --query 'LaunchTemplateVersions[0].LaunchTemplateData.UserData' --output text)"

    # The script travels as base64 so quoting and newlines survive the trip
    # through the SSM parameter.
    run_remote "re-run bootstrap" \
      "echo '${user_data}' | base64 -d > /root/bootstrap.sh && bash /root/bootstrap.sh"
    ;;
  *)
    echo "Unknown option: $1" >&2
    sed -n '2,10p' "$0" >&2
    exit 1
    ;;
esac
