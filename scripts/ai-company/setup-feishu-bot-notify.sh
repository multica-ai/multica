#!/usr/bin/env bash
# Wire CEO brief notifications to a Feishu *open-platform* bot (private DM).
# Alternative to group webhook — uses the same app as feishu-cursor-claw.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CONFIG_DIR="$MULTICA_ROOT/.ai-company/config"
OUT_ENV="$CONFIG_DIR/feishu-bot-notify.env"
CLAW_ENV="${FEISHU_CURSOR_CLAW_ENV:-$HOME/Projects/feishu-cursor-claw/.env}"

usage() {
  cat <<'EOF'
Usage: setup-feishu-bot-notify.sh [--claw-env PATH]

Reads FEISHU_APP_ID / FEISHU_APP_SECRET from feishu-cursor-claw .env (or --claw-env),
resolves the app owner's open_id, writes .ai-company/config/feishu-bot-notify.env,
and sends a test CEO brief line to your Feishu private chat with that bot.

No Feishu *group* webhook required.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --claw-env) CLAW_ENV="${2:?}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

if [ ! -f "$CLAW_ENV" ]; then
  echo "error: claw .env not found: $CLAW_ENV" >&2
  exit 1
fi

# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"
# shellcheck source=lib/notify.sh
source "$SCRIPT_DIR/lib/notify.sh"

python3 - "$CLAW_ENV" "$OUT_ENV" <<'PY'
import json
import os
import re
import sys
import urllib.request
from pathlib import Path

claw_path, out_path = sys.argv[1], sys.argv[2]
env = {}
for line in Path(claw_path).read_text().splitlines():
    line = line.strip()
    if not line or line.startswith("#") or "=" not in line:
        continue
    k, v = line.split("=", 1)
    env[k.strip()] = v.strip()

app_id = env.get("FEISHU_APP_ID", "")
app_secret = env.get("FEISHU_APP_SECRET", "")
if not app_id or not app_secret:
    raise SystemExit("error: FEISHU_APP_ID/SECRET missing in claw .env")

tok_req = urllib.request.Request(
    "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal",
    data=json.dumps({"app_id": app_id, "app_secret": app_secret}).encode(),
    headers={"Content-Type": "application/json"},
    method="POST",
)
with urllib.request.urlopen(tok_req, timeout=30) as resp:
    token = json.load(resp)["tenant_access_token"]

app_req = urllib.request.Request(
    f"https://open.feishu.cn/open-apis/application/v6/applications/{app_id}?lang=zh_cn",
    headers={"Authorization": f"Bearer {token}"},
)
with urllib.request.urlopen(app_req, timeout=30) as resp:
    app = json.load(resp)
if app.get("code") != 0:
    raise SystemExit(f"error: app info: {app.get('msg')}")

owner = app.get("data", {}).get("app", {}).get("owner", {})
open_id = owner.get("owner_id") or app.get("data", {}).get("app", {}).get("creator_id")
if not open_id:
    raise SystemExit("error: could not resolve owner open_id from Feishu app")

out = Path(out_path)
out.parent.mkdir(parents=True, exist_ok=True)
out.write_text(
    "\n".join(
        [
            "# Feishu open-platform bot DM (gitignored). Sourced by ai-company scripts.",
            f"export FEISHU_BOT_APP_ID={app_id}",
            f"export FEISHU_BOT_APP_SECRET={app_secret}",
            f"export FEISHU_BOT_NOTIFY_OPEN_ID={open_id}",
            "",
        ]
    )
)
print(f"ok: wrote {out_path} (open_id={open_id})")
PY

# shellcheck disable=SC1090
source "$OUT_ENV"

test_msg="AI 公司日报测试（Bot 私聊）— $(date '+%Y-%m-%d %H:%M') — nightly cron 已就绪"
if notify_ceo_brief "$test_msg"; then
  echo "ok: test DM sent via Feishu bot"
else
  echo "error: notify failed" >&2
  exit 1
fi
