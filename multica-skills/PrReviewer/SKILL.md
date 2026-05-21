---
name: PrReviewer
description: End-to-end GitHub PR review plumbing for a Multica agent — resolves PR coordinates from the assigned Multica issue, clones the repo at the PR head, prints the diff for the agent to reason over, and posts inline or summary comments / approves / requests-changes back on the PR via the `gh` CLI. USE WHEN review pr, code review github pr, scan pr for vulnerabilities, post pr review comments, comment on github pr, approve pr, request changes on pr.
---

# PrReviewer

Operational plumbing for an agent that reviews GitHub pull requests. The skill does **not** perform the review itself — it gives the agent (Claude) the PR coordinates, the working copy, and the diff, then takes back review comments and posts them. The judgement about what is a bad practice or a vulnerability is the agent's job.

The PR/issue handoff is the contract defined by `docs/pr-agent-sidecar-plan.md` (INV-496): the sidecar receives a GitHub webhook, creates a Multica issue assigned to this agent, and embeds the PR URL + head SHA in the issue description. This skill consumes that issue.

## Hard rules

- **Never print `$GH_TOKEN` or `$MULTICA_TOKEN`.** Not in echoes, not in error messages, not in command traces. Keep the literal `$GH_TOKEN` reference in any command shown to the user.
- **Read-only by default.** `resolve`, `clone`, and `diff` are safe to run unconditionally. `comment`, `review`, and `push` are write operations on the PR — only run them when the agent has explicitly produced a payload to send.
- **Repo allowlist is enforced.** `REPO_ALLOWLIST` must contain `owner/repo` (comma-separated) or every write subcommand and every clone refuses with exit 5. This mirrors the sidecar's webhook-side allowlist so the agent cannot be tricked into operating on an arbitrary repo by a forged issue body.
- **`gh` CLI is a prerequisite, not bundled.** It is not yet installed in the runtime-workspace base image. If `gh` is missing the script exits 8 with a clear message — do not try to install it on the fly.
- **The agent reasons; the script plumbs.** Do not add review heuristics, linters, or vuln scanners to this skill. Hand the diff back; let the agent decide.

## Multica auto-resolution

When the skill is attached to a Multica agent and the daemon spawns the agent process, these env vars are set automatically: `MULTICA_TASK_ID`, `MULTICA_AGENT_ID`, `MULTICA_WORKSPACE_ID`, `MULTICA_TOKEN`, `MULTICA_SERVER_URL`. The `resolve` subcommand chains the public API (`/api/agents/{id}/tasks` → `.issue_id` → `/api/issues/{id}`) to find the issue that triggered this run, then parses the PR URL and head SHA out of the issue body.

No upstream patch to Multica is needed — every API used here is already public.

## Prerequisites

| Env var          | Required by                  | Notes |
|------------------|------------------------------|-------|
| `GH_TOKEN`       | every subcommand except `resolve` and `diff` | GitHub PAT or App installation token with `repo` + `pull_requests:write` scopes. `gh` reads it automatically via the standard env var. |
| `REPO_ALLOWLIST` | `clone`, `comment`, `review`, `push` | Comma-separated `owner/repo`, or `*` to allow every repo. |
| `MULTICA_*`      | `resolve`                    | Injected by the daemon. |

Verify before the first call:

```bash
[ -n "$GH_TOKEN" ] && command -v gh >/dev/null && echo ok || echo "missing GH_TOKEN or gh CLI"
```

If `gh` is missing, stop and tell the user:
> `gh` CLI is not installed in this runtime workspace. Bake it into `runtime-workspace-deployment/Dockerfile` (`apt-get install gh` after adding the GitHub CLI apt source) and redeploy.

## Helper script

All work goes through `Tools/PrReviewer.sh`. Invoke as (relative to the skill directory):

```bash
bash Tools/PrReviewer.sh <subcommand> [args...]
```

## Workflows

### 1. Resolve the PR coordinates from the issue

**Triggers:** "what PR am I reviewing", "start the review", "review the PR" — implicit first step before any other subcommand.

```bash
bash Tools/PrReviewer.sh resolve
```

Reads the Multica issue assigned to this task and prints a JSON object:

```json
{
  "pr_url": "https://github.com/owner/repo/pull/123",
  "owner": "owner",
  "repo": "repo",
  "pr_number": 123,
  "head_sha": "abc123…",
  "base_ref": "main"
}
```

`base_ref` is fetched via `gh pr view` if `gh` and `GH_TOKEN` are available; otherwise it is `null` and the agent should resolve it later.

### 2. Clone the repo at the PR head

**Triggers:** "clone the repo", "check out the PR locally"

```bash
bash Tools/PrReviewer.sh clone <owner> <repo> <head_sha>
```

Shallow-clones `https://github.com/<owner>/<repo>` (depth 50) into a fresh tmpdir, fetches the head SHA, checks it out, and prints the tmpdir path to stdout. The repo must be in `REPO_ALLOWLIST`.

### 3. Compute the diff vs the PR base

**Triggers:** "show me the diff", "what changed in this PR"

```bash
bash Tools/PrReviewer.sh diff <tmpdir> <base_ref>
```

Fetches `origin/<base_ref>` (depth 50) and prints `git diff origin/<base_ref>...HEAD` (three-dot — merge-base diff, which is what GitHub itself shows on the Files Changed tab). Pipe this into the agent's reasoning step.

### 4. Post an inline comment on a specific file + line

**Triggers:** "comment on line N of <file>", "leave an inline note"

```bash
bash Tools/PrReviewer.sh comment <owner> <repo> <pr_number> <path> <line> <body>
```

POSTs to `/repos/{owner}/{repo}/pulls/{pr_number}/comments` via `gh api`. The script auto-resolves the PR's head commit (`headRefOid`) and uses it as `commit_id`. `<path>` is the repo-relative file path. `<line>` is the line number in the new (head) version of the file.

### 5. Submit an overall review (summary / approve / request changes)

**Triggers:** "approve the PR", "request changes", "leave a summary review"

```bash
# Comment-only summary review (most common)
bash Tools/PrReviewer.sh review <owner> <repo> <pr_number> COMMENT "<markdown body>"

# Block the merge
bash Tools/PrReviewer.sh review <owner> <repo> <pr_number> REQUEST_CHANGES "<markdown body>"

# Approve
bash Tools/PrReviewer.sh review <owner> <repo> <pr_number> APPROVE "<markdown body>"
```

`<event>` must be one of `COMMENT`, `APPROVE`, `REQUEST_CHANGES`. The body can also be piped via stdin if it's larger than the shell can pass safely:

```bash
echo "$LONG_BODY" | bash Tools/PrReviewer.sh review owner repo 123 COMMENT -
```

### 6. Push a fix commit back to the PR branch (optional)

**Triggers:** "commit the fix and push", "push my changes to the PR"

```bash
bash Tools/PrReviewer.sh push <tmpdir> <branch> "<commit_msg>"
```

Stages all changes in `<tmpdir>`, commits with `<commit_msg>` as the author `multica-pr-reviewer <noreply@multica>`, and pushes to `origin <branch>`. The repo must be in `REPO_ALLOWLIST`. Refuses to push if the working tree is clean.

## Error handling

The script exits non-zero with a stderr message for these cases:

| Exit | Meaning                                            | What to tell the user                                      |
|------|----------------------------------------------------|------------------------------------------------------------|
| 2    | Missing required env var or positional arg         | Name the missing field; ask for it or set it               |
| 3    | Multica API call failed                            | Check `MULTICA_SERVER_URL` / `MULTICA_TOKEN` validity      |
| 4    | Could not extract PR URL / head SHA from issue body| The triggering issue isn't a PR-review issue from the sidecar — refuse to proceed |
| 5    | Repo not in `REPO_ALLOWLIST`                       | "Repo `owner/repo` is not in the allowlist — refusing"     |
| 6    | git clone / fetch / checkout failed                | Surface the underlying git error                           |
| 7    | `gh` API call failed                               | Surface the underlying gh error (token scope, 404, etc.)   |
| 8    | `gh` CLI not installed                             | Install `gh` in the runtime container                      |

## See also

- `docs/pr-agent-sidecar-plan.md` — the sidecar that creates the Multica issues this skill consumes. The issue body shape is owned by that sidecar; if you change the format there, update the parser here.
- `Outline/SKILL.md` — same canonical skill layout; reference for prose style and error-table convention.
