#!/usr/bin/env bash
# Dispatch site-factory tickets through Multica self-host (daemon runtime).
# shellcheck shell=bash

site_factory_resolve_multica_agent() {
  if [ -n "${SITE_FACTORY_MULTICA_AGENT_ID:-}" ]; then
    echo "$SITE_FACTORY_MULTICA_AGENT_ID"
    return 0
  fi
  if [ -n "${MULTICA_DEV_AGENT_ID:-}" ]; then
    echo "$MULTICA_DEV_AGENT_ID"
    return 0
  fi
  multica agent list --output json 2>/dev/null | python3 -c "
import json, sys
raw = sys.stdin.read().strip()
if not raw:
    sys.exit(1)
agents = json.loads(raw)
candidates = []
for row in agents:
    if row.get('archived_at'):
        continue
    if row.get('runtime_bound') and row.get('runtime_mode') == 'local':
        candidates.append(row)
if not candidates:
    candidates = [r for r in agents if not r.get('archived_at')]
if not candidates:
    sys.exit(1)
# Prefer idle agents; stable sort by name
candidates.sort(key=lambda r: (r.get('status') != 'idle', r.get('name') or ''))
print(candidates[0]['id'])
"
}

site_factory_dispatch_via_multica() {
  local repo="$1"
  local target="$2"
  local slug="$3"
  local slots="$4"
  local log_file="$5"

  if ! site_factory_daemon_running; then
    echo "multica dispatch: daemon not running" >>"$log_file"
    return 1
  fi

  local agent_id
  agent_id="$(site_factory_resolve_multica_agent)" || {
    echo "multica dispatch: no agent id (set SITE_FACTORY_MULTICA_AGENT_ID)" >>"$log_file"
    return 1
  }

  local brief="$target/.delivery/$slug/brief.md"
  local research="$target/.delivery/$slug/research.md"
  local desc_extra=""
  [ -f "$research" ] && desc_extra="$(head -c 4000 "$research")"

  local parent_json
  parent_json="$(multica issue create \
    --title "🏭 Site factory — $slug" \
    --description "CEO one-line site factory pipeline.

Repo: $target
GitHub: https://github.com/$repo
Delivery: .delivery/$slug/

---
Research excerpt:
${desc_extra:-_(none)_}
" \
    --status todo \
    --output json 2>>"$log_file")" || return 1

  local parent_id
  parent_id="$(python3 - "$parent_json" <<'PY'
import json, sys
data = json.loads(sys.argv[1])
print(data.get("id", ""))
PY
)"
  [ -n "$parent_id" ] || return 1
  echo "multica dispatch: parent issue $parent_id" >>"$log_file"

  local issues_json count=0
  issues_json="$(gh issue list -R "$repo" -l agent-safe -s open --json number,title,body,url 2>/dev/null || echo '[]')"

  while read -r num; do
    [ -z "$num" ] && continue
    [ "$count" -ge "$slots" ] && break
    local issue_json title body url
    issue_json="$(gh issue view "$num" -R "$repo" --json title,body,url 2>/dev/null || echo '{}')"
    title="$(echo "$issue_json" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('title',''))")"
    url="$(echo "$issue_json" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('url',''))")"
    body="$(echo "$issue_json" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('body') or '')")"

    local stage=$((count + 1))
    local child_json child_id
    child_json="$(multica issue create \
      --title "$title" \
      --parent "$parent_id" \
      --stage "$stage" \
      --description "GitHub mirror: $url

Repo path: $target
Read .delivery/$slug/brief.md and accept_cases.md before coding.
Follow orchestrator pipeline (Planner→Implementer→Verifier).

---
$body" \
      --status "$([ "$stage" -eq 1 ] && echo todo || echo backlog)" \
      --output json 2>>"$log_file")" || continue

    child_id="$(python3 - "$child_json" <<'PY'
import json, sys
data = json.loads(sys.argv[1])
print(data.get("id", ""))
PY
)"
    [ -z "$child_id" ] && continue

    if [ "$stage" -eq 1 ]; then
      multica issue assign "$child_id" --to-id "$agent_id" >>"$log_file" 2>&1 || true
      gh issue edit "$num" -R "$repo" --add-label "agent-running" 2>/dev/null || true
      gh issue comment "$num" -R "$repo" --body "🤖 Dispatched via Multica daemon (issue \`$child_id\`, agent \`$agent_id\`)" 2>/dev/null || true
    fi
    echo "multica dispatch: github #$num → multica $child_id (stage $stage)" >>"$log_file"
    count=$((count + 1))
  done < <(
    python3 - "$issues_json" "$slots" <<'PY'
import json, sys
issues = json.loads(sys.argv[1])
limit = int(sys.argv[2])
issues.sort(key=lambda row: row["number"])
for row in issues[:limit]:
    print(row["number"])
PY
  )

  if [ "$count" -eq 0 ]; then
    echo "multica dispatch: no issues assigned" >>"$log_file"
    return 1
  fi
  echo "multica dispatch: started $count staged issue(s) on agent $agent_id" >>"$log_file"
  return 0
}
