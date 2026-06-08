# Claude Version Watcher — Auto-Bump + OAuth Constants Refresh (Plan F.3) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Keep the `multica-runtime-claude` image's Claude version current and the broker's embedded `packaging/oauth-constants.json` in lockstep with whatever Claude binary ships. A daily GitHub Action checks `npm view @anthropic-ai/claude-code version`, and if it differs from what's pinned in this repo, opens a PR that bumps the Dockerfile pin AND refreshes the OAuth constants in one atomic change. The PR is human-reviewed before merge.

**Architecture:** Two small artefacts.

1. **Dockerfile pin.** The runtime image's claude install gets pinned to a specific version (sourced from a build arg `CLAUDE_CODE_VERSION`). Today's Dockerfile installs whatever `npm install -g @anthropic-ai/claude-code` resolves at build time — implicit `latest`, which makes any rebuild a silent version bump. We make the version explicit.
2. **GitHub Actions workflow.** `claude-version-watch.yml` runs daily. It compares the pinned version to npm registry's `latest` tag. If they differ, it: (a) installs the new claude locally in the runner, (b) runs `server/cmd/extract-oauth-constants` (built in Plan F.2 Task 1) against the new binary, (c) writes the new `packaging/oauth-constants.json` and updates the Dockerfile pin, (d) opens a PR titled `chore(claude): bump to <version>` with the diff for human review.

**Tech stack:** GitHub Actions, npm, the extractor from Plan F.2 Task 1.

**Source spec:** the broker plan (`2026-05-29-multica-claude-broker.md`) frames the drift problem this plan solves.

**Builds on:** Plan F.2 Task 1 (the extractor binary). Without that binary, this plan has nothing to invoke. If executing in parallel, this plan must merge AFTER F.2 Task 1 lands.

---

## Key facts established by code reading (do not re-investigate)

- **Package distribution.** `@anthropic-ai/claude-code` is published to the public npm registry. Versions follow semver and are immutable. Listing: `npm view @anthropic-ai/claude-code version` (latest), `npm view @anthropic-ai/claude-code versions --json` (history).
- **Native binary.** Each installed version drops a platform-specific Mach-O / ELF binary at `<npm-root>/@anthropic-ai/claude-code/bin/claude.exe`. The `package.json` in that directory has the `"version"` field we surface in extraction metadata.
- **Today's runtime Dockerfile** at `packaging/docker/runtime/Dockerfile.claude` (Plan C / D) calls `npm install -g @anthropic-ai/claude-code` with no version pin — this is the implicit drift point we're closing.
- **The PR-opening pattern** used by other workflows in this repo: `gh pr create` with `--head` set to the auto-branch name and `--body` rendered from a heredoc. Re-using that style keeps the auto-PR visually consistent with hand-opened ones.

---

## File structure

### Modified by this plan

```
packaging/docker/runtime/Dockerfile.claude    # pin claude version via ARG CLAUDE_CODE_VERSION
packaging/scripts/build-images.sh             # read pin from a single source of truth
packaging/claude-code-version                 # CREATE: plain text file containing the pinned version
```

### Created by this plan

```
.github/workflows/claude-version-watch.yml    # daily cron + workflow_dispatch
scripts/claude-version-bump.sh                # the body of the auto-bump (factored so it's testable locally)
```

---

## Prerequisites

1. Plan F.2 Task 1 merged — the extractor binary exists at `server/cmd/extract-oauth-constants/`.
2. The repo has a `GITHUB_TOKEN` with PR-write permissions (default for `actions/checkout` + `gh pr create`).
3. `npm` available on the runner (default on `ubuntu-latest`).

---

## Task 1: Source-of-truth version pin file

**Files:**
- Create: `packaging/claude-code-version`
- Modify: `packaging/docker/runtime/Dockerfile.claude`
- Modify: `packaging/scripts/build-images.sh`

We use a single plain-text file as the canonical pin. Why a file and not an env var or a Dockerfile constant: the auto-bump PR diff should be exactly one line on this file plus N lines on `oauth-constants.json`, making review trivial. Burying the version inside a Dockerfile ARG is also fine, but bumping it requires sed-on-Dockerfile, which is fiddlier to write defensively than `echo` to a single-purpose file.

- [ ] **Step 1: Write the file**

```bash
echo "2.1.148" > packaging/claude-code-version
```

(Use the currently-installed version from `/opt/homebrew/lib/node_modules/@anthropic-ai/claude-code/package.json`.)

- [ ] **Step 2: Update the Dockerfile to consume it**

Change `packaging/docker/runtime/Dockerfile.claude` so the install line is:

```dockerfile
ARG CLAUDE_CODE_VERSION
RUN npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}"
```

The `ARG` has no default — builds must pass it explicitly. This is intentional: an unset version should fail the build loudly, not silently fall back to `latest`.

- [ ] **Step 3: Update `build-images.sh` to read and pass the version**

In `packaging/scripts/build-images.sh`, locate the `build_runtime` function (Plan C) and add:

```bash
CLAUDE_CODE_VERSION="$(cat "$ROOT/packaging/claude-code-version")"
[ -n "$CLAUDE_CODE_VERSION" ] || { echo "packaging/claude-code-version is empty" >&2; exit 1; }
```

…and pass it via `--build-arg CLAUDE_CODE_VERSION="$CLAUDE_CODE_VERSION"` on the Dockerfile.claude build invocation.

- [ ] **Step 4: Verify a runtime rebuild still works**

```bash
./packaging/scripts/build-images.sh --no-push --tag testpin runtime
docker run --rm ghcr.io/chrissnell/multica-runtime-claude:testpin \
  /usr/local/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe --version 2>&1 \
  | head -2
```

Expected: the printed version matches `packaging/claude-code-version`.

- [ ] **Step 5: Commit**

```bash
git add packaging/claude-code-version packaging/docker/runtime/Dockerfile.claude packaging/scripts/build-images.sh
git commit -m "feat(packaging): pin claude-code version in a single-source-of-truth file"
```

---

## Task 2: The bump script — runs in-CI, also runnable locally

**Files:**
- Create: `scripts/claude-version-bump.sh`

Factoring the bump logic into a script (rather than inlining it in the workflow YAML) means we can run it locally to debug, and the workflow YAML stays focused on glue.

- [ ] **Step 1: Write the script**

```bash
#!/usr/bin/env bash
# Bump the pinned claude-code version and refresh oauth-constants.json
# against the new binary.
#
# Usage:   scripts/claude-version-bump.sh [--check-only]
# Effects: mutates packaging/claude-code-version and packaging/oauth-constants.json
# Output:  prints the new version to stdout on a successful bump;
#          exits 0 silently if no bump is needed; exits non-zero on failure.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
PIN_FILE="$ROOT/packaging/claude-code-version"
CONSTANTS_FILE="$ROOT/packaging/oauth-constants.json"
EXTRACTOR_BIN="$ROOT/server/cmd/extract-oauth-constants"

CHECK_ONLY=0
if [[ "${1:-}" == "--check-only" ]]; then CHECK_ONLY=1; fi

CURRENT="$(cat "$PIN_FILE" | tr -d '[:space:]')"
[ -n "$CURRENT" ] || { echo "no current version pinned in $PIN_FILE" >&2; exit 1; }

LATEST="$(npm view @anthropic-ai/claude-code version 2>/dev/null)"
[ -n "$LATEST" ] || { echo "npm view returned empty" >&2; exit 1; }

if [[ "$CURRENT" == "$LATEST" ]]; then
  echo "claude-code already at latest ($CURRENT)" >&2
  exit 0
fi
echo "claude-code: $CURRENT → $LATEST" >&2

if [[ "$CHECK_ONLY" -eq 1 ]]; then
  exit 0
fi

# Install the new claude locally for extraction
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
npm install --prefix "$WORKDIR" "@anthropic-ai/claude-code@$LATEST" >/dev/null
CLAUDE_BIN="$WORKDIR/node_modules/@anthropic-ai/claude-code/bin/claude.exe"
[ -x "$CLAUDE_BIN" ] || { echo "expected binary not found: $CLAUDE_BIN" >&2; exit 1; }

# Build the extractor if needed
if [[ ! -x "$EXTRACTOR_BIN/bin" ]]; then
  ( cd "$ROOT/server" && go build -o /tmp/extract-oauth-constants "./cmd/extract-oauth-constants" )
fi

# Run extraction — fail-loud on any mismatch
/tmp/extract-oauth-constants \
  -binary "$CLAUDE_BIN" \
  -claude-version "$LATEST" \
  -out "$CONSTANTS_FILE"

# Update the pin
echo "$LATEST" > "$PIN_FILE"

# Print the new version (for the workflow to read)
echo "$LATEST"
```

- [ ] **Step 2: Test locally**

```bash
chmod +x scripts/claude-version-bump.sh
# Force a no-op run to verify the check-only path is silent:
scripts/claude-version-bump.sh --check-only

# Run a fake bump by hand-editing the pin file to an older version:
echo "2.0.0" > packaging/claude-code-version
scripts/claude-version-bump.sh
# Should: install whatever's actually latest, extract constants, update both files.
# Inspect the diffs:
git diff packaging/claude-code-version packaging/oauth-constants.json

# Revert (we don't actually want to land this test bump)
git checkout packaging/claude-code-version packaging/oauth-constants.json
```

- [ ] **Step 3: Commit**

```bash
git add scripts/claude-version-bump.sh
git commit -m "feat(ci): scripts/claude-version-bump.sh — bump pin + refresh OAuth constants"
```

---

## Task 3: GitHub Actions workflow

**Files:**
- Create: `.github/workflows/claude-version-watch.yml`

- [ ] **Step 1: Write the workflow**

```yaml
name: claude-version-watch

on:
  schedule:
    - cron: "0 10 * * *"   # daily 10:00 UTC
  workflow_dispatch:        # also runnable on-demand

permissions:
  contents: write
  pull-requests: write

jobs:
  watch:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0    # need full history to push a branch

      - uses: actions/setup-go@v5
        with:
          go-version-file: server/go.mod

      - uses: actions/setup-node@v4
        with:
          node-version: "22"

      - name: Check whether claude-code has a newer release
        id: check
        run: |
          CURRENT="$(cat packaging/claude-code-version | tr -d '[:space:]')"
          LATEST="$(npm view @anthropic-ai/claude-code version)"
          echo "current=$CURRENT" >> "$GITHUB_OUTPUT"
          echo "latest=$LATEST"   >> "$GITHUB_OUTPUT"
          if [[ "$CURRENT" != "$LATEST" ]]; then
            echo "needs_bump=true" >> "$GITHUB_OUTPUT"
            echo "::notice::claude-code newer release: $CURRENT → $LATEST"
          else
            echo "::notice::claude-code already current at $CURRENT"
          fi

      - name: Bail out if a bump PR is already open
        if: steps.check.outputs.needs_bump == 'true'
        id: pr-check
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          EXISTING="$(gh pr list \
            --search "head:auto/claude-${{ steps.check.outputs.latest }}" \
            --json number --jq '.[0].number' || true)"
          if [[ -n "$EXISTING" ]]; then
            echo "::notice::PR for $LATEST already open: #$EXISTING"
            echo "skip=true" >> "$GITHUB_OUTPUT"
          fi

      - name: Run the bump
        if: steps.check.outputs.needs_bump == 'true' && steps.pr-check.outputs.skip != 'true'
        run: |
          scripts/claude-version-bump.sh

      - name: Open PR
        if: steps.check.outputs.needs_bump == 'true' && steps.pr-check.outputs.skip != 'true'
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          BRANCH="auto/claude-${{ steps.check.outputs.latest }}"
          git config user.name  "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git checkout -b "$BRANCH"
          git add packaging/claude-code-version packaging/oauth-constants.json
          git commit -m "chore(claude): bump to ${{ steps.check.outputs.latest }}

          Automated by .github/workflows/claude-version-watch.yml.

          OAuth constants re-extracted from the new claude binary by
          server/cmd/extract-oauth-constants. Review the
          packaging/oauth-constants.json diff carefully — any change
          to client_id, version_header, or scopes indicates Anthropic
          rotated something and the broker needs the new values."
          git push origin "$BRANCH"

          gh pr create \
            --title "chore(claude): bump to ${{ steps.check.outputs.latest }}" \
            --head  "$BRANCH" \
            --base  main \
            --body  "$(cat <<EOF
          Automated bump from \`.github/workflows/claude-version-watch.yml\`.

          - Previous: \`${{ steps.check.outputs.current }}\`
          - New:      \`${{ steps.check.outputs.latest }}\`

          ### What to review

          1. **\`packaging/oauth-constants.json\`** — re-extracted from the new claude binary.
             A diff on \`client_id\`, \`version_header\`, or \`scopes\` means Anthropic rotated
             something. Investigate before merging.
          2. **\`packaging/claude-code-version\`** — just the version string.
          3. **Anthropic changelog** if visible — does the new version mention anything
             about OAuth, MCP, or agent execution shape? Anything material there may
             warrant a manual smoke test in the cluster.

          ### After merge

          - The runtime image at the next \`./packaging/scripts/build-images.sh runtime\`
            invocation will use the new claude.
          - The broker image at the next build will embed the new \`oauth-constants.json\`.
          - \`make check\` / CI on this PR confirms tests still pass with the new constants.
          EOF
          )"
```

- [ ] **Step 2: Test the workflow with `workflow_dispatch`**

```bash
gh workflow run claude-version-watch.yml --ref <your-feature-branch>
gh run watch --exit-status
```

If the current pin matches latest, the workflow should print `claude-code already current` and exit clean. To validate the bump path, force a downgrade in `packaging/claude-code-version`, push, and run again.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/claude-version-watch.yml
git commit -m "feat(ci): daily claude-version watcher + auto-bump PR"
```

---

## Task 4: Operator documentation

**Files:**
- Modify: `packaging/README.md`

Add a short section explaining:
- The pin file (`packaging/claude-code-version`) and how to bump manually.
- The watcher workflow and the daily-PR cadence.
- What to do when a PR's `oauth-constants.json` diff shows a `client_id` or `version_header` change (don't merge until the broker's runtime metric `multica_claude_broker_refresh_failures_total{reason="permanent"}` has been observed to stay at zero against a test build with the new constants).
- How to disable the watcher temporarily (delete the workflow's schedule, or `gh workflow disable claude-version-watch.yml`).

- [ ] **Commit**

```bash
git add packaging/README.md
git commit -m "docs(packaging): claude-version-watch operator guide"
```

---

## Task 5: Final regression

- [ ] `make check` (or whatever your local equivalent is) passes.
- [ ] `gh workflow run claude-version-watch.yml` against your branch executes successfully.
- [ ] A force-bump (downgrade the pin to an older version, then run the workflow) successfully opens a PR with both files updated.

---

## What's next (deferred)

- **Auto-merge for patch-level bumps.** If the watcher's PR has zero changes to `client_id`, `version_header`, and `scopes` (i.e., the constants are bit-identical), it could safely auto-merge after CI passes. v1 leaves this for human review every time; the noise is low (~1 PR/week).
- **Test the new claude in a sandbox before opening the PR.** A workflow step that spins up a minimal kind cluster, runs the broker + a worker pod with the new constants, and verifies a single refresh succeeds. Adds ~5 minutes to the workflow but catches Anthropic-side breaking changes before they reach a human reviewer.
- **Notification to Slack / email** when a PR contains a non-trivial constants diff (anything beyond `claude_version`). For a single-operator deployment this is overkill; gets useful if a team starts depending on the broker.

## What this enables

- The runtime image's claude version stops drifting silently.
- The broker's embedded constants are always pinned to a specific claude version that you can trace through the PR history.
- A breaking change from Anthropic surfaces as a PR diff before it reaches production, with the failure mode being "the PR doesn't merge" (visible) rather than "the cluster goes dark at 3am" (invisible).
