#!/usr/bin/env bash
# Maintain g2crowd/agentfarm in sync with upstream multica-ai/multica.
# Idempotent end-to-end: safe to re-run when there is nothing new.
#
# The sync target is an upstream RELEASE TAG, never upstream/main HEAD. Our
# deployment is release-versioned, so the fork must always sit exactly on a tag
# boundary — that is what makes "the fork is at v0.4.12" a checkable statement
# and the hop diff reproducible.
#
# Override the target with UPSTREAM_TAG=v0.4.12 to pin a specific release
# (used to land the fork on a tag boundary the first time, and to walk one
# release at a time when a hop is too large to review).
#
# Optional JIRA_REF=AIPLAT-123 stamps that key into the PR title. The org-level
# required check `jira-ref-check-and-description` ("Rule PR Title semantics")
# demands a bracketed ref there and fails without one, so the title carries
# `[NO JIRA]` when JIRA_REF is unset — see step 14b.
set -euo pipefail

# ── Git ownership trust ───────────────────────────────────────────────────────
# The runtime creates the checkout under a different uid (50012) than the one this
# script runs as (1000), so git refuses the repository with `detected dubious
# ownership` and exits 128 on step 1 — the first git command in the file, before
# any of the guards below can report anything. That is ANK-49.
#
# Duplicated from sync-tick.sh rather than sourced: this script is also a
# standalone entry point (a human runs it directly to walk one release at a time),
# so it cannot depend on the tick having exported anything. When the tick DOES
# invoke it the exported vars are simply inherited, and re-running the helper is
# idempotent in effect — it adds a second identical safe.directory entry, which
# git treats the same as one.
#
# See sync-tick.sh for why this is env-scoped (`GIT_CONFIG_*`) instead of
# `git config --global --add safe.directory`, and why the value is `*` rather than
# this checkout's path.
trust_git_checkouts() {
  local n="${GIT_CONFIG_COUNT:-0}"
  case "${n}" in ''|*[!0-9]*) n=0 ;; esac
  export "GIT_CONFIG_KEY_${n}=safe.directory"
  export "GIT_CONFIG_VALUE_${n}=*"
  export GIT_CONFIG_COUNT=$(( n + 1 ))
}
trust_git_checkouts

UPSTREAM_URL="https://github.com/multica-ai/multica.git"
UPSTREAM_REMOTE="upstream"
FORK_REMOTE="origin"
FORK_BRANCH="main"

# Local ref the target tag is fetched into. Deliberately NOT under refs/tags/:
# the fork publishes its own CLI release tags using the same vX.Y.Z scheme (see
# CLAUDE.md "CLI Release"), so upstream tags must never be resolved from — or
# written into — the local tag namespace.
UPSTREAM_REF="refs/upstream-sync/target"

# Files we never let upstream overwrite. README.md gets snapshotted into
# UPSTREAM_README.md so reviewers can still see upstream's docs.
KEEP_OURS=(CLAUDE.md README.md)
UPSTREAM_README_SNAPSHOT="UPSTREAM_README.md"

# Cursor file in the fork repo. Records the upstream release the fork currently
# sits on, as `tag=` + the SHA that tag pointed at, so the next sync survives PR
# squash-merge (which severs git's natural ancestry).
CURSOR_FILE=".upstream-sync-cursor"

# The baked CLI version. `multica version` reports it, and it is the ONLY semver
# marker anywhere in the deployment, so it must track the synced tag — see 8b.
BAKEFILE="docker/agent-runtime-base/docker-bake.hcl"

# 1. Ensure remote (idempotent).
if ! git remote get-url "${UPSTREAM_REMOTE}" >/dev/null 2>&1; then
  git remote add "${UPSTREAM_REMOTE}" "${UPSTREAM_URL}"
fi

# 2. Refuse to run on a dirty tree.
#    git's exit status is checked separately from its output: a git that cannot
#    read the repository at all (dubious ownership, corrupt objects) complains on
#    stderr and prints NOTHING on stdout, which is indistinguishable from a clean
#    tree — the old one-line test read that as "clean" and synced on against a
#    repository it could not trust.
if ! DIRTY=$(git status --porcelain); then
  echo "cannot read the working tree — git status failed; the checkout is unusable"
  exit 1
fi
if [ -n "${DIRTY}" ]; then
  # Print the evidence. A checkout cloned from a blobless cache without its
  # promisor config reports every tracked file as deleted rather than failing, so
  # a bare "working tree dirty" is not diagnosable once the checkout is pruned —
  # which is exactly how ANK-51 died with nothing left to inspect.
  echo "working tree dirty:"
  printf '%s\n' "${DIRTY}" | head -20
  DIRTY_COUNT=$(printf '%s\n' "${DIRTY}" | wc -l | tr -d ' ')
  [ "${DIRTY_COUNT}" -le 20 ] || echo "... and $((DIRTY_COUNT - 20)) more"
  exit 1
fi

# 3. Resolve the sync target.
#    Tags are listed from the upstream REMOTE rather than `git tag -l` so the
#    fork's own release tags cannot be mistaken for upstream's. The exact-semver
#    filter drops prereleases (v1.2.3-rc1); draft releases have no tag ref at
#    all, so they are excluded by construction.
if [ -n "${UPSTREAM_TAG:-}" ]; then
  TARGET_TAG="${UPSTREAM_TAG}"
else
  TARGET_TAG=$(git ls-remote --tags --refs "${UPSTREAM_REMOTE}" 'v*' \
    | sed 's#.*refs/tags/##' \
    | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
    | sort -V \
    | tail -1)
fi
[ -n "${TARGET_TAG}" ] || { echo "could not resolve an upstream release tag"; exit 1; }

# 4. Fetch BOTH sides — never compute against stale refs.
#    upstream `main` is fetched only so the cursor commit is guaranteed to exist
#    locally: squash-merge severs it from the fork's ancestry, and it need not be
#    reachable from the target tag.
git fetch "${FORK_REMOTE}" "${FORK_BRANCH}"
git fetch --no-tags "${UPSTREAM_REMOTE}" main
git fetch --no-tags --force "${UPSTREAM_REMOTE}" "refs/tags/${TARGET_TAG}:${UPSTREAM_REF}"

UPSTREAM_HEAD=$(git rev-parse "${UPSTREAM_REF}^{commit}")
UPSTREAM_SHORT=$(git rev-parse --short=7 "${UPSTREAM_HEAD}")

# 5. Resolve fork-point. Prefer the explicit cursor (survives squash-merge);
#    fall back to git merge-base only on first run (no cursor present).
#    `tag=` is empty only before the fork's first tag-aligned sync.
FROM_TAG=""
FORK_POINT=""
if [ -f "${CURSOR_FILE}" ]; then
  FROM_TAG=$(sed -n 's/^tag=//p' "${CURSOR_FILE}" | tr -d '[:space:]')
  FORK_POINT=$(sed -n 's/^sha=//p' "${CURSOR_FILE}" | tr -d '[:space:]')
  # Defensive peel: a cursor written before this script peeled at write-time
  # (see UPSTREAM_HEAD above) may still hold a bare tag-object SHA, which
  # `git replace --graft` below rejects with "Not a valid commit name".
  if [ -n "${FORK_POINT}" ]; then
    FORK_POINT=$(git rev-parse "${FORK_POINT}^{commit}")
  fi
fi
if [ -z "${FORK_POINT}" ]; then
  FORK_POINT=$(git merge-base "${FORK_REMOTE}/${FORK_BRANCH}" "${UPSTREAM_REF}")
fi
FORK_SHORT=$(git rev-parse --short=7 "${FORK_POINT}")

# Label the hop by tag where we have one, else by the SHA the fork is stranded on.
FROM_LABEL="${FROM_TAG:-${FORK_SHORT}}"

# 6. Nothing to sync — exit clean.
if [ "${FORK_POINT}" = "${UPSTREAM_HEAD}" ]; then
  echo "in sync at ${TARGET_TAG} (${UPSTREAM_SHORT}); nothing to do"
  exit 0
fi

BRANCH="upstream-sync/${FROM_LABEL}-to-${TARGET_TAG}"
# -B, not -b: a prior attempt at this exact hop may have created this branch
# locally and then died before pushing it (crash, killed tick, ANK-117's
# "branch already exists" block). stage_syncing() in sync-tick.sh already
# refuses to get here if a PUSHED branch for this hop has no open PR
# (stale_sync_branch), so any local branch of this name left to find here is
# guaranteed unpushed and safe to rebuild from scratch — which is exactly what
# every other step below already does from fetched refs.
git checkout -B "${BRANCH}" "${FORK_REMOTE}/${FORK_BRANCH}"

# 6b. Restore upstream ancestry so the merge base is the release we last synced.
#
#     Sync PRs are squash-merged, which severs the fork from the upstream commit
#     it was synced to. `git merge-base` then falls all the way back to the
#     original fork point, and the merge re-litigates every upstream change since
#     — producing hundreds of spurious conflicts in files nobody here has ever
#     touched. Measured on the v0.4.12 hop: 127 conflicting files without this,
#     390 when targeting upstream/main HEAD, and 0 with it.
#
#     A temporary replace-ref gives the fork tip the cursor commit as an extra
#     parent, which makes merge-base exactly the cursor. It is local-only
#     (refs/replace/* is not pushed), it affects only object traversal, and the
#     sealed merge commit still records its real parents — verify with
#     `git log --graph` after a run. Removed on every exit path by the trap.
#
#     This is what makes an unattended sync viable: without it every run ends in
#     the conflict path and needs a human.
GRAFT_TARGET=""
cleanup_graft() {
  if [ -n "${GRAFT_TARGET}" ]; then
    git replace -d "${GRAFT_TARGET}" >/dev/null 2>&1 || true
  fi
  return 0
}
trap cleanup_graft EXIT

if ! git merge-base --is-ancestor "${FORK_POINT}" "${FORK_REMOTE}/${FORK_BRANCH}"; then
  FORK_TIP=$(git rev-parse "${FORK_REMOTE}/${FORK_BRANCH}")
  mapfile -t FORK_TIP_PARENTS < <(git rev-parse "${FORK_TIP}^@")
  git replace --graft "${FORK_TIP}" "${FORK_TIP_PARENTS[@]}" "${FORK_POINT}"
  GRAFT_TARGET="${FORK_TIP}"
  echo "grafted ${FORK_SHORT} as an extra parent of ${FORK_REMOTE}/${FORK_BRANCH} so merge-base is the last synced release (temporary, local-only)"
fi

# 7. Merge with --no-commit so we can apply the fork-owned-docs rule before sealing.
if ! git merge --no-commit --no-ff "${UPSTREAM_REF}"; then
  if [ -z "$(git ls-files -u)" ]; then
    # Merge failed for a non-conflict reason (dirty tree, bad ref, ...).
    exit 1
  fi
  # Every KEEP_OURS path is fork-owned by policy, so a conflict on any of them is
  # expected — not just README.md. Resolve those now with the fork's copy; step 9
  # restores KEEP_OURS again (and snapshots upstream's README separately) before
  # the merge is sealed. Any conflict outside KEEP_OURS still requires human
  # review. This used to check README.md alone, which meant a hop where upstream
  # also touched CLAUDE.md (fork-owned too) aborted for a human even though the
  # policy already had an answer for it.
  CONFLICTS=$(git diff --name-only --diff-filter=U | sort -u)
  UNEXPECTED_CONFLICTS="${CONFLICTS}"
  for keep in "${KEEP_OURS[@]}"; do
    UNEXPECTED_CONFLICTS=$(printf '%s\n' "${UNEXPECTED_CONFLICTS}" | grep -vxF -- "${keep}" || true)
  done
  if [ -n "${UNEXPECTED_CONFLICTS}" ]; then
    echo "conflict — aborting for human review:"
    printf '%s\n' "${CONFLICTS}"
    git merge --abort
    exit 2
  fi
  for keep in "${KEEP_OURS[@]}"; do
    if printf '%s\n' "${CONFLICTS}" | grep -qxF -- "${keep}"; then
      git checkout "${FORK_REMOTE}/${FORK_BRANCH}" -- "${keep}"
      git add -- "${keep}"
      echo "resolved fork-owned ${keep}"
    fi
  done
  # Nothing may still be unmerged once the policy resolution has run.
  if [ -n "$(git ls-files -u)" ]; then
    echo "unresolved paths remain after applying the fork-owned-docs rule:"
    git diff --name-only --diff-filter=U
    git merge --abort
    exit 1
  fi
fi

# 8a. Enforce upstream authority on every non-fork-owned path.
#
#     The fork's ONLY intentional divergences are the files it changed since the
#     fork-point (FORK_POINT..origin/main). Every other path must equal the
#     target tag exactly. Resetting them here catches two failure modes the
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
  if git cat-file -e "${UPSTREAM_REF}:${path}" 2>/dev/null; then
    git checkout "${UPSTREAM_REF}" -- "${path}"
    git add -- "${path}"
    RESET_TO_UPSTREAM+=("${path}")
  else
    git rm -q -- "${path}"
    UPSTREAM_DELETIONS+=("${path}")
  fi
done < <(git diff --cached --name-only "${UPSTREAM_REF}" | sort -u)

# 8b. Invariant guard — after enforcement no non-fork-owned path may differ from
#     the target tag. Compare against the STAGED INDEX, not HEAD: `git merge
#     --no-commit` does NOT advance HEAD, so a HEAD comparison reads the
#     pre-merge tree and false-positives on every real sync (missing every file
#     upstream added since the fork-point).
STRAY=$(comm -23 \
  <(git diff --cached --name-only "${UPSTREAM_REF}" | sort -u) \
  <(printf '%s\n' "${FORK_OWNED}"))
if [ -n "${STRAY}" ]; then
  echo "ERROR: non-fork-owned paths still diverge from ${TARGET_TAG} after enforcement:"
  echo "${STRAY}"
  echo "Investigate before pushing — enforcement or fork-owned detection is wrong."
  exit 3
fi

# 8c. Deletion guard — the sync may only delete files upstream itself deleted.
#
#     8b is blind to deletions. It reports paths that still DIFFER from the
#     target tag, and a file the sync wrongly deleted matches the tag by absence
#     — so it reads as compliant. That blind spot shipped a sync with the
#     production backend Deployment (gitops/base/deployment-backend.yaml)
#     removed: the sync exited 0, and only the Kustomize Validation job on the
#     PR caught it, several steps downstream of where it went wrong.
#
#     The invariant is deliberately NOT expressed in terms of FORK_OWNED. That
#     list is exactly what 8a's deletion branch consults, so reusing it here
#     would re-derive the guard from the thing being guarded and agree with any
#     bug in it. Independent formulation: a deletion is legitimate only when the
#     path existed on the upstream side at the fork-point and is gone at the
#     target tag — i.e. upstream deleted something it owned. Anything the fork
#     itself introduced never existed at the fork-point, so it can never qualify.
#
#     A path upstream deleted that the fork had also modified surfaces earlier as
#     a modify/delete conflict (exit 2), so it never reaches here.
#
#     KEEP_OURS is exempt: step 9 restores those from the fork branch below.
LOST=()
while IFS= read -r path; do
  [ -z "${path}" ] && continue
  keep=false
  for k in "${KEEP_OURS[@]}"; do [ "${path}" = "${k}" ] && keep=true; done
  if "${keep}"; then
    continue
  fi
  if git cat-file -e "${FORK_POINT}:${path}" 2>/dev/null \
    && ! git cat-file -e "${UPSTREAM_REF}:${path}" 2>/dev/null; then
    continue
  fi
  LOST+=("${path}")
done < <(git diff --cached --no-renames --diff-filter=D --name-only \
  "${FORK_REMOTE}/${FORK_BRANCH}" | sort -u)
if [ "${#LOST[@]}" -gt 0 ]; then
  echo "ERROR: the sync deleted path(s) ${TARGET_TAG} never owned:"
  printf -- '  %s\n' "${LOST[@]}"
  echo "Investigate before pushing — enforcement or fork-owned detection is wrong."
  exit 3
fi

# 9. Snapshot upstream README, restore fork-owned docs.
git show "${UPSTREAM_REF}:README.md" > "${UPSTREAM_README_SNAPSHOT}"
for keep in "${KEEP_OURS[@]}"; do
  if git cat-file -e "${FORK_REMOTE}/${FORK_BRANCH}:${keep}" 2>/dev/null; then
    git checkout "${FORK_REMOTE}/${FORK_BRANCH}" -- "${keep}"
  fi
done
git add "${UPSTREAM_README_SNAPSHOT}" "${KEEP_OURS[@]}"

# 10. Keep the baked CLI version in step with the tag we just synced to.
#
#     `multica version` is a purely local read of compile-time ldflags fed from
#     MULTICA_VERSION in the bakefile, and CI passes no override — the bakefile
#     default is what ships. It is also the only semver marker in the whole
#     deployment (no server version endpoint; backend/web images are SHA-tagged
#     only), which makes it the signal the sync pipeline reads to decide whether
#     a release is already deployed. Left hand-maintained, a missed bump makes a
#     freshly rolled pod claim the old version indefinitely, so the bump belongs
#     in the sync commit rather than in a human's discipline.
#
#     Runs after the invariant guard so the guard only ever reasons about merge
#     output, never about an edit this script made.
TARGET_SEMVER="${TARGET_TAG#v}"
grep -qE '^variable "MULTICA_VERSION"' "${BAKEFILE}" \
  || { echo "ERROR: MULTICA_VERSION not found in ${BAKEFILE}"; exit 1; }

# Written via a temp file rather than `sed -i`, whose syntax differs between GNU
# and BSD sed — this script also gets run by hand on macOS.
BAKEFILE_TMP=$(mktemp)
sed -E "s#^(variable \"MULTICA_VERSION\"[[:space:]]*\{[[:space:]]*default[[:space:]]*=[[:space:]]*\")[^\"]*(\")#\1${TARGET_SEMVER}\2#" \
  "${BAKEFILE}" > "${BAKEFILE_TMP}"
mv "${BAKEFILE_TMP}" "${BAKEFILE}"

grep -qE "^variable \"MULTICA_VERSION\"[[:space:]]*\{[[:space:]]*default[[:space:]]*=[[:space:]]*\"${TARGET_SEMVER}\"" "${BAKEFILE}" \
  || { echo "ERROR: failed to set MULTICA_VERSION to ${TARGET_SEMVER} in ${BAKEFILE}"; exit 1; }
git add -- "${BAKEFILE}"

# 11. Advance the cursor to the tag we synced to.
{
  echo "# Upstream release the fork currently sits on. Written by scripts/upstream-sync.sh."
  printf 'tag=%s\nsha=%s\n' "${TARGET_TAG}" "${UPSTREAM_HEAD}"
} > "${CURSOR_FILE}"
git add "${CURSOR_FILE}"

# 11b. Resolve the JIRA reference the PR title must carry.
#
#      `jira-ref-check-and-description` is an org-injected required check on this
#      repo. It reads the PR TITLE, and unless the title carries a bracketed
#      exclusion tag it resolves the key against Atlassian and fails when it
#      finds none. Every tick-generated sync PR failed that gate until a human
#      edited its title by hand — the loop advertised itself as autonomous up to
#      the merge while in practice needing a manual edit at exactly that point,
#      because the check does not gate dev.yml and the pipeline still went green
#      in dev (observed on PR #248: `failure` at 04:46:08Z, `success` at
#      06:56:41Z only after the title was edited).
#
#      `[NO JIRA]` is the fallback rather than the intent: sync-tick.sh passes
#      the per-hop AIPLAT key as JIRA_REF, and JIRA is allowed to fail there
#      without blocking a sync, so the default has to stand on its own.
normalize_jira_ref() {
  local raw="${1:-}"
  # Tolerate `[AIPLAT-1]`, ` AIPLAT-1 ` and multi-line input: the caller may be
  # piping through metadata this script does not control.
  raw="$(printf '%s' "${raw}" | tr -d '[]' | tr '\n\t' '  ')"
  raw="$(printf '%s' "${raw}" | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//; s/[[:space:]]+/ /g')"
  if [ -z "${raw}" ]; then
    printf 'NO JIRA'
  else
    printf '%s' "${raw}"
  fi
}

SYNC_SUBJECT="chore: sync upstream multica-ai/multica ${FROM_LABEL}..${TARGET_TAG}"
PR_TITLE="${SYNC_SUBJECT} [$(normalize_jira_ref "${JIRA_REF:-}")]"

# 12. Seal the merge. The commit message carries the same ref as the PR title, so
#     a rebase-merge lands a compliant subject too (squash-merge takes the PR
#     title regardless).
git commit -m "${PR_TITLE}"

# 13. Local verification.
#     `go build ./...` does NOT compile _test.go cross-file references — use go vet.
#
#     Skipped when there is no Go toolchain, because the agent runtime that runs
#     this script unattended has none: docker/agent-runtime-base/Dockerfile keeps
#     Go in the `multica-builder` stage and ships a `debian:bookworm-slim`
#     runtime. Making a missing toolchain fatal would abort every autonomous sync
#     *after* the merge commit and *before* the push — the worst place to stop.
#     `ci.yml` runs the Go checks on the PR, and the dev deploy gates on CI, so
#     the merged tree is still verified before anything is deployed. The skip is
#     announced here and in the PR body so a green PR is never mistaken for a
#     locally vetted one.
VET_NOTE=""
if command -v go >/dev/null 2>&1; then
  ( cd server && go vet ./... )
else
  VET_NOTE="No Go toolchain on this host — \`go vet ./...\` was skipped; CI is the gate."
  echo "NOTE: ${VET_NOTE}"
fi

# 14. Push and open the PR with a drift summary baked into the body.
git push -u "${FORK_REMOTE}" "${BRANCH}"

DRIFT_PATHS=(.github/ gitops/ server/migrations/)
DRIFT=$(git diff --stat "${UPSTREAM_REF}..HEAD" -- "${DRIFT_PATHS[@]}" || true)

DELETED_LIST=""
if [ "${#UPSTREAM_DELETIONS[@]}" -gt 0 ]; then
  DELETED_LIST=$(printf -- '- %s\n' "${UPSTREAM_DELETIONS[@]}")
fi
RESET_LIST=""
if [ "${#RESET_TO_UPSTREAM[@]}" -gt 0 ]; then
  RESET_LIST=$(printf -- '- %s\n' "${RESET_TO_UPSTREAM[@]}")
fi

# The PR is opened through the REST API rather than `gh pr create`, which uses
# the GraphQL `createPullRequest` mutation. The GitHub App the agent runtime
# authenticates as cannot reach GraphQL mutations — `gh pr create`, `gh pr
# comment` and `gh pr edit` all fail with "Resource not accessible by
# integration" — while the equivalent REST endpoints work. GraphQL *reads*
# (`gh pr view --json`, `gh pr checks`) are fine, but always pass them `--repo`:
# without it `gh` infers the repo from the checkout and can answer about a
# different PR entirely.
if [ -z "${FORK_SLUG:-}" ]; then
  FORK_SLUG=$(git remote get-url "${FORK_REMOTE}" \
    | sed -E 's#^(git@|https://|ssh://git@)github\.com[:/]##; s#\.git$##')
fi
# Fail loudly rather than POSTing to a nonsense path if the remote is not a
# recognisable github.com URL.
if ! printf '%s' "${FORK_SLUG}" | grep -qE '^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$'; then
  echo "ERROR: could not derive owner/repo from ${FORK_REMOTE} (got '${FORK_SLUG}')."
  echo "The branch is pushed; open the PR by hand or re-run with FORK_SLUG=owner/repo."
  exit 1
fi

gh api -X POST "repos/${FORK_SLUG}/pulls" \
  --jq '.html_url' \
  -f base=main \
  -f head="${BRANCH}" \
  -f title="${PR_TITLE}" \
  -f body="$(cat <<BODY
Sync upstream multica-ai/multica from \`${FROM_LABEL}\` to release \`${TARGET_TAG}\` (\`${UPSTREAM_SHORT}\`).

## Conflict resolution
- Fork-owned docs restored from \`${FORK_REMOTE}/${FORK_BRANCH}\`: ${KEEP_OURS[*]}
- \`${UPSTREAM_README_SNAPSHOT}\` refreshed from upstream \`README.md\`.
- All other upstream-managed paths take \`${TARGET_TAG}\`.

## Upstream deletions applied
${DELETED_LIST:-_none in this range_}

## Non-fork-owned files reset to upstream (silent mis-merges corrected)
${RESET_LIST:-_none in this range_}

## Drift in fork-sensitive paths
\`\`\`
${DRIFT:-no diff in these paths}
\`\`\`

## Version markers
- \`${CURSOR_FILE}\` advanced to \`tag=${TARGET_TAG}\` / \`sha=${UPSTREAM_HEAD}\`.
- \`MULTICA_VERSION\` in \`${BAKEFILE}\` set to \`${TARGET_SEMVER}\`, so \`multica version\` reports the synced release once the agentrunner pod rolls.

## Local verification
${VET_NOTE:-\`go vet ./...\` passed in \`server/\` before push.}
BODY
)"
