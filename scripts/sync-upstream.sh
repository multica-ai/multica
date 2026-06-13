#!/usr/bin/env bash
#
# sync-upstream.sh — Safe upstream sync for the Hira fork of Multica.
#
# Pulls the latest multica-ai/multica into an ISOLATED sync branch, keeps the
# fork's Vietnamese localization + Hira branding intact (via .gitattributes
# merge=ours and the Touch-point Registry), then runs the safety nets that catch
# any translation or wiring the merge left behind. It NEVER pushes and NEVER
# touches main directly — you review, run `make check`, then merge yourself.
#
# Read BRANDING.md first: it holds the 3-layer principle, the Touch-point
# Registry (every upstream-owned file this fork edits + how to resolve each),
# and the forbidden list.
#
# Usage:
#   scripts/sync-upstream.sh            # fetch + merge into a sync branch + run safety nets
#   scripts/sync-upstream.sh --full     # also run the full `make check` (Go + E2E) at the end
#   scripts/sync-upstream.sh --no-test  # merge only, skip the safety nets (NOT recommended)
#
set -euo pipefail

UPSTREAM_REMOTE="upstream"
UPSTREAM_URL="https://github.com/multica-ai/multica.git"
UPSTREAM_BRANCH="main"
INTEGRATION_BRANCH="main"   # the fork branch that tracks upstream merges

RUN_FULL=0
RUN_TESTS=1
for arg in "$@"; do
  case "$arg" in
    --full)    RUN_FULL=1 ;;
    --no-test) RUN_TESTS=0 ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "Unknown flag: $arg (try --help)"; exit 2 ;;
  esac
done

# ---------- pretty output ----------
if [ -t 1 ]; then B=$'\033[1m'; G=$'\033[32m'; Y=$'\033[33m'; R=$'\033[31m'; C=$'\033[36m'; N=$'\033[0m'; else B=; G=; Y=; R=; C=; N=; fi
step() { echo "${C}${B}==>${N} ${B}$*${N}"; }
ok()   { echo "  ${G}✓${N} $*"; }
warn() { echo "  ${Y}!${N} $*"; }
die()  { echo "${R}${B}✗ $*${N}" >&2; exit 1; }

cd "$(git rev-parse --show-toplevel)"

# ---------- 0. Preconditions ----------
step "Checking preconditions"

[ -z "$(git status --porcelain)" ] || die "Working tree is dirty. Commit or stash first, then re-run."
ok "Working tree clean"

if ! git remote get-url "$UPSTREAM_REMOTE" >/dev/null 2>&1; then
  warn "No '$UPSTREAM_REMOTE' remote — adding $UPSTREAM_URL"
  git remote add "$UPSTREAM_REMOTE" "$UPSTREAM_URL"
fi
ok "Upstream remote: $(git remote get-url "$UPSTREAM_REMOTE")"

# merge=ours is NOT a built-in git driver; it must be registered per clone or the
# .gitattributes `merge=ours` rules (brand.css, favicon, desktop icons, logos) are
# silently ignored and upstream would overwrite the fork's brand assets.
if [ "$(git config --get merge.ours.driver || true)" != "true" ]; then
  warn "merge=ours driver not configured — enabling it (protects brand assets on merge)"
  git config merge.ours.driver true
fi
ok "merge=ours driver active (brand assets protected)"

# ---------- 1. Fetch upstream ----------
step "Fetching $UPSTREAM_REMOTE"
git fetch --tags "$UPSTREAM_REMOTE"

INCOMING="$(git rev-list --count "$INTEGRATION_BRANCH..$UPSTREAM_REMOTE/$UPSTREAM_BRANCH")"
if [ "$INCOMING" -eq 0 ]; then
  ok "Already up to date with $UPSTREAM_REMOTE/$UPSTREAM_BRANCH — nothing to sync."
  exit 0
fi
ok "$INCOMING new upstream commit(s):"
git --no-pager log --oneline --no-decorate "$INTEGRATION_BRANCH..$UPSTREAM_REMOTE/$UPSTREAM_BRANCH" | head -20 | sed 's/^/    /'

# ---------- 2. Isolated sync branch ----------
STAMP="$(date +%Y%m%d-%H%M%S)"
SYNC_BRANCH="sync/upstream-${STAMP}"
step "Creating isolated sync branch ${SYNC_BRANCH} (off ${INTEGRATION_BRANCH})"
git switch -c "$SYNC_BRANCH" "$INTEGRATION_BRANCH" >/dev/null
ok "On $SYNC_BRANCH — your $INTEGRATION_BRANCH is untouched"

# ---------- 3. Merge ----------
step "Merging $UPSTREAM_REMOTE/$UPSTREAM_BRANCH"
if git merge --no-edit "$UPSTREAM_REMOTE/$UPSTREAM_BRANCH"; then
  ok "Merge clean (no conflicts)"
else
  echo
  warn "Merge has conflicts. Files needing manual resolution:"
  git --no-pager diff --name-only --diff-filter=U | sed 's/^/    /'
  cat <<EOF

  ${B}Resolve each per BRANDING.md → Touch-point Registry${N} (the "Conflict policy"
  column tells you exactly what to re-apply for each file). Reminders:
    • NEVER edit en/zh-Hans/ko/ja strings or tokens.css/base.css — overrides live in brand.css.
    • Keep DEFAULT_LOCALE = "en".
    • If you edit a NEW upstream-owned file, add a row to the Registry in the same commit.

  After resolving:
    git add -A && git merge --continue
    scripts/sync-upstream.sh --no-test   # re-run is not needed; instead run the nets below:
    pnpm install && pnpm typecheck && pnpm test && (cd server && go build ./... )
EOF
  die "Stopping so you can resolve conflicts on $SYNC_BRANCH. The merge is in progress."
fi

# ---------- 4. Safety nets ----------
if [ "$RUN_TESTS" -eq 0 ]; then
  warn "Skipping safety nets (--no-test). Run pnpm typecheck + parity test before merging!"
else
  step "Safety net 0/3: install (lockfile/catalog may have changed)"
  pnpm install
  ok "Dependencies installed"

  step "Safety net 1/3: typecheck (flags any Record<SupportedLocale> map missing a vi entry)"
  pnpm typecheck
  ok "Typecheck passed — every locale-keyed map has its vi entry"

  step "Safety net 2/3: locale parity (flags en keys/namespaces vi is missing)"
  pnpm --filter @multica/views exec vitest run locales/parity.test.ts
  ok "Locale parity passed — vi has every en key (translate any new values it copied verbatim)"

  step "Safety net 3/3: server compiles"
  ( cd server && go build ./... )
  ok "Go server builds"

  if [ "$RUN_FULL" -eq 1 ]; then
    step "Full pipeline: make check (typecheck + unit + Go + E2E)"
    make check
    ok "Full pipeline passed"
  fi
fi

# ---------- 5. Report ----------
step "Sync complete on ${SYNC_BRANCH}"
cat <<EOF

  Upstream changes touching files this fork also edits (review against BRANDING.md):
$(git --no-pager diff --name-only "$INTEGRATION_BRANCH" "$SYNC_BRANCH" | grep -Ff <(awk -F'|' '/^\| / && $2 !~ /File/ {gsub(/^ +| +$/,"",$2); print $2}' BRANDING.md 2>/dev/null) 2>/dev/null | sed 's/^/    /' || true)

  Next steps:
    1. Skim the diff and the Touch-point Registry; re-translate any new strings the
       parity net surfaced (search vi/*.json for English left over from the merge).
    2. ${B}make check${N}    # if you didn't pass --full
    3. git switch ${INTEGRATION_BRANCH} && git merge --no-ff ${SYNC_BRANCH}
    4. git push      # only after you're satisfied

  Nothing was pushed. Your ${INTEGRATION_BRANCH} is unchanged until you merge ${SYNC_BRANCH} into it.
EOF
