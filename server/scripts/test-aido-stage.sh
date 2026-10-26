#!/bin/sh
set -eu
: "${DATABASE_URL:?Set an isolated nex1565_* database URL}"
python3 -c 'import os,urllib.parse; assert urllib.parse.urlparse(os.environ["DATABASE_URL"]).path.lstrip("/").startswith("nex1565_"), "Use an isolated nex1565_* database"'
# These packages maintain SQL fixtures; do not run their TestMain cleanup in
# parallel against the same isolated database. Use a fresh migrated DB per run.
go test -p 1 ./internal/handler ./internal/middleware ./cmd/multica ./cmd/server -count=1
