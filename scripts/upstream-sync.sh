#!/usr/bin/env bash
# Maintain g2crowd/agentfarm in sync with upstream multica-ai/multica.
# Idempotent end-to-end: safe to re-run when there is nothing new.
set -euo pipefail

UPSTREAM_URL="https://github.com/multica-ai/multica.git"
UPSTREAM_REMOTE="upstream"
FORK_REMOTE="origin"
FORK_BRANCH="main"

# Files we never let upstream overwrite. README.md gets snapshotted into
# UPSTREAM_README.md so reviewers can still see upstream's docs.
KEEP_OURS=(CLAUDE.md README.md)
UPSTREAM_README_SNAPSHOT="UPSTREAM_README.md"

# Cursor file in the fork repo. Tracks the last synced upstream SHA so the
# next sync survives PR squash-merge (which severs git's natural ancestry).
CURSOR_FILE=".upstream-sync-cursor"

# 1. Ensure remote (idempotent).
if ! git remote get-url "${UPSTREAM_REMOTE}" >/dev/null 2>&1; then
  git remote add "${UPSTREAM_REMOTE}" "${UPSTREAM_URL}"
fi

# 2. Refuse to run on a dirty tree.
[ -z "$(git status --porcelain)" ] || { echo "working tree dirty"; exit 1; }

# 3. Fetch BOTH sides — never compute against stale refs.
git fetch "${FORK_REMOTE}" "${FORK_BRANCH}"
git fetch "${UPSTREAM_REMOTE}" main --tags

UPSTREAM_HEAD=$(git rev-parse "${UPSTREAM_REMOTE}/main")
UPSTREAM_SHORT=$(git rev-parse --short=7 "${UPSTREAM_HEAD}")

# 4. Resolve fork-point. Prefer the explicit cursor (survives squash-merge);
#    fall back to git merge-base only on first run (no cursor present).
if [ -f "${CURSOR_FILE}" ]; then
  FORK_POINT=$(tr -d '[:space:]' < "${CURSOR_FILE}")
else
  FORK_POINT=$(git merge-base "${FORK_REMOTE}/${FORK_BRANCH}" "${UPSTREAM_REMOTE}/main")
fi
FORK_SHORT=$(git rev-parse --short=7 "${FORK_POINT}")

# 5. Nothing to sync — exit clean.
if [ "${FORK_POINT}" = "${UPSTREAM_HEAD}" ]; then
  echo "in sync at ${UPSTREAM_SHORT}; nothing to do"
  exit 0
fi

BRANCH="upstream-sync/multica-${FORK_SHORT}-to-${UPSTREAM_SHORT}"
git checkout -b "${BRANCH}" "${FORK_REMOTE}/${FORK_BRANCH}"

# 6. Merge with --no-commit so we can apply the fork-owned-docs rule before sealing.
if ! git merge --no-commit --no-ff "${UPSTREAM_REMOTE}/main"; then
  if [ -n "$(git ls-files -u)" ]; then
    echo "conflict — aborting for human review:"
    git diff --name-only --diff-filter=U
    git merge --abort
    exit 2
  fi
  exit 1
fi

# 7a. Apply upstream deletions (files upstream removed but the merge couldn't
#     auto-resolve because the fork co-touched them).
mapfile -t UPSTREAM_DELETIONS < <(
  git log --diff-filter=D --name-only --pretty=format: \
    "${FORK_POINT}..${UPSTREAM_REMOTE}/main" | sort -u | sed '/^$/d'
)
for path in "${UPSTREAM_DELETIONS[@]}"; do
  if git ls-files --error-unmatch -- "${path}" >/dev/null 2>&1; then
    keep=false
    for k in "${KEEP_OURS[@]}"; do [ "${path}" = "${k}" ] && keep=true; done
    "${keep}" || git rm -q -- "${path}"
  fi
done

# 7b. Regression check — files upstream has at HEAD that the fork lost.
#     Should be empty; if not, something over-deleted and we fail loud.
LOST=$(
  comm -23 \
    <(git ls-tree -r "${UPSTREAM_REMOTE}/main" --name-only | sort) \
    <(git ls-tree -r HEAD --name-only | sort)
)
if [ -n "${LOST}" ]; then
  echo "ERROR: fork is missing files that exist on upstream/main:"
  echo "${LOST}"
  echo "Investigate before pushing — likely an over-deletion."
  exit 3
fi

# 8. Snapshot upstream README, restore fork-owned docs.
git show "${UPSTREAM_REMOTE}/main:README.md" > "${UPSTREAM_README_SNAPSHOT}"
for keep in "${KEEP_OURS[@]}"; do
  if git cat-file -e "${FORK_REMOTE}/${FORK_BRANCH}:${keep}" 2>/dev/null; then
    git checkout "${FORK_REMOTE}/${FORK_BRANCH}" -- "${keep}"
  fi
done
git add "${UPSTREAM_README_SNAPSHOT}" "${KEEP_OURS[@]}"

# 9. Advance the cursor.
echo "${UPSTREAM_HEAD}" > "${CURSOR_FILE}"
git add "${CURSOR_FILE}"

# 10. Seal the merge.
git commit -m "chore: sync upstream multica-ai/multica ${FORK_SHORT}..${UPSTREAM_SHORT}"

# 11. Local verification.
#     `go build ./...` does NOT compile _test.go cross-file references — use go vet.
( cd server && go vet ./... )

# 12. Push and open the PR with a drift summary baked into the body.
git push -u "${FORK_REMOTE}" "${BRANCH}"

DRIFT_PATHS=(.github/ gitops/ server/migrations/)
DRIFT=$(git diff --stat "${UPSTREAM_REMOTE}/main..HEAD" -- "${DRIFT_PATHS[@]}" || true)
DELETED_LIST=$(printf -- '- %s\n' "${UPSTREAM_DELETIONS[@]}")

gh pr create \
  --base main \
  --head "${BRANCH}" \
  --title "chore: sync upstream multica-ai/multica ${FORK_SHORT}..${UPSTREAM_SHORT}" \
  --body "$(cat <<BODY
Sync upstream multica-ai/multica from \`${FORK_SHORT}\` to \`${UPSTREAM_SHORT}\`.

## Conflict resolution
- Fork-owned docs restored from \`${FORK_REMOTE}/${FORK_BRANCH}\`: ${KEEP_OURS[*]}
- \`${UPSTREAM_README_SNAPSHOT}\` refreshed from upstream \`README.md\`.
- All other upstream-managed paths take upstream.

## Upstream deletions applied
${DELETED_LIST:-_none in this range_}

## Drift in fork-sensitive paths
\`\`\`
${DRIFT:-no diff in these paths}
\`\`\`

## Cursor
\`${CURSOR_FILE}\` advanced to \`${UPSTREAM_HEAD}\`.
BODY
)"
