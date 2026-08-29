# Team Epsilon Branch and Worktree Retirement Plan

Audit date: 2026-08-29

Primary archaeology snapshot: `2026-08-29T21:46:19Z`

Scope: all locally named Multica branches and refs, live `origin` and `fork` heads and tags, every registered worktree, and the three worktrees named in the dispatch. Team Epsilon made no fetch, checkout, stash, reset, clean, branch deletion, worktree removal, or push during archaeology. The only planned repository change is this report commit.

## Executive disposition

No audited branch or worktree is safe to delete immediately.

The common repository is active, the primary checkout owns the `.git` directory for eight linked worktrees, two historical feature lines remain unmerged, one linked worktree has an unstaged tracked change, and `refs/stash` is the only ref preserving a 20-file Phase 3 snapshot. The safe order is:

1. Quiesce all Multica workers and repeat the ref and status snapshot.
2. Create and verify a full bundle, archival tags, the replacement-runtime patch, the Phase 3 stash patch and untracked-file archive, and a secure copy of the ignored Phase 3 environment file.
3. Record which old operational and replacement-provider behaviors were ported to `fork/main`, or explicitly rejected after review.
4. Merge or otherwise accept the active reconciliation reports.
5. Remove linked worktrees without `--force`, prune only branches that pass the stated gates, and quarantine the primary checkout last.

## Audit conventions

- `+N/-M vs origin/main` means commits reachable only from the row ref, followed by commits reachable only from `origin/main`.
- `Merged O/F` is exact ancestor status into `origin/main` and `fork/main` at the snapshot.
- A branch can have zero tip-exclusive commits while still protecting commits absent from both remote defaults. Alias branches and ancestor branches are called out separately below.
- Standard dirty state includes staged, unstaged, and nonignored untracked files. Ignored runtime artifacts are listed separately where preservation matters.
- The report branch itself was at `240d4b9b...` during archaeology. Its final one-file documentation commit is intentionally outside the snapshot and is reported to the coordinator separately.

## Repository and ref universe

The shared common Git directory is:

```text
/Users/bradstrawbridge/.agents/workspaces/_archive/multica/.git
```

The repository is non-bare, non-shallow, and uses SHA-1 objects. Its configured remotes contain no embedded credentials:

```text
origin  https://github.com/multica-ai/multica.git
fork    https://github.com/CCRBrad/multica.git
```

At the snapshot, the local ref universe contained:

| Namespace | Count | Finding |
| --- | ---: | --- |
| `refs/heads/*` | 13 | All local branches are inventoried below |
| `refs/remotes/origin/*` | 2,369 including `origin/HEAD` | 2,368 live upstream heads, exact live/local parity at verification |
| `refs/remotes/fork/*` | 3 including `fork/HEAD` | Two live fork heads, exact live/local parity at verification |
| `refs/tags/*` | 157 | Exact tag-name and object parity with live origin after excluding peeled `^{}` records |
| `refs/stash` | 1 | Unique Phase 3 snapshot at `0f5927be...` |
| replace refs | 0 | None |
| notes | 0 | None |

No existing tag points at any scoped custom tip. No `origin` ref contains `240d4b9b`, `ff970fd2`, `7c93031e`, `da4bd038`, `284facd6`, or `d6034684`. Only `fork/main` contains `240d4b9b`. The thousands of unrelated upstream feature heads were set-reconciled against `git ls-remote`; none has a scoped local branch name or protects a scoped custom tip.

### Relevant live remote refs

| Remote ref | Tip | Relation | Retirement status |
| --- | --- | --- | --- |
| `origin/main` | `64ec7f54163d918d5d7fd4dcae857f241b7842d0` | Upstream default | Never delete in this plan |
| `fork/main` | `240d4b9bb69df1d2fb1bf179668216b7c68d48c1` | Two commits ahead, zero behind `origin/main`; fork default | Preserve as current integration base |
| `fork/CCRBrad/multica-upstream-reconciliation` | `db45728c143f6f2e9cc0beb0aa1c354401cb0e04` | One report commit ahead of `fork/main` | Delete only after coordinator acceptance and the guarded lease check below |

The configured upstream `fork/feat/phase3-operational-controls` is already absent remotely. There is no remote branch to delete for Phase 3, the old operational line, or the replacement-runtime branch.

## Local branch inventory

| Local branch | Tip at snapshot | Upstream relation | `+/- vs origin/main` | Merged O/F | Worktree | Dirty state |
| --- | --- | --- | ---: | --- | --- | --- |
| `CCRBrad/multica-beta-ring-fence` | `240d4b9bb69df1d2fb1bf179668216b7c68d48c1` | none | `+2/-0` | no/yes | W3 | clean |
| `CCRBrad/multica-delta-operational-controls` | `240d4b9bb69df1d2fb1bf179668216b7c68d48c1` | none | `+2/-0` | no/yes | W4 | clean |
| `CCRBrad/multica-epsilon-branch-archaeology` | `240d4b9bb69df1d2fb1bf179668216b7c68d48c1` | none | `+2/-0` | no/yes | W5 | clean before this report |
| `CCRBrad/multica-gamma-provider-validation` | `240d4b9bb69df1d2fb1bf179668216b7c68d48c1` | none | `+2/-0` | no/yes | W6 | clean |
| `CCRBrad/multica-orca-selector-test` | `3f371d95b09631b756681fd692d6a80836978e9b` | none | `+3/-0` | no/no | W7 | clean |
| `CCRBrad/multica-upstream-reconciliation` | `db45728c143f6f2e9cc0beb0aa1c354401cb0e04` | `fork/CCRBrad/multica-upstream-reconciliation`, `0/0` | `+3/-0` | no/no | W8 | clean |
| `backup/phase3-omniroute-pre-rebase` | `ff970fd2837d865684f0efe3a5c356279e382f11` | none | `+17/-1186` | no/no | none | no working tree |
| `feat/operational-agent-mode` | `7c93031e63d2fa230344751eff7335e86c42cd85` | none | `+6/-1186` | no/no | W0 | clean |
| `feat/operational-agent-mode-phase3` | `da4bd0380e7cf5d69e3cb64ac46ad82a72ccbab3` | none | `+7/-1186` | no/no | none | no working tree |
| `feat/phase3-operational-controls` | `284facd686121ce7feeaff7b7ad171a208232eae` | `fork/feat/phase3-operational-controls`, gone | `+18/-1186` | no/no | W1 | standard status clean; hidden stash exists |
| `feat/replacement-runtime-providers` | `7c93031e63d2fa230344751eff7335e86c42cd85` | none | `+6/-1186` | no/no | none | no working tree |
| `feat/replacement-runtime-providers-main` | `d603468415e5e428251150a5f42010013eda5da4` | `origin/main`, `+5/-0` | `+5/-0` | no/no | W2 | one unstaged tracked file |
| `main` | `ff970fd2837d865684f0efe3a5c356279e382f11` | `origin/main`, `+17/-1186` | `+17/-1186` | no/no | none | no working tree |

The two Alpha report commits, `3f371d95...` and `db45728c...`, have the same tree and are patch-equivalent. The second is the live fork branch. Keep both until the coordinator accepts one canonical report lineage.

## Worktree inventory

| ID | Path | Type and branch | Snapshot state | Retirement gate |
| --- | --- | --- | --- | --- |
| W0 | `/Users/bradstrawbridge/.agents/workspaces/_archive/multica` | Primary checkout, `feat/operational-agent-mode` | Clean | Cannot be removed as an ordinary linked worktree. It owns the common `.git` directory for W1 through W8. Quarantine last, after every linked worktree is removed and the bundle is verified. |
| W1 | `/Users/bradstrawbridge/.config/superpowers/worktrees/multica/phase3` | Linked, `feat/phase3-operational-controls` | Standard status clean; `refs/stash` and ignored `.env.worktree` need preservation | Archive branch, stash, untracked stash parent, and environment file. Record integration or rejection of Phase 3 work. Remove without `--force` only after acceptance. |
| W2 | `/Users/bradstrawbridge/Documents/Codex/2026-08-28/multica-phase2-audit-2/work/multica-replacement-runtime` | Linked, `feat/replacement-runtime-providers-main` | Dirty: one unstaged tracked file, zero staged and zero untracked | Archive five commits and working-tree patch. Port or explicitly reject both. The worktree must become clean through an authorized integration process before normal removal. |
| W3 | `/Users/bradstrawbridge/orca/workspaces/multica/multica-beta-ring-fence` | Linked, active Orca branch | Clean at snapshot | Coordinator-controlled active worktree. Do not delete before its report is accepted. |
| W4 | `/Users/bradstrawbridge/orca/workspaces/multica/multica-delta-operational-controls` | Linked, active Orca branch | Clean at snapshot | Coordinator-controlled active worktree. Do not delete before its report is accepted. |
| W5 | `/Users/bradstrawbridge/orca/workspaces/multica/multica-epsilon-branch-archaeology` | Linked, this report branch | Clean before report creation | Do not delete before this report commit is accepted or integrated. |
| W6 | `/Users/bradstrawbridge/orca/workspaces/multica/multica-gamma-provider-validation` | Linked, active Orca branch | Clean at snapshot | Coordinator-controlled active worktree. Do not delete before its report is accepted. |
| W7 | `/Users/bradstrawbridge/orca/workspaces/multica/multica-orca-selector-test` | Linked, Alpha report branch | Clean at snapshot | Contains the local patch-equivalent Alpha report commit. Remove only after report acceptance. |
| W8 | `/Users/bradstrawbridge/orca/workspaces/multica/multica-upstream-reconciliation` | Linked, published Alpha report branch | Clean at snapshot | Remove only after report acceptance and remote cleanup. |

No registered worktree had a `locked` or `prunable` marker. The primary and Phase 3 `server/data/` directories were empty. Dependency directories and build caches are regenerable and do not need preservation.

## Unique commits that need preservation or review

### Current fork integration base

These two commits are published on `fork/main`, are not in `origin/main`, and must remain the base unless the coordinator explicitly replaces them:

```text
cdba5480000c1a49ceb1a03c3fc52befdcbbaaab feat: add provider runtime support
240d4b9bb69df1d2fb1bf179668216b7c68d48c1 test: add live provider smoke coverage
```

### Historical operational and OmniRoute line

The following 17 commits are absent from both remote default branches. The first six implement the original operational controls, the seventh completes that control line, and the final ten implement the older OmniRoute path. They must be treated as requirements and test evidence, not merged wholesale onto current main.

```text
079c99c6c33f1b9b9eb1b26d02c9f4285c6ef9b5 feat: operational and hybrid agent modes (Phase 1)
2a7b5f66313be35852b58c252c2654244f2c183d feat(agent): add agent_action_log audit table (Phase 3 part 1)
84cc1b7ea0a42f42d65c4665a152d102491a8963 feat(agent): add agent_action_log data layer (sqlc queries + generated code)
9ecba3dbb9d12ec1c430f6c184186cd919b26d54 feat(agent): write agent_action_log on task complete and fail (Phase 3 audit write)
0868fa1a579bc5dc24751b963827d83969abe217 feat(agent): add allowed_tools column and data layer (Phase 3 tool allowlist)
7c93031e63d2fa230344751eff7335e86c42cd85 feat(agent): persist and expose allowed_tools via API (Phase 3 allowlist plumbing)
da4bd0380e7cf5d69e3cb64ac46ad82a72ccbab3 feat(agent): complete operational agent controls and audit
53cf3566bc290575154c0b66b435dbb46d28982f docs: specify OmniRoute provider design
9a7cc9d3a7e5a8f9478b24c6367b03a0c1776d26 docs: plan OmniRoute provider implementation
7a0000f8c9a62a57c096fc86697273e64d281c5c feat: add OmniRoute backend transport
34be9e31dc065d9af1987c2716e4cf3815bc3ba0 feat: parse OmniRoute streaming responses
c895331a601ec0b3308bb8de2bc5e822b28ecd63 feat: register OmniRoute as a Multica provider
16aac31216a77b483ac31bbefe32b24df624da33 feat: connect OmniRoute agent loop to MCP tools
934ea114ad6195c4620248505831a57020062925 feat: harden OmniRoute session discovery
9dac84eb8190d90ac376ac702a856ff04ceddb6a feat: support stdio MCP with OmniRoute
ccbf1de5a934e1bb9f6b439ccdce16f6912611ea docs: document OmniRoute endpoint verification
ff970fd2837d865684f0efe3a5c356279e382f11 feat: complete OmniRoute provider execution
```

The Phase 3 branch adds one commit found on no other local branch:

```text
284facd686121ce7feeaff7b7ad171a208232eae docs: specify agent approval queue and operations dashboard
```

It adds `docs/superpowers/specs/2026-08-28-agent-approval-queue-and-operations-dashboard-design.md`.

### Replacement-runtime alternative

All five commits below are reachable from no other current local branch, remote ref, or tag. They compete with, rather than descend from, the provider implementation on `fork/main`.

```text
b2db1fe737dfd372dc0e3c23f33081be792d844b docs: plan replacement runtime OpenRouter slice
76e9fbf51f31392bde76c476789329e44db7c5b9 feat(agent): add replacement provider capability catalog
3282f748a8d8ef25146b82abb7965551adae2987 feat(agent): add OpenRouter HTTP execution slice
8974d8d92b05684bd8c03d62acc768b4b307a897 docs: document replacement provider capability gates
d603468415e5e428251150a5f42010013eda5da4 fix(agent): redact provider secrets from streamed output
```

Review these commits for any missing capability gates, streaming bounds, redirect refusal, redaction cases, and tests. Port only accepted gaps onto `fork/main`.

### Active reconciliation report refs

Keep the following until the coordinator records acceptance:

```text
3f371d95b09631b756681fd692d6a80836978e9b docs(reconciliation): audit upstream delta
db45728c143f6f2e9cc0beb0aa1c354401cb0e04 docs(reconciliation): audit upstream delta
```

They are patch-equivalent and have the same tree. Only `db45728c...` is published.

## Uncommitted and hidden artifacts

### W2 replacement-runtime dirty file

`packages/core/types/agent.ts` has 58 unstaged insertions and no staged counterpart. It introduces the provider-neutral runtime ID, execution-family, setup-mode, capability, status, and descriptor types. The working-tree blob is `5a502749d58fc19af1eff0a0078d440e710db778`; the HEAD/index blob is `66c7982dc23f9e18880a7cb3abd7effe04019b35`. This working-tree content is not in any commit and differs from `fork/main`.

Preservation requirement: save a binary patch before any worktree removal, then either port it into a reviewed commit or record its explicit rejection. Do not reset, checkout, or force-remove the worktree.

### W1 Phase 3 stash

The clean Phase 3 worktree hides a substantial stash:

```text
refs/stash 0f5927be70ff1ad3c639c7efa6a81517436a15b4
On feat/phase3-operational-controls: archive: obsolete OmniRoute and incomplete approval queue
```

Only `refs/stash` points at this commit. It preserves 16 unstaged tracked modifications and four untracked files, 20 files total, with 1,130 insertions and 132 deletions when untracked files are included.

Tracked modified paths:

```text
.env.example
README.md
README.zh-CN.md
docs/superpowers/plans/2026-08-27-omniroute-provider.md
docs/superpowers/specs/2026-08-27-omniroute-provider-design.md
server/internal/handler/agent.go
server/internal/handler/agent_allowed_tools.go
server/internal/handler/agent_allowed_tools_test.go
server/pkg/agent/omniroute_live_test.go
server/pkg/agent/omniroute_mcp.go
server/pkg/agent/omniroute_mcp_test.go
server/pkg/agent/omniroute_models.go
server/pkg/agent/omniroute_models_test.go
server/pkg/db/generated/agent.sql.go
server/pkg/db/generated/models.go
server/pkg/db/queries/agent.sql
```

Untracked paths stored only in the stash's third parent:

```text
server/migrations/138_agent_approval_queue.down.sql
server/migrations/138_agent_approval_queue.up.sql
server/pkg/db/generated/agent_approval_request.sql.go
server/pkg/db/queries/agent_approval_request.sql
```

The stash label is not disposal approval. Preserve the ref, a patch, and the third-parent files. Do not run `git stash drop` or `git stash clear`.

### W1 ignored environment file

W1 has an ignored `.env.worktree`. Its contents were not printed or committed. Copy it only into a permission-restricted private archive if its database identity or local runtime configuration must survive retirement. Never put it in a Git patch, tag, bundle manifest, issue, or report.

## Retirement gates by asset

| Asset | Earliest safe retirement point |
| --- | --- |
| `backup/phase3-omniroute-pre-rebase` | After full bundle verification and archival tag creation. It aliases local `main`, but keep at least one named archival ref until operational and OmniRoute review is accepted. |
| `feat/replacement-runtime-providers` | After bundle/tag verification. It aliases `feat/operational-agent-mode` exactly and is also an ancestor of the historical main line. |
| `feat/operational-agent-mode` and W0 checkout | Branch only after operational semantic-port decision and removal of W0 from that branch. W0 directory only after every linked worktree is removed and the common Git directory is externally archived. |
| `feat/operational-agent-mode-phase3` | After the operational controls are ported or explicitly rejected, and its exact tip is archived. |
| local `main` at `ff970fd2...` | Do not delete as if it were current main. Rename it to an archive branch after bundle/tag verification, then create a new local main from the coordinator-selected modern base. |
| `feat/phase3-operational-controls`, W1, and `refs/stash` | After the branch, stash, four untracked stash files, and ignored environment file are preserved; the approval-queue design and code are ported or explicitly rejected; and the coordinator accepts retirement. |
| `feat/replacement-runtime-providers-main` and W2 | After all five commits and the 58-line dirty patch are preserved and semantically reviewed; accepted deltas are integrated or explicitly rejected; W2 is clean; and the coordinator accepts retirement. |
| active Orca branches W3 through W8 | After each team's report commit is accepted or integrated. The remote Alpha branch also requires guarded remote deletion. |
| `fork/main` | Not a retirement candidate. It is the current modern integration base. |
| `origin/*` and release tags | Not retirement candidates. They are upstream history. |

## Exact preservation commands

These commands are a plan, not commands executed by Team Epsilon. Run them only after the coordinator quiesces all Multica workers and records approval.

### 1. Create a private archive and full bundle

```bash
repo_root='/Users/bradstrawbridge/.agents/workspaces/_archive/multica'
retire_archive='/Users/bradstrawbridge/Documents/Codex/2026-08-29/multica-retirement-archive'

mkdir -p "$retire_archive/private"
chmod 700 "$retire_archive" "$retire_archive/private"

git -C "$repo_root" bundle create \
  "$retire_archive/multica-all-refs-2026-08-29.bundle" --all
git -C "$repo_root" bundle verify \
  "$retire_archive/multica-all-refs-2026-08-29.bundle"
shasum -a 256 "$retire_archive/multica-all-refs-2026-08-29.bundle" \
  > "$retire_archive/multica-all-refs-2026-08-29.bundle.sha256"

git -C "$repo_root" bundle list-heads \
  "$retire_archive/multica-all-refs-2026-08-29.bundle" \
  | rg 'refs/(heads|remotes|stash|tags)/|refs/stash'
```

`--all` captures the current named refs, including `refs/stash`. Keep the bundle private until it has passed a secret scan. It must not be pushed to a public location.

### 2. Create explicit archival tags

```bash
git -C "$repo_root" tag -a archive/provider-runtime-main-20260829 \
  240d4b9bb69df1d2fb1bf179668216b7c68d48c1 \
  -m 'Archive provider runtime fork main before branch retirement'
git -C "$repo_root" tag -a archive/operational-baseline-20260706 \
  7c93031e63d2fa230344751eff7335e86c42cd85 \
  -m 'Archive operational agent baseline before branch retirement'
git -C "$repo_root" tag -a archive/operational-controls-20260815 \
  da4bd0380e7cf5d69e3cb64ac46ad82a72ccbab3 \
  -m 'Archive completed operational controls before branch retirement'
git -C "$repo_root" tag -a archive/omniroute-pre-rebase-20260828 \
  ff970fd2837d865684f0efe3a5c356279e382f11 \
  -m 'Archive historical OmniRoute line before branch retirement'
git -C "$repo_root" tag -a archive/phase3-plan-20260828 \
  284facd686121ce7feeaff7b7ad171a208232eae \
  -m 'Archive Phase 3 approval queue design before branch retirement'
git -C "$repo_root" tag -a archive/replacement-runtime-20260828 \
  d603468415e5e428251150a5f42010013eda5da4 \
  -m 'Archive replacement runtime alternative before branch retirement'
git -C "$repo_root" tag -a archive/phase3-stash-20260828 \
  0f5927be70ff1ad3c639c7efa6a81517436a15b4 \
  -m 'Archive Phase 3 stash and its parents before branch retirement'

git -C "$repo_root" show-ref --verify refs/tags/archive/provider-runtime-main-20260829
git -C "$repo_root" show-ref --verify refs/tags/archive/operational-baseline-20260706
git -C "$repo_root" show-ref --verify refs/tags/archive/operational-controls-20260815
git -C "$repo_root" show-ref --verify refs/tags/archive/omniroute-pre-rebase-20260828
git -C "$repo_root" show-ref --verify refs/tags/archive/phase3-plan-20260828
git -C "$repo_root" show-ref --verify refs/tags/archive/replacement-runtime-20260828
git -C "$repo_root" show-ref --verify refs/tags/archive/phase3-stash-20260828
```

Do not push the stash tag or any tag containing unreviewed historical content until a secret scan passes. The full private bundle is the first-line archive.

### 3. Preserve dirty and hidden artifacts

```bash
phase3_wt='/Users/bradstrawbridge/.config/superpowers/worktrees/multica/phase3'
replacement_wt='/Users/bradstrawbridge/Documents/Codex/2026-08-28/multica-phase2-audit-2/work/multica-replacement-runtime'

git -C "$replacement_wt" diff --binary HEAD -- \
  packages/core/types/agent.ts \
  > "$retire_archive/replacement-runtime-working-tree.patch"
test -s "$retire_archive/replacement-runtime-working-tree.patch"

git -C "$phase3_wt" stash show --include-untracked --binary -p refs/stash \
  > "$retire_archive/phase3-stash.patch"
test -s "$retire_archive/phase3-stash.patch"

git -C "$repo_root" archive --format=tar \
  --output="$retire_archive/phase3-stash-untracked.tar" refs/stash^3

install -m 600 "$phase3_wt/.env.worktree" \
  "$retire_archive/private/phase3.env.worktree"

shasum -a 256 \
  "$retire_archive/replacement-runtime-working-tree.patch" \
  "$retire_archive/phase3-stash.patch" \
  "$retire_archive/phase3-stash-untracked.tar" \
  > "$retire_archive/artifacts.sha256"
```

The private environment copy is intentionally excluded from the public checksum manifest and all Git artifacts.

## Exact guarded retirement commands

### Delete the merged remote report branch

There is one current remote deletion candidate, but it is not yet merged or patch-absorbed into `fork/main`. Run this only after the coordinator accepts the Alpha report and `fork/main` contains the report change or a patch-equivalent commit.

```bash
remote_branch='CCRBrad/multica-upstream-reconciliation'
expected_tip='db45728c143f6f2e9cc0beb0aa1c354401cb0e04'

git -C "$repo_root" fetch fork main
live_tip=$(git -C "$repo_root" ls-remote --heads fork \
  "refs/heads/$remote_branch" | awk '{print $1}')
test "$live_tip" = "$expected_tip"

test -z "$(git -C "$repo_root" cherry refs/remotes/fork/main \
  "$expected_tip" | awk '$1 == "+" {print}')"

git -C "$repo_root" push \
  --force-with-lease="refs/heads/$remote_branch:$expected_tip" \
  fork ":refs/heads/$remote_branch"
```

The lease prevents deletion if another worker advances the branch. Never delete `fork/main` or any `origin` branch as part of this plan.

### Remove linked worktrees

Use the same preflight for each accepted worktree. Do not use `--force`.

```bash
git -C "$repo_root" bundle verify \
  "$retire_archive/multica-all-refs-2026-08-29.bundle"

test -z "$(GIT_OPTIONAL_LOCKS=0 git -C "$phase3_wt" \
  status --porcelain --untracked-files=all)"
git -C "$repo_root" show-ref --verify \
  refs/tags/archive/phase3-plan-20260828
git -C "$repo_root" show-ref --verify \
  refs/tags/archive/phase3-stash-20260828
git -C "$repo_root" worktree remove "$phase3_wt"

test -z "$(GIT_OPTIONAL_LOCKS=0 git -C "$replacement_wt" \
  status --porcelain --untracked-files=all)"
git -C "$repo_root" show-ref --verify \
  refs/tags/archive/replacement-runtime-20260828
git -C "$repo_root" worktree remove "$replacement_wt"

git -C "$repo_root" worktree prune --dry-run --verbose
```

The W2 cleanliness test fails now by design. It may pass only after an authorized process preserves and resolves the dirty file. If a worktree owns a Multica development database that also needs retirement, back up or explicitly waive that data first, then use the repository's `make remove-worktree WORKTREE=<absolute-path>` workflow instead of bypassing the environment ledger.

### Prune local branches

First preview branches actually merged into the accepted modern base:

```bash
git -C "$repo_root" branch --merged refs/remotes/fork/main
git -C "$repo_root" worktree list --porcelain
```

After the relevant worktrees are removed and the coordinator confirms the branch names are no longer needed, use lowercase `-d` so Git refuses unmerged deletion:

```bash
git -C "$repo_root" branch -d CCRBrad/multica-beta-ring-fence
git -C "$repo_root" branch -d CCRBrad/multica-delta-operational-controls
git -C "$repo_root" branch -d CCRBrad/multica-gamma-provider-validation
git -C "$repo_root" branch -d CCRBrad/multica-orca-selector-test
git -C "$repo_root" branch -d CCRBrad/multica-upstream-reconciliation
```

Add the Epsilon branch only after this report is accepted. Branches from the historical operational, Phase 3, and replacement-runtime lines are currently unmerged and must not be forced away. After bundle/tag verification and a written integration or rejection decision, exact-tip guards can protect any approved forced pruning. Example for the redundant backup alias:

```bash
test "$(git -C "$repo_root" rev-parse \
  refs/heads/backup/phase3-omniroute-pre-rebase)" = \
  'ff970fd2837d865684f0efe3a5c356279e382f11'
test "$(git -C "$repo_root" rev-parse \
  refs/tags/archive/omniroute-pre-rebase-20260828^{})" = \
  'ff970fd2837d865684f0efe3a5c356279e382f11'
git -C "$repo_root" bundle verify \
  "$retire_archive/multica-all-refs-2026-08-29.bundle"
git -C "$repo_root" branch -D backup/phase3-omniroute-pre-rebase
```

Repeat that three-gate pattern with the exact expected tip and matching archival tag for any other intentionally rejected unmerged branch. Do not use a broad wildcard or bulk `-D` command.

Preserve the misleading local `main` name without deleting its history:

```bash
git -C "$repo_root" branch -m main archive/legacy-main-pre-rebase-20260828
git -C "$repo_root" branch --track main refs/remotes/fork/main
```

Run those two commands only after the coordinator selects `fork/main` as the canonical local main and verifies no worktree has `main` checked out.

### Quarantine the primary checkout last

W0 is not removable through `git worktree remove`. After all linked worktrees are gone, the bundle is verified, and the coordinator accepts retirement, move it to a recoverable quarantine instead of deleting it:

```bash
test "$(git -C "$repo_root" worktree list --porcelain \
  | awk '$1 == "worktree" {count++} END {print count+0}')" -eq 1
test -z "$(GIT_OPTIONAL_LOCKS=0 git -C "$repo_root" \
  status --porcelain --untracked-files=all)"
git -C "$repo_root" bundle verify \
  "$retire_archive/multica-all-refs-2026-08-29.bundle"

mkdir -p '/Users/bradstrawbridge/.agents/workspaces/_retired'
mv "$repo_root" \
  '/Users/bradstrawbridge/.agents/workspaces/_retired/multica-2026-08-29'
```

No `rm -rf` command belongs in this retirement plan.

## Strict do-not-delete list

Until the coordinator records acceptance, do not delete, move, reset, clear, force-remove, or prune any of the following:

1. `/Users/bradstrawbridge/.agents/workspaces/_archive/multica` or its `.git` directory.
2. `refs/stash` at `0f5927be70ff1ad3c639c7efa6a81517436a15b4` and its three parents.
3. W1, `feat/phase3-operational-controls`, or its ignored `.env.worktree`.
4. W2, `feat/replacement-runtime-providers-main`, or its modified `packages/core/types/agent.ts`.
5. Local `main`, `backup/phase3-omniroute-pre-rebase`, `feat/operational-agent-mode`, `feat/operational-agent-mode-phase3`, or `feat/replacement-runtime-providers`.
6. `fork/main` or either of its two custom commits.
7. Any active Orca worktree or its branch: W3 through W8, including this Epsilon report branch after commit.
8. The live remote report branch `fork/CCRBrad/multica-upstream-reconciliation` until its patch is accepted and the exact lease gate passes.
9. Any `origin` ref or release tag.

Also prohibited before acceptance: `git stash drop`, `git stash clear`, `git worktree remove --force`, `git branch -D` without all three guards, unguarded `git push --delete`, broad `git worktree prune`, and recursive filesystem deletion.

## Concurrent-state collision observed

The common Git directory was mutated by other workers during this read-only audit:

- Registered worktrees increased from eight to nine when W8 appeared.
- `CCRBrad/multica-orca-selector-test` advanced from `240d4b9b...` to `3f371d95...`.
- `CCRBrad/multica-upstream-reconciliation` advanced to `db45728c...` and its fork remote branch appeared.
- `.git/config` and `FETCH_HEAD` changed while the audit was running because another process refreshed refs and configured/published the Alpha branch.

Team Epsilon did not make those changes. This is not data loss, but it invalidates any deletion decision based on the first snapshot. The coordinator must quiesce workers and rerun the preflight immediately before retirement.

## Coordinator decision paths and execution plan

### Path 1: Preserve, semantically integrate, then retire

1. Quiesce workers and make the verified bundle, tags, patches, stash archive, and secure environment copy.
2. Use `fork/main` as the modern base.
3. Port accepted operational controls, approval-queue requirements, and replacement-provider defenses as small current-main changes.
4. Record acceptance of all reconciliation reports.
5. Remove clean linked worktrees, delete the accepted remote report branch with the lease guard, prune local aliases, and quarantine W0 last.

### Path 2: Preserve and explicitly reject old alternatives

1. Produce the same verified private archive.
2. Record a commit-by-commit and artifact-by-artifact rejection decision with the reason and accepted modern substitute.
3. Keep immutable archival tags, then prune the rejected local branch names with exact-tip guards.
4. Remove linked worktrees and quarantine W0 only after all status gates pass.

### Path 3: Freeze without retirement

1. Preserve the bundle and patches now.
2. Leave every branch, stash, and worktree registered and unchanged.
3. Revisit retirement after the coordinator has accepted the provider-validation and operational-control reports.

Path 1 is the safest route to a smaller working set without losing requirements evidence. Path 3 is the correct fallback if workers cannot be quiesced or the integration decisions remain open.
