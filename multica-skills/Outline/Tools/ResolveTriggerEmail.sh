#!/usr/bin/env bash
# Resolve the current Multica task's triggering user → email, using only
# Multica's public API. Designed to run from inside an agent process spawned
# by the Multica daemon, where these env vars are guaranteed to be set:
#
#   MULTICA_TASK_ID, MULTICA_AGENT_ID, MULTICA_WORKSPACE_ID,
#   MULTICA_TOKEN, MULTICA_SERVER_URL
#
# Exits 0 with the email on stdout. Exits non-zero with a stderr message if
# the task has no identifiable triggering user (e.g. assignment-triggered
# tasks with no comment or chat session).
#
# This is the "no upstream patch" path: the script chains existing public
# API endpoints (agents/{id}/tasks → issues/{id}/comments OR chat/sessions/
# {id} → workspaces/{id}/members) so the Multica server stays unmodified
# and the fork can rebase on upstream cleanly forever.

set -euo pipefail

: "${MULTICA_TASK_ID:?MULTICA_TASK_ID not set}"
: "${MULTICA_AGENT_ID:?MULTICA_AGENT_ID not set}"
: "${MULTICA_WORKSPACE_ID:?MULTICA_WORKSPACE_ID not set}"
: "${MULTICA_TOKEN:?MULTICA_TOKEN not set}"
: "${MULTICA_SERVER_URL:?MULTICA_SERVER_URL not set}"

BASE="${MULTICA_SERVER_URL%/}"

api() {
  curl -fsS \
    -H "Authorization: Bearer $MULTICA_TOKEN" \
    -H "X-Workspace-ID: $MULTICA_WORKSPACE_ID" \
    "$BASE$1"
}

# 1. Locate this task in the agent's task list to get its trigger pointers.
task=$(api "/api/agents/$MULTICA_AGENT_ID/tasks" \
       | jq -c --arg id "$MULTICA_TASK_ID" '.[]? | select(.id == $id)' \
       | head -n1)

if [[ -z "$task" ]]; then
  echo "task $MULTICA_TASK_ID not found in agent $MULTICA_AGENT_ID" >&2
  exit 1
fi

issue_id=$(echo "$task"           | jq -r '.issue_id // empty')
trigger_comment_id=$(echo "$task" | jq -r '.trigger_comment_id // empty')
chat_session_id=$(echo "$task"    | jq -r '.chat_session_id // empty')

# 2. Resolve to a user_id (uuid).
user_id=""
if [[ -n "$trigger_comment_id" && -n "$issue_id" ]]; then
  user_id=$(api "/api/issues/$issue_id/comments" \
            | jq -r --arg c "$trigger_comment_id" \
              '.[]? | select(.id == $c and .author_type == "member") | .author_id' \
            | head -n1)
elif [[ -n "$chat_session_id" ]]; then
  user_id=$(api "/api/chat/sessions/$chat_session_id" | jq -r '.creator_id // empty')
fi

if [[ -z "$user_id" ]]; then
  echo "no triggering user could be identified for task $MULTICA_TASK_ID" >&2
  echo "(assignment-triggered tasks have no per-trigger user)" >&2
  exit 2
fi

# 3. Map user_id → email via the workspace member list.
email=$(api "/api/workspaces/$MULTICA_WORKSPACE_ID/members" \
        | jq -er --arg u "$user_id" '.[]? | select(.user_id == $u) | .email' \
        | head -n1)

if [[ -z "$email" ]]; then
  echo "user $user_id not found in workspace members" >&2
  exit 3
fi

printf '%s\n' "$email"
