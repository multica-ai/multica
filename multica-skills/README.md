# multica-skills

Canonical source for Multica skills owned by this fork. Lives outside any
upstream-tracked path so `git rebase upstream/main` stays conflict-free
forever.

## Layout

```
multica-skills/
├── README.md              # this file
├── install.sh             # uploads any subdirectory as a Multica skill
├── Outline/               # per-user-filtered Outline access
│   ├── SKILL.md
│   └── Tools/
│       ├── Outline.sh
│       └── ResolveTriggerEmail.sh
└── OutlineLocal/          # workspace-wide Outline access (no filtering)
    ├── SKILL.md
    └── Tools/
        └── OutlineLocal.sh
```

Each skill subdirectory follows the canonical Claude-skill layout: a
top-level `SKILL.md` with YAML frontmatter (`name`, `description`) and
optional `Tools/` for executable helpers. The same `SKILL.md` works whether
the skill is invoked locally via Claude Code or uploaded to Multica via the
CLI — the daemon writes each skill to `<workdir>/.claude/skills/<name>/`,
preserving file paths.

## The two skills

### `Outline` — per-user filtered

Resolves the **triggering user's** Outline permissions and post-filters
results before returning them. Inside Multica, the email is auto-resolved
from `MULTICA_TASK_ID` via the public API (`agents/{id}/tasks` →
`comments` or `chat/sessions/{id}` → `workspaces/{id}/members`). Outside
Multica, the email is the first positional arg.

Use this when you want users in a Multica workspace to only see Outline
content they themselves have access to.

### `OutlineLocal` — no filtering

Calls Outline's API directly with the workspace admin token. No email,
no permission lookup, no post-filtering. Returns whatever the admin token
can see.

Use this when (a) the workspace doesn't need per-user filtering, (b)
performance matters and you can't pay the 3 extra HTTP calls per request,
or (c) you're testing.

## Install

```bash
# Pick a workspace (export once or pass --workspace-id each time).
export MULTICA_WORKSPACE_ID=<uuid>

# Install the per-user-filtered skill.
bash multica-skills/install.sh Outline

# Install the simpler unfiltered variant.
bash multica-skills/install.sh OutlineLocal

# Both.
bash multica-skills/install.sh Outline OutlineLocal
```

Then in the Multica web UI: attach the skill to an agent and set
`OUTLINE_API_KEY` either in the agent's Custom Env or in the workspace
daemon container's env (`runtime-workspace-deployment/docker-compose.yml`).

## Why this directory exists outside `server/` / `apps/`

Upstream `multica-ai/multica` doesn't have a `multica-skills/` directory.
By keeping skill sources here we get version control + diff review +
shared installs across team members, **without** patching any upstream
file. After every `git rebase upstream/main`, this directory remains
exactly as the fork left it; no merge conflicts, no patch reapplication.

The same fork-only-additive-directory pattern is already used by
`runtime-workspace-deployment/` and `self-hosted-deployment-script/`.
