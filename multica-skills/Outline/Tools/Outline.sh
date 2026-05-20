#!/usr/bin/env bash
# Read-only Outline client with per-user permission post-filtering.
# Uses the workspace admin token in $OUTLINE_API_KEY but enforces the
# permissions of the email passed by the agent on every call.
#
# Inside Multica, an empty <email> arg is auto-resolved from MULTICA_TASK_ID
# via Tools/ResolveTriggerEmail.sh.
#
# Usage:
#   Outline.sh search       <email> <query> [limit]
#   Outline.sh get          <email> <doc_id_or_slug>
#   Outline.sh collections  <email>
#   Outline.sh list         <email> <collection_id> [limit]

set -euo pipefail

BASE="https://docs.zoop.tools/api"

if [[ -z "${OUTLINE_API_KEY:-}" ]]; then
  echo "OUTLINE_API_KEY not set" >&2
  exit 1
fi

api() {
  curl -sS -X POST "$BASE/$1" \
    -H "Authorization: Bearer $OUTLINE_API_KEY" \
    -H "Content-Type: application/json" \
    -d "$2"
}

require_user() {
  local email="${1:-}"

  # Auto-resolve the triggering user's email when running inside a Multica
  # agent task. The daemon injects MULTICA_TASK_ID; the resolver chains
  # public API calls to map it to the user who triggered this run.
  if [[ -z "$email" && -n "${MULTICA_TASK_ID:-}" ]]; then
    local resolver
    resolver="$(dirname "${BASH_SOURCE[0]}")/ResolveTriggerEmail.sh"
    if [[ -f "$resolver" ]]; then
      email=$(bash "$resolver" 2>/dev/null || true)
    fi
  fi

  if [[ -z "$email" ]]; then
    echo "email is required" >&2; exit 2
  fi
  local body resp
  body=$(jq -n --arg q "$email" '{query:$q,limit:25,filter:"all"}')
  resp=$(api "users.list" "$body")
  if ! echo "$resp" | jq -e 'has("data")' >/dev/null 2>&1; then
    echo "user lookup failed: $(echo "$resp" | jq -r '.error // "unknown"')" >&2; exit 3
  fi
  local user
  user=$(echo "$resp" | jq -c --arg e "$email" '
    .data[]? | select((.email | ascii_downcase) == ($e | ascii_downcase))
  ' | head -n1)
  if [[ -z "$user" ]]; then
    echo "no workspace user found for email $email" >&2; exit 4
  fi
  local suspended
  suspended=$(echo "$user" | jq -r '
    if (.isSuspended // false) then "true"
    elif (.suspendedAt // null) != null then "true"
    else "false" end
  ')
  if [[ "$suspended" == "true" ]]; then
    echo "user $email is suspended; access denied" >&2; exit 5
  fi
  echo "$user" | jq -c '{id, role, email}'
}

is_admin() {
  [[ "$(echo "$1" | jq -r '.role')" == "admin" ]]
}

user_group_ids() {
  # echoes one group id per line for groups the user belongs to
  local user_id="$1"
  local resp
  resp=$(api "groups.list" "$(jq -n --arg u "$user_id" '{userId:$u,limit:100}')") || true
  echo "$resp" | jq -r '
    (.data.groups // .data // [])[]?.id // empty
  ' 2>/dev/null || true
}

visible_collection_ids() {
  # echoes one collection id per line that the user can access
  local user_json="$1"
  local user_id user_email
  user_id=$(echo "$user_json" | jq -r '.id')
  user_email=$(echo "$user_json" | jq -r '.email')

  local cols
  cols=$(api "collections.list" '{"limit":100}')
  if ! echo "$cols" | jq -e 'has("data")' >/dev/null 2>&1; then
    echo "collections.list failed: $(echo "$cols" | jq -r '.error // "unknown"')" >&2
    return 1
  fi

  # workspace-default collections (permission != null) are visible to any active user
  echo "$cols" | jq -r '.data[] | select(.permission != null) | .id'

  local private_ids
  private_ids=$(echo "$cols" | jq -r '.data[] | select(.permission == null) | .id')
  [[ -z "$private_ids" ]] && return 0

  local groups
  groups=$(user_group_ids "$user_id" | sort -u)

  local cid
  while IFS= read -r cid; do
    [[ -z "$cid" ]] && continue
    # direct user membership
    local mem
    mem=$(api "collections.memberships" \
      "$(jq -n --arg c "$cid" --arg q "$user_email" '{id:$c,query:$q,limit:25}')")
    if echo "$mem" | jq -e --arg u "$user_id" '
      (.data.users // [])[]? | select(.id == $u)
    ' >/dev/null 2>&1; then
      echo "$cid"; continue
    fi
    # group membership
    if [[ -n "$groups" ]]; then
      local gm col_groups
      gm=$(api "collections.group_memberships" \
        "$(jq -n --arg c "$cid" '{id:$c,limit:100}')")
      col_groups=$(echo "$gm" | jq -r '
        (.data.collectionGroupMemberships // .data.groups // [])[]? |
        (.groupId // .id) // empty
      ' 2>/dev/null | sort -u)
      if [[ -n "$col_groups" ]] && comm -12 <(echo "$groups") <(echo "$col_groups") | grep -q .; then
        echo "$cid"
      fi
    fi
  done <<<"$private_ids"
}

doc_membership_ok() {
  # check explicit document-level membership for a single doc
  local doc_id="$1" user_id="$2"
  local resp
  resp=$(api "documents.memberships" "$(jq -n --arg i "$doc_id" '{id:$i,limit:100}')") || return 1
  echo "$resp" | jq -e --arg u "$user_id" '
    (.data.users // [])[]? | select(.id == $u)
  ' >/dev/null 2>&1
}

cmd_search() {
  local email="${1:-}" query="${2:-}" limit="${3:-10}"
  [[ -z "$query" ]] && { echo "query is required" >&2; exit 2; }
  local user; user=$(require_user "$email")
  local resp
  resp=$(api "documents.search" \
    "$(jq -n --arg q "$query" --argjson l "$limit" '{query:$q,limit:$l}')")
  if is_admin "$user"; then
    echo "$resp" | jq '.data[] | {title: .document.title, url: .document.url, snippet: .context}'
    return
  fi
  local visible
  visible=$(visible_collection_ids "$user" | sort -u | jq -R . | jq -s .)
  echo "$resp" | jq --argjson v "$visible" '
    .data[] | select((.document.collectionId // "") as $c | ($v | index($c)) != null)
            | {title: .document.title, url: .document.url, snippet: .context}
  '
}

cmd_get() {
  local email="${1:-}" doc="${2:-}"
  [[ -z "$doc" ]] && { echo "doc id or slug is required" >&2; exit 2; }
  local user; user=$(require_user "$email")
  local resp; resp=$(api "documents.info" "$(jq -n --arg i "$doc" '{id:$i}')")
  if ! echo "$resp" | jq -e '.data' >/dev/null 2>&1; then
    echo "document not found: $doc" >&2; exit 4
  fi
  if is_admin "$user"; then
    echo "$resp" | jq '.data | {title, url, text}'
    return
  fi
  local cid did uid
  cid=$(echo "$resp" | jq -r '.data.collectionId // empty')
  did=$(echo "$resp" | jq -r '.data.id')
  uid=$(echo "$user" | jq -r '.id')
  if [[ -n "$cid" ]] && visible_collection_ids "$user" | grep -qx "$cid"; then
    echo "$resp" | jq '.data | {title, url, text}'
    return
  fi
  if doc_membership_ok "$did" "$uid"; then
    echo "$resp" | jq '.data | {title, url, text}'
    return
  fi
  echo "access denied: cannot view doc $doc" >&2; exit 7
}

cmd_collections() {
  local email="${1:-}"
  local user; user=$(require_user "$email")
  local resp; resp=$(api "collections.list" '{"limit":100}')
  if is_admin "$user"; then
    echo "$resp" | jq '.data[] | {id, name, description}'
    return
  fi
  local visible
  visible=$(visible_collection_ids "$user" | sort -u | jq -R . | jq -s .)
  echo "$resp" | jq --argjson v "$visible" '
    .data[] | select(.id as $c | ($v | index($c)) != null)
            | {id, name, description}
  '
}

cmd_list() {
  local email="${1:-}" collection="${2:-}" limit="${3:-100}"
  [[ -z "$collection" ]] && { echo "collection id is required" >&2; exit 2; }
  local user; user=$(require_user "$email")
  if ! is_admin "$user"; then
    if ! visible_collection_ids "$user" | grep -qx "$collection"; then
      echo "access denied: cannot view collection $collection" >&2; exit 7
    fi
  fi
  local resp
  resp=$(api "documents.list" \
    "$(jq -n --arg c "$collection" --argjson l "$limit" '{collectionId:$c,limit:$l}')")
  echo "$resp" | jq '.data[] | {id, title, url}'
}

case "${1:-}" in
  search)      shift; cmd_search "$@" ;;
  get)         shift; cmd_get "$@" ;;
  collections) shift; cmd_collections "$@" ;;
  list)        shift; cmd_list "$@" ;;
  ""|-h|--help)
    echo "usage: Outline.sh {search|get|collections|list} <email> ..." >&2; exit 1 ;;
  *) echo "unknown command: $1" >&2; exit 1 ;;
esac
