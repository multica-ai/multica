#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Clean Multica issues that were created from a specific Feishu Project sync.

The script matches issues through feishu_project_issue_binding, so it only
targets synced Feishu Project issues and leaves manually-created issues alone.
It runs in dry-run mode by default. Pass --execute to delete.

Usage:
  scripts/ops/clean-feishu-project-issues.sh \
    --workspace-slug fangangmin \
    --project-key 6718cd2a1d3fdf1b50810683

  scripts/ops/clean-feishu-project-issues.sh \
    --workspace-slug fangangmin \
    --project-key 6718cd2a1d3fdf1b50810683 \
    --execute

Options:
  --namespace NS        Kubernetes namespace, default: multica
  --workspace-slug SLUG Multica workspace slug
  --project-key KEY    Feishu Project key recorded on bindings
  --work-item-type T   Work item type, default: issue
  --execute            Actually delete matching issues
  -h, --help           Show this help

Notes:
  Deleting the issue cascades DB rows such as bindings, comments, tasks,
  inbox items, labels, and attachment records. Object-storage files referenced
  by attachment rows are not deleted by this SQL cleanup path.
EOF
}

namespace="multica"
workspace_slug=""
project_key=""
work_item_type="issue"
execute=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      namespace="${2:-}"
      shift 2
      ;;
    --workspace-slug)
      workspace_slug="${2:-}"
      shift 2
      ;;
    --project-key)
      project_key="${2:-}"
      shift 2
      ;;
    --work-item-type)
      work_item_type="${2:-}"
      shift 2
      ;;
    --execute)
      execute=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$workspace_slug" || -z "$project_key" ]]; then
  echo "--workspace-slug and --project-key are required" >&2
  usage >&2
  exit 2
fi

psql_pod="$(
  kubectl -n "$namespace" get pods --no-headers \
    | awk '$1 ~ /^psql-debug-/ && $3 == "Running" {print $1; exit}'
)"
if [[ -z "$psql_pod" ]]; then
  echo "no running psql-debug pod found in namespace $namespace" >&2
  exit 1
fi

mode="DRY RUN"
if [[ "$execute" -eq 1 ]]; then
  mode="EXECUTE"
fi

echo "mode: $mode"
echo "namespace: $namespace"
echo "psql pod: $psql_pod"
echo "workspace slug: $workspace_slug"
echo "project key: $project_key"
echo "work item type: $work_item_type"
echo

sql=$(cat <<'SQL'
\set ON_ERROR_STOP on
begin;

create temp table target_issues as
select
  i.id,
  i.title,
  i.status,
  i.created_at,
  b.external_identifier,
  b.work_item_id,
  b.project_key
from issue i
join workspace w on w.id = i.workspace_id
join feishu_project_issue_binding b
  on b.issue_id = i.id
 and b.workspace_id = i.workspace_id
where w.slug = :'workspace_slug'
  and b.project_key = :'project_key'
  and b.work_item_type = :'work_item_type';

\echo 'matched issues:'
select count(*) from target_issues;

\echo 'matched attachments:'
select count(*)
from attachment a
join target_issues t on t.id = a.issue_id;

\echo 'sample:'
select id, external_identifier, status, left(title, 100) as title, created_at
from target_issues
order by created_at asc
limit 20;

SQL
)

if [[ "$execute" -eq 1 ]]; then
  sql+=$(cat <<'SQL'

\echo 'deleting issues:'
delete from issue
where id in (select id from target_issues);

commit;
SQL
)
else
  sql+=$(cat <<'SQL'

\echo 'dry run only; pass --execute to delete'
rollback;
SQL
)
fi

kubectl -n "$namespace" exec -i "$psql_pod" -- sh -lc \
  'psql "$DATABASE_URL" -v workspace_slug="$1" -v project_key="$2" -v work_item_type="$3" -f -' \
  sh "$workspace_slug" "$project_key" "$work_item_type" <<<"$sql"
