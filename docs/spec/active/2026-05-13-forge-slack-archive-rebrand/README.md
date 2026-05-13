---
project_id: SPEC-2026-05-13-001
project_name: "Forge: Slack Notifications + Archive Relabel + Tab Rebrand"
slug: forge-slack-archive-rebrand
status: approved
created: 2026-05-13T21:50:00+05:30
approved: 2026-05-13T23:57:00+05:30
approved_by: "shivasymbl <sdevinarayanan@asymbl.com>"
started: null
completed: null
expires: 2026-08-13T21:50:00+05:30
superseded_by: null
tags: [slack, integrations, notifications, branding, rbac, fork]
stakeholders: [shivasymbl]
worktree:
  branch: main
  base_branch: main
---

# Forge: Slack Notifications + Archive Relabel + Tab Rebrand

## Quick overview

Three features bundled because they share scope (post-v0.2.32 sync polish) and ship together:

1. **Slack webhook notifications per workspace** — admin-configured webhook URL, fires on issue status change to selected statuses.
2. **Archive relabel** — rename `cancelled` status to "Archive" in the UI (zero schema change, 5 locations).
3. **Browser tab rebrand fix** — favicon shows the Multica asterisk and tab title says "Multica — …" — a brand leak from the v0.2.32 sync.

## Documents

| File | Purpose |
|---|---|
| `REQUIREMENTS.md` | What to build, success criteria, constraints |
| `ARCHITECTURE.md` | Technical design — data model, hook points, blast radius |
| `IMPLEMENTATION_PLAN.md` | Phased task breakdown with effort estimates |
| `DECISIONS.md` | ADRs for the key choices |
| `RESEARCH_NOTES.md` | jcodemunch blast-radius + zen analysis evidence |
| `CHANGELOG.md` | Spec evolution history |

## Status: approved

Approved 2026-05-13 by shivasymbl <sdevinarayanan@asymbl.com>.

## Next step

```
/claude-spec:implement forge-slack-archive-rebrand
```
