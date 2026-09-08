#!/usr/bin/env bash
# @vitest-environment node — shell tests for path glob matching used by check-merge-eligible.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=check-merge-eligible.sh
source() { :; }

path_matches_glob() {
  python3 - "$1" "$2" <<'PY'
import re
import sys

path = sys.argv[1].replace("\\", "/")
pattern = sys.argv[2]

def glob_to_re(glob: str) -> str:
    i = 0
    n = len(glob)
    out: list[str] = []
    while i < n:
        if i + 1 < n and glob[i : i + 2] == "**":
            if i + 2 < n and glob[i + 2] == "/":
                out.append("(?:.*/)?")
                i += 3
            else:
                out.append(".*")
                i += 2
            continue
        ch = glob[i]
        if ch == "*":
            out.append("[^/]*")
        elif ch == "?":
            out.append("[^/]")
        else:
            out.append(re.escape(ch))
        i += 1
    return "^" + "".join(out) + "$"

sys.exit(0 if re.match(glob_to_re(pattern), path) else 1)
PY
}

assert_match() {
  local file="$1" pattern="$2"
  if path_matches_glob "$file" "$pattern"; then
    echo "ok match: $pattern ~ $file"
  else
    echo "FAIL expected match: $pattern ~ $file" >&2
    exit 1
  fi
}

assert_no_match() {
  local file="$1" pattern="$2"
  if path_matches_glob "$file" "$pattern"; then
    echo "FAIL unexpected match: $pattern ~ $file" >&2
    exit 1
  else
    echo "ok no-match: $pattern ~ $file"
  fi
}

# Regression: deny **/migrations/** must not match delivery markdown
assert_no_match ".delivery/meigen-replica/accept_cases.md" "**/migrations/**"
assert_no_match ".delivery/meigen-replica/brief.md" "**/auth/**"
assert_match "db/migrations/001.sql" "**/migrations/**"
assert_match "public/js/app.js" "public/**"
assert_match "e2e/locale.spec.ts" "e2e/**"
assert_match ".delivery/meigen-replica/plan.md" "**/*.md"
assert_no_match ".github/workflows/ci.yml" "public/**"
assert_match ".github/workflows/ci.yml" ".github/workflows/**"

echo "check-merge-eligible glob tests: PASS"
