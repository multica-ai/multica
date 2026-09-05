#!/usr/bin/env bash
# Check whether changed files in a PR are eligible for auto-merge per merge-policy.json.
set -euo pipefail

PR_NUMBER="${1:?usage: check-merge-eligible.sh <pr_number>}"
REPO="${GITHUB_REPOSITORY:-multica-ai/multica}"
ROOT="${REPO_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}"
POLICY="${MERGE_POLICY_PATH:-$ROOT/.delivery/config/merge-policy.json}"

if [ ! -f "$POLICY" ]; then
  echo "merge_eligible=false reason=policy_missing"
  exit 1
fi

ENABLED="$(jq -r '.autoMergeEnabled' "$POLICY")"
if [ "$ENABLED" != "true" ]; then
  echo "merge_eligible=false reason=auto_merge_disabled"
  exit 0
fi

BASE="$(gh pr view "$PR_NUMBER" -R "$REPO" --json baseRefName -q .baseRefName)"
HEAD="$(gh pr view "$PR_NUMBER" -R "$REPO" --json headRefName -q .headRefName)"
BRANCH_PREFIX="$(jq -r '.branchNamePrefix // "cursor-issue"' "$POLICY")"

if [[ "$HEAD" != ${BRANCH_PREFIX}* ]]; then
  echo "merge_eligible=false reason=branch_prefix"
  exit 0
fi

# Optional: linked issues must carry required labels (e.g. agent-safe).
REQUIRE_LABELS="$(jq -r '.requireLabels // [] | join("\n")' "$POLICY")"
if [ -n "$REQUIRE_LABELS" ]; then
  issue_nums="$(
    gh pr view "$PR_NUMBER" -R "$REPO" --json closingIssuesReferences -q '.closingIssuesReferences[].number' 2>/dev/null || true
  )"
  if [ -z "$issue_nums" ]; then
    echo "merge_eligible=false reason=no_linked_issue"
    exit 0
  fi
  for issue_num in $issue_nums; do
    [ -z "$issue_num" ] && continue
    labels="$(
      gh issue view "$issue_num" -R "$REPO" --json labels -q '[.labels[].name] | join(",")' 2>/dev/null || true
    )"
    while IFS= read -r required; do
      [ -z "$required" ] && continue
      if [[ ",$labels," != *",$required,"* ]]; then
        echo "merge_eligible=false reason=missing_label issue=$issue_num label=$required"
        exit 0
      fi
    done <<<"$REQUIRE_LABELS"
  done
fi

FILES=()
while IFS= read -r line; do
  [ -n "$line" ] && FILES+=("$line")
done < <(gh pr diff "$PR_NUMBER" -R "$REPO" --name-only)

deny_patterns=()
while IFS= read -r line; do
  deny_patterns+=("$line")
done < <(jq -r '.deny[]' "$POLICY")

allow_patterns=()
while IFS= read -r line; do
  allow_patterns+=("$line")
done < <(jq -r '.allow[]' "$POLICY")

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

for f in "${FILES[@]}"; do
  for d in "${deny_patterns[@]}"; do
    if path_matches_glob "$f" "$d"; then
      echo "merge_eligible=false reason=deny_path file=$f pattern=$d"
      exit 0
    fi
  done
done

for f in "${FILES[@]}"; do
  allowed=false
  for a in "${allow_patterns[@]}"; do
    if path_matches_glob "$f" "$a"; then
      allowed=true
      break
    fi
  done
  if [ "$allowed" = false ]; then
    echo "merge_eligible=false reason=not_in_allowlist file=$f"
    exit 0
  fi
done

echo "merge_eligible=true base=$BASE head=$HEAD files=${#FILES[@]}"
exit 0
