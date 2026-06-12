---
name: Shared State Operations
description: Read and write coding-team pipeline state stored as a JSON block in the Multica master issue description
---

# Coding Team State Operations

All pipeline state lives in the master Multica issue description as a fenced JSON block. Every agent reads the current state, mutates its portion, and writes it back before handing off to the next agent.

Role boundaries are strict: Orchestrator coordinates, never edits repository code, and checks out the repo only in Run 2a to create/verify the shared feature branch; Planner owns the first codebase inspection and never edits; Implementer writes production code and must commit/push before handoff; Test Writer writes tests and must commit/push before handoff; Reviewer reviews and signals; PR Writer creates the draft PR. Never clean or delete a worktree that contains uncommitted or unpushed changes.

Use the `pipeline_state_parse` deterministic tool to parse issue descriptions. Do not reimplement fenced/leading JSON extraction in shell or Python.

Call shape:
```json
{"description":"{issue description text}"}
```

Use `machine_data.state` only when `machine_data.is_pipeline_state == true`. Use `machine_data.config` and `machine_data.body` during Fresh Start when the issue has a config-only JSON block. If `pipeline_state_parse` is unavailable, stop and report that the deterministic tool plane is not enabled.

Use the `coding_comment_extract` deterministic tool to parse coding-team comments, marker ordering, and fenced `json coding-team-artifact` blocks. Downstream roles must prefer `machine_data.artifacts.*` over prose markdown when an artifact exists. If `coding_comment_extract` is unavailable, stop instead of regex-scanning comment markdown.

---

## State schema

```json
{
  "deliverable_id": "12345 or null for direct master-issue requests",
  "code_org": "ineight",
  "code_project": "Platform",
  "repo_name": "AgenticAI",
  "repo_url": "https://anything:ADO_PAT_INEIGHT@dev.azure.com/{code_org}/{code_project}/_git/{repo_name}",
  "base_branch": "develop",
  "branch": "feature/12345_enforcement_post_authorize",
  "stage": "guided_planning | awaiting_approval | implementing | awaiting_push | done",
  "planning_source": "regular_ado_tasks | guided_plan | orchestrator_decomposition",
  "planning_status": "questioning | ready | approved",
  "guided_plan": {
    "status": "questioning | ready",
    "source_context": {
      "deliverable_summary": "...",
      "authoritative_comments": ["..."]
    },
    "current_question": {
      "question": "...",
      "recommended_answer": "...",
      "why_this_matters": "..."
    },
    "answered_questions": [
      {
        "question": "...",
        "recommended_answer": "...",
        "answer": "...",
        "resolution": "..."
      }
    ],
    "resolved_decisions": ["..."],
    "codebase_findings": ["..."]
  },
  "deliverable": {
    "source": "ado | master_issue",
    "title": "...",
    "description": "...",
    "acceptance_criteria": ["..."],
    "area_path": "ineight\\Team",
    "iteration_path": "ineight\\Sprint 42"
  },
  "tasks": [
    {
      "task_issue_id": "multica-abc123",
      "ado_id": 67890,
      "source": "ado_existing | guided_plan | generated",
      "ado_title": "Create integration tests",
      "title": "Integration tests for POST /authorize (C#)",
      "description": "...",
      "acceptance_criteria": ["..."],
      "estimated_language": "csharp",
      "status": "pending | awaiting_clarification | planned | implemented | tested | committed | failed"
    }
  ]
}
```

`planning_source` records how implementation tasks entered the pipeline:

| Value | Meaning |
|-------|---------|
| `regular_ado_tasks` | Planning happened in ADO before Multica started; task objects came from existing non-Done child Task work items. |
| `guided_plan` | The Orchestrator used the ADO deliverable or direct master issue content as guided-planning input; ADO Task work items are created after approval only when `deliverable_id` exists. |
| `orchestrator_decomposition` | The Orchestrator decomposed the deliverable from ADO or direct master issue content. |

`planning_status` is `questioning` while guided planning is asking one decision question at a time, `ready` while the master issue is waiting for task approval, and `approved` after Run 2a has created/reused any applicable ADO task work items and created Multica child issues.

`guided_plan` is optional and may be present when `planning_source == "guided_plan"`. It stores the guided planning conversation and any ADO or codebase findings used to resolve decisions. Canonical task data always lives in `tasks`. A guided run does not require a pre-existing `guided_plan` object in the Multica issue.

If `deliverable_id` is null/absent, the run is Multica-only: do not call ADO, do not create/link ADO work items, do not fetch ADO Component context, and do not post ADO comments. Use the master issue title/body/comments as deliverable context and keep task `ado_id` null/empty.

In Multica-only guided planning, the first Fresh Start run must initialize `stage: "guided_planning"` and ask exactly one guided-planning question before any task synthesis, child issue creation, branch creation, or Planner handoff. A config-only JSON block with `planning_source: "guided_plan"` is not pipeline state unless it also has `stage`.

Task `source` controls ADO creation idempotency when `deliverable_id` exists:
- `ado_existing`: `ado_id` must already be populated; Run 2a skips ADO creation and only creates the Multica task issue if needed.
- `guided_plan`: `ado_id` is empty until Run 2a creates the ADO Task; if `deliverable_id` is null, `ado_id` remains null/empty.
- `generated`: `ado_id` is empty until Run 2a creates the ADO Task; if `deliverable_id` is null, `ado_id` remains null/empty.

The code repository is configured by the master issue and stored in state. `repo_url` must embed `ADO_PAT_INEIGHT` so git operations authenticate without prompts. If the issue supplied `repo_url`, preserve its org/project/repo path and add credentials. If `repo_url` was omitted, build it from `code_org`, `code_project`, and `repo_name`:
```
https://anything:$ADO_PAT_INEIGHT@dev.azure.com/{code_org}/{code_project}/_git/{repo_name}
```

If old issues omit repo fields, default to `code_org: "ineight"`, `code_project: "Platform"`, `repo_name: "AgenticAI"` for backward compatibility.

---

## Read state from the master issue

1. Fetch the issue JSON with `multica issue get {master_issue_id} --output json`.
2. Pass its `description` to `pipeline_state_parse`.
3. Use `machine_data.state` when `machine_data.is_pipeline_state == true`; otherwise treat the state as `{}`.

An empty object `{}` means the issue is uninitialized (Orchestrator first run). A config-only JSON block without `stage` must be handled via `machine_data.config`/`machine_data.body`; it is Run 1 input, not resumable state.

If `pipeline_state_parse` is unavailable, stop and report that the deterministic tool plane is not enabled. Do not substitute inline Python, jq, grep, or regex parsing.

---

## Write state to the master issue

Build the updated state JSON in a shell variable or file, then write it back:

The master issue id is a remote Multica record, not a filesystem path. Do not use `Edit`, `Write`, `NotebookEdit`, patch tools, or file editing APIs on `{master_issue_id}`, `$MULTICA_ISSUE_ID`, task issue ids, ADO ids, or UUIDs. State writes must go through `multica issue update` only. If a file-editing tool reports `Could not edit file ... ENOENT`, that means an issue id was treated as a local file; stop and rerun the update with `multica issue update {master_issue_id} --description-stdin`.

```bash
# $NEW_STATE_JSON holds the complete updated state as a JSON string
cat <<ENDDESC | multica issue update {master_issue_id} --description-stdin
\`\`\`json
$NEW_STATE_JSON
\`\`\`
ENDDESC
```

To produce `$NEW_STATE_JSON`, use Python to build and serialize the state dict:

```bash
NEW_STATE_JSON=$(python3 - <<EOF
import json

state = json.loads('''$CURRENT_STATE''')
# ... mutate state ...
state['stage'] = 'implementing'
print(json.dumps(state, indent=2))
EOF
)
```

---

## Update a single task's status

```bash
NEW_STATE_JSON=$(python3 - "$CURRENT_STATE" "$TASK_ISSUE_ID" "$NEW_STATUS" <<'EOF'
import json, sys

state = json.loads(sys.argv[1])
target_id = sys.argv[2]
new_status = sys.argv[3]

for task in state.get('tasks', []):
    if task['task_issue_id'] == target_id:
        task['status'] = new_status
        break

print(json.dumps(state, indent=2))
EOF
)
```

Then write the updated state back to the master issue as shown above.

---

## Discover a coding-team agent ID by name

All pipeline agents use well-known names. Look them up at runtime to get their IDs for @mentions:

```bash
AGENTS_JSON=$(multica agent list --output json)

get_agent_id() {
  local agents_json="$1"
  local target="$2"
  local result
  result=$(python3 - "$agents_json" "$target" <<'EOF'
import json, sys
agents = json.loads(sys.argv[1])
target = sys.argv[2]
for a in agents:
    if a.get('name') == target:
        print(a['id'])
        sys.exit(0)
sys.exit(1)
EOF
  )
  if [ -z "$result" ]; then
    echo "ERROR: agent '$target' not found in workspace" >&2
    return 1
  fi
  echo "$result"
}

PLANNER_ID=$(get_agent_id "Coding Team Planner")
IMPLEMENTER_ID=$(get_agent_id "Coding Team Implementer")
TESTER_ID=$(get_agent_id "Coding Team Test Writer")
REVIEWER_ID=$(get_agent_id "Coding Team Reviewer")
PR_WRITER_ID=$(get_agent_id "Coding Team PR Writer")
ORCHESTRATOR_ID=$(get_agent_id "Coding Team Orchestrator")
```

Agent names (exact, case-sensitive):
| Role | Name |
|------|------|
| Orchestrator | `Coding Team Orchestrator` |
| Planner | `Coding Team Planner` |
| Implementer | `Coding Team Implementer` |
| Test Writer | `Coding Team Test Writer` |
| Reviewer | `Coding Team Reviewer` |
| PR Writer | `Coding Team PR Writer` |

---

## Sync to the shared feature branch after repo checkout

**`$BRANCH` always comes from the `branch` field of the task issue description JSON** (or the master issue state for the Orchestrator and PR Writer). Never derive it from `git rev-parse --abbrev-ref HEAD`, never read it from the current worktree, never accept any branch name that does not start with `feature/`.

`multica repo checkout` gives a worktree on a daemon-assigned local branch like `agent/<name>/<task-id>`. **That name is irrelevant.** It must never appear in any push, commit message, PR field, or status update. Always `git reset --hard` to the shared feature branch and always `git push` to it explicitly.

```bash
# $BRANCH must already be set from the task issue JSON, e.g. feature/enforcement_post_authorize_47358
if [[ "$BRANCH" != feature/* ]]; then
  echo "ERROR: BRANCH must start with 'feature/' — got '$BRANCH'" >&2
  exit 1
fi

REPO_PATH=$(multica repo checkout "$REPO_URL")
cd "$REPO_PATH"
git fetch origin
git reset --hard "origin/$BRANCH"
```

The `git reset --hard` aligns the daemon-assigned worktree to the shared feature branch without a conflicting checkout. If `origin/$BRANCH` does not exist, the reset fails — stop and surface the error. Do not fall back to the daemon-assigned branch.

To push commits back to the feature branch, use `git_push_clean` instead of a bare push. It handles both local `post-commit` hook injection (via `git_commit_clean`) and server-side trailer injection (by verifying the remote commit after push and force-pushing a clean replacement if needed):

```bash
git add -A
git_commit_clean "your message"
git_push_clean "$BRANCH"
```

This pushes to `origin/{branch}` regardless of the local branch name.

---

## Commit attribution — single-author commits only

Commits MUST show only the human user as the author. Do NOT add `Co-Authored-By:` trailers — not for Claude, not for `multica-agent`, not for anyone. The default Claude Code behavior of appending `Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>` is **explicitly overridden in this pipeline**, as is any Multica runtime instruction to add `Co-Authored-By: multica-agent <github@multica.ai>`.

Use this helper for every commit. It makes the initial commit, then unconditionally replaces it with a hook-free raw commit object so no `post-commit` hook (including Multica's trailer injector) can survive:

```bash
git_commit_clean() {
  local msg="$1"

  # Step 1 — make the real commit (pre-commit and commit-msg hooks run normally;
  # post-commit still runs and may inject a Co-authored-by trailer)
  printf '%s\n' "$msg" | git -c commit.template= commit \
    -F - \
    --cleanup=verbatim

  # Step 2 — read metadata from the commit we just made
  local tree parent author_name author_email author_date
  local committer_name committer_email committer_date
  tree=$(git log -1 --pretty=format:"%T")
  parent=$(git log -1 --pretty=format:"%P")
  author_name=$(git log -1 --pretty=format:"%an")
  author_email=$(git log -1 --pretty=format:"%ae")
  author_date=$(git log -1 --pretty=format:"%aI")
  committer_name=$(git log -1 --pretty=format:"%cn")
  committer_email=$(git log -1 --pretty=format:"%ce")
  committer_date=$(git log -1 --pretty=format:"%cI")

  # Step 3 — create a new raw commit object with commit-tree (runs NO hooks)
  # and point HEAD at it. This eliminates any trailer the post-commit hook added.
  local new_sha
  new_sha=$(
    GIT_AUTHOR_NAME="$author_name" \
    GIT_AUTHOR_EMAIL="$author_email" \
    GIT_AUTHOR_DATE="$author_date" \
    GIT_COMMITTER_NAME="$committer_name" \
    GIT_COMMITTER_EMAIL="$committer_email" \
    GIT_COMMITTER_DATE="$committer_date" \
    printf '%s\n' "$msg" | git commit-tree "$tree" -p "$parent" -F -
  )
  git update-ref HEAD "$new_sha"
}
```

After every commit, the commit message body must contain only your intended message — no `Co-Authored-By:` lines, no `🤖 Generated with` footer, no agent-attribution boilerplate.

Also define this push helper, which verifies the remote commit after push and replaces it with a clean one if the platform injected a trailer server-side:

```bash
git_push_clean() {
  local branch="$1"
  git push origin "HEAD:$branch"

  # Verify the remote commit is clean. If the platform injected a co-author
  # trailer server-side, replace the remote commit with a hook-free version.
  git fetch origin "$branch"
  if git log -1 --pretty=format:"%B" "origin/$branch" | grep -qi "co-authored-by"; then
    local msg tree parent an ae ad cn ce cd new_sha
    msg=$(git log -1 --pretty=format:"%B" "origin/$branch" \
          | grep -vi "co-authored-by" \
          | sed -e ':a' -e '/^\s*$/{$d;N;ba' -e '}')
    tree=$(git log -1 --pretty=format:"%T"  "origin/$branch")
    parent=$(git log -1 --pretty=format:"%P" "origin/$branch")
    an=$(git log -1 --pretty=format:"%an"   "origin/$branch")
    ae=$(git log -1 --pretty=format:"%ae"   "origin/$branch")
    ad=$(git log -1 --pretty=format:"%aI"   "origin/$branch")
    cn=$(git log -1 --pretty=format:"%cn"   "origin/$branch")
    ce=$(git log -1 --pretty=format:"%ce"   "origin/$branch")
    cd=$(git log -1 --pretty=format:"%cI"   "origin/$branch")
    new_sha=$(
      GIT_AUTHOR_NAME="$an"    GIT_AUTHOR_EMAIL="$ae"    GIT_AUTHOR_DATE="$ad" \
      GIT_COMMITTER_NAME="$cn" GIT_COMMITTER_EMAIL="$ce" GIT_COMMITTER_DATE="$cd" \
      printf '%s\n' "$msg" | git commit-tree "$tree" -p "$parent" -F -
    )
    git update-ref HEAD "$new_sha"
    git push origin "HEAD:$branch" --force-with-lease
  fi
}
```
