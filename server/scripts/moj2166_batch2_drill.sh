#!/usr/bin/env bash
# MOJ-2166 batch 2 migration drill.
# Run only against a disposable database whose name is explicitly drill-shaped.
# DATABASE_URL=postgresql://.../multica_moj2166_drill ./scripts/moj2166_batch2_drill.sh
set -euo pipefail

SERVER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATABASE_URL="${DATABASE_URL:-}"
[[ -n "$DATABASE_URL" ]] || { echo "DATABASE_URL is required" >&2; exit 2; }

DB_NAME="$(awk 'BEGIN{u=ENVIRON["DATABASE_URL"]; n=substr(u,index(u,"//")+2); sub(/\?.*/,"",n); if (index(n,"/")>0) print substr(n,index(n,"/")+1); else print ""}')"
[[ "$DB_NAME" =~ (^|_)(drill|copy|staging)(_|$) ]] || {
  echo "refusing non-drill database: $DB_NAME" >&2
  exit 2
}

cd "$SERVER_DIR"
export GOTOOLCHAIN=auto

echo "== baseline evidence archive rows =="
BEFORE_ARCHIVES="$(psql "$DATABASE_URL" -Atqc 'SELECT count(*) FROM issue_archive')"
BEFORE_COMMENTS="$(psql "$DATABASE_URL" -Atqc 'SELECT count(*) FROM issue_archive_comment')"
printf '%s\n' "$BEFORE_ARCHIVES" "$BEFORE_COMMENTS"

echo "== apply 683 =="
go run ./cmd/migrate up
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -Atqc "
  SELECT to_regclass('public.issue_archive') IS NOT NULL
     AND to_regclass('public.issue_archive_comment') IS NOT NULL
     AND EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_issue_archive_on_delete')
     AND NOT EXISTS (
       SELECT 1 FROM pg_constraint
       WHERE conrelid = 'activity_log'::regclass
         AND conname = 'activity_log_issue_id_fkey'
         AND pg_get_constraintdef(oid) LIKE '%ON DELETE CASCADE%'
     );
" | grep -qx t

echo "== handler deletion audit tests =="
go test ./internal/handler -run 'TestDeleteIssue|TestBatchDeleteIssues|DeleteIssueArchivesImmutableEvidence' -count=1

echo "== immutable rows before rollback =="
AFTER_UP_ARCHIVES="$(psql "$DATABASE_URL" -Atqc 'SELECT count(*) FROM issue_archive')"
AFTER_UP_COMMENTS="$(psql "$DATABASE_URL" -Atqc 'SELECT count(*) FROM issue_archive_comment')"

echo "== roll back 683 without dropping archives =="
go run ./cmd/migrate down
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -Atqc "
  SELECT to_regclass('public.issue_archive') IS NOT NULL
     AND to_regclass('public.issue_archive_comment') IS NOT NULL
     AND NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_issue_archive_on_delete');
" | grep -qx t
AFTER_DOWN_ARCHIVES="$(psql "$DATABASE_URL" -Atqc 'SELECT count(*) FROM issue_archive')"
AFTER_DOWN_COMMENTS="$(psql "$DATABASE_URL" -Atqc 'SELECT count(*) FROM issue_archive_comment')"

[[ "$AFTER_UP_ARCHIVES" == "$AFTER_DOWN_ARCHIVES" ]]
[[ "$AFTER_UP_COMMENTS" == "$AFTER_DOWN_COMMENTS" ]]
[[ "$AFTER_UP_ARCHIVES" -ge "$BEFORE_ARCHIVES" ]]

printf 'PASS drill_db=%s archives_before=%s after_up=%s after_down=%s comments_after_up=%s after_down=%s\n' \
  "$DB_NAME" "$BEFORE_ARCHIVES" "$AFTER_UP_ARCHIVES" "$AFTER_DOWN_ARCHIVES" \
  "$AFTER_UP_COMMENTS" "$AFTER_DOWN_COMMENTS"
