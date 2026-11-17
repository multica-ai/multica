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

echo "== DC-6 fail-closed deletion evidence =="
FAIL_CLOSED_DB="$(
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -Atqc "SELECT 'moj2166_dc6_' || replace(gen_random_uuid()::text, '-', '_')"
)"
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qc "CREATE DATABASE $FAIL_CLOSED_DB"
FAIL_CLOSED_URL="$(
  python3 - "$DATABASE_URL" "$FAIL_CLOSED_DB" <<'PY'
import sys
from urllib.parse import urlsplit, urlunsplit
u = urlsplit(sys.argv[1])
host = u.hostname or "localhost"
port = f":{u.port}" if u.port else ""
netloc = f"{u.username or ''}{':' + u.password if u.password else ''}@{host}{port}"
print(urlunsplit((u.scheme, netloc, "/" + sys.argv[2], u.query, u.fragment)))
PY
)"
cleanup_dc6() {
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qc "DROP DATABASE IF EXISTS $FAIL_CLOSED_DB"
}
trap cleanup_dc6 EXIT
psql "$FAIL_CLOSED_URL" -v ON_ERROR_STOP=1 >/tmp/moj2166_dc6.out 2>&1 <<'SQL'
CREATE TABLE issue(id bigint primary key);
CREATE TABLE issue_archive(id bigint primary key);
CREATE FUNCTION archive_issue_on_delete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO issue_archive(id) VALUES (OLD.id);
  RETURN OLD;
END;
$$;
CREATE TRIGGER trg_issue_archive_on_delete BEFORE DELETE ON issue
FOR EACH ROW EXECUTE FUNCTION archive_issue_on_delete();
ALTER TABLE issue_archive ADD CONSTRAINT tmp_fail_closed CHECK (false) NOT VALID;
INSERT INTO issue(id) VALUES (1);
BEGIN;
DELETE FROM issue WHERE id = 1;
SQL
if grep -q 'tmp_fail_closed' /tmp/moj2166_dc6.out; then
  ISSUE_COUNT="$(psql "$FAIL_CLOSED_URL" -Atqc 'SELECT count(*) FROM issue')"
  ARCHIVE_COUNT="$(psql "$FAIL_CLOSED_URL" -Atqc 'SELECT count(*) FROM issue_archive')"
  if [[ "$ISSUE_COUNT" == 1 && "$ARCHIVE_COUNT" == 0 ]]; then
    echo "PASS DC-6 issue_count=$ISSUE_COUNT archive_count=$ARCHIVE_COUNT"
  else
    echo "DC-6 FAIL: issue=$ISSUE_COUNT archive=$ARCHIVE_COUNT, want issue=1 archive=0" >&2
    exit 1
  fi
else
  echo "DC-6 FAIL: expected tmp_fail_closed rejection while archiving" >&2
  cat /tmp/moj2166_dc6.out >&2
  exit 1
fi
rm -f /tmp/moj2166_dc6.out
trap - EXIT
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qc "DROP DATABASE $FAIL_CLOSED_DB"

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
