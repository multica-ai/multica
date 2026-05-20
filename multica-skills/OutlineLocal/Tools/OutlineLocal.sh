#!/usr/bin/env bash
# Minimal read-only Outline client. Uses OUTLINE_API_KEY directly with no
# per-user filtering — returns whatever the admin token can see.
#
# Usage:
#   OutlineLocal.sh search       <query> [limit]
#   OutlineLocal.sh get          <doc_id_or_slug>
#   OutlineLocal.sh collections
#   OutlineLocal.sh list         <collection_id> [limit]

set -euo pipefail

BASE="https://docs.zoop.tools/api"

if [[ -z "${OUTLINE_API_KEY:-}" ]]; then
  echo "OUTLINE_API_KEY not set" >&2
  exit 1
fi

api() {
  local resp
  resp=$(curl -sS -X POST "$BASE/$1" \
           -H "Authorization: Bearer $OUTLINE_API_KEY" \
           -H "Content-Type: application/json" \
           -d "$2")
  if ! echo "$resp" | jq -e '.ok != false' >/dev/null 2>&1; then
    echo "outline api $1 failed: $(echo "$resp" | jq -r '.error // "unknown"')" >&2
    return 3
  fi
  printf '%s' "$resp"
}

cmd_search() {
  local query="${1:-}" limit="${2:-10}"
  [[ -z "$query" ]] && { echo "query is required" >&2; exit 2; }
  api "documents.search" \
    "$(jq -n --arg q "$query" --argjson l "$limit" '{query:$q,limit:$l}')" \
    | jq '.data[] | {title: .document.title, url: .document.url, snippet: .context}'
}

cmd_get() {
  local doc="${1:-}"
  [[ -z "$doc" ]] && { echo "doc id or slug is required" >&2; exit 2; }
  api "documents.info" "$(jq -n --arg i "$doc" '{id:$i}')" \
    | jq '.data | {title, url, text}'
}

cmd_collections() {
  api "collections.list" '{"limit":100}' \
    | jq '.data[] | {id, name, description}'
}

cmd_list() {
  local collection="${1:-}" limit="${2:-100}"
  [[ -z "$collection" ]] && { echo "collection id is required" >&2; exit 2; }
  api "documents.list" \
    "$(jq -n --arg c "$collection" --argjson l "$limit" '{collectionId:$c,limit:$l}')" \
    | jq '.data[] | {id, title, url}'
}

case "${1:-}" in
  search)      shift; cmd_search "$@" ;;
  get)         shift; cmd_get "$@" ;;
  collections) shift; cmd_collections "$@" ;;
  list)        shift; cmd_list "$@" ;;
  ""|-h|--help)
    echo "usage: OutlineLocal.sh {search|get|collections|list} ..." >&2; exit 1 ;;
  *) echo "unknown command: $1" >&2; exit 1 ;;
esac
