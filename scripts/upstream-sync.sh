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

# 7a. Enforce upstream authority on every non-fork-owned path.
#
#     The fork's ONLY intentional divergences are the files it changed since the
#     fork-point (FORK_POINT..origin/main). Every other path must equal
#     upstream/main exactly. Resetting them here catches two failure modes the
#     old "replay the D-log" deletion sweep silently missed:
#       * upstream deletions the merge kept — renames and merge-commit deletes
#         don't reliably surface in `git log --diff-filter=D` over the range; and
#       * silent line-level mis-merges — because the git merge-base predates the
#         cursor, a file can auto-merge WITHOUT a conflict into "fork-stale +
#         upstream" combined content (duplicated blocks, imports of a module
#         upstream deleted, ...). These never appear in the conflict list.
FORK_OWNED=$(git diff --name-only "${FORK_POINT}" "${FORK_REMOTE}/${FORK_BRANCH}" | sort -u)

RESET_TO_UPSTREAM=()
UPSTREAM_DELETIONS=()
while IFS= read -r path; do
  [ -z "${path}" ] && continue
  # Intentional fork divergences (incl. KEEP_OURS docs) are left untouched.
  if printf '%s\n' "${FORK_OWNED}" | grep -qxF -- "${path}"; then
    continue
  fi
  keep=false
  for k in "${KEEP_OURS[@]}"; do [ "${path}" = "${k}" ] && keep=true; done
  if "${keep}"; then
    continue
  fi
  if git cat-file -e "${UPSTREAM_REMOTE}/main:${path}" 2>/dev/null; then
    git checkout "${UPSTREAM_REMOTE}/main" -- "${path}"
    git add -- "${path}"
    RESET_TO_UPSTREAM+=("${path}")
  else
    git rm -q -- "${path}"
    UPSTREAM_DELETIONS+=("${path}")
  fi
done < <(git diff --cached --name-only "${UPSTREAM_REMOTE}/main" | sort -u)

# 7b. Invariant guard — after enforcement no non-fork-owned path may differ from
#     upstream/main. Compare against the STAGED INDEX, not HEAD: `git merge
#     --no-commit` does NOT advance HEAD, so a HEAD comparison reads the
#     pre-merge tree and false-positives on every real sync (missing every file
#     upstream added since the fork-point).
STRAY=$(comm -23 \
  <(git diff --cached --name-only "${UPSTREAM_REMOTE}/main" | sort -u) \
  <(printf '%s\n' "${FORK_OWNED}"))
if [ -n "${STRAY}" ]; then
  echo "ERROR: non-fork-owned paths still diverge from upstream/main after enforcement:"
  echo "${STRAY}"
  echo "Investigate before pushing — enforcement or fork-owned detection is wrong."
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

DELETED_LIST=""
if [ "${#UPSTREAM_DELETIONS[@]}" -gt 0 ]; then
  DELETED_LIST=$(printf -- '- %s\n' "${UPSTREAM_DELETIONS[@]}")
fi
RESET_LIST=""
if [ "${#RESET_TO_UPSTREAM[@]}" -gt 0 ]; then
  RESET_LIST=$(printf -- '- %s\n' "${RESET_TO_UPSTREAM[@]}")
fi

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

## Non-fork-owned files reset to upstream (silent mis-merges corrected)
${RESET_LIST:-_none in this range_}

## Drift in fork-sensitive paths
\`\`\`
${DRIFT:-no diff in these paths}
\`\`\`

## Cursor
\`${CURSOR_FILE}\` advanced to \`${UPSTREAM_HEAD}\`.
BODY
)"
