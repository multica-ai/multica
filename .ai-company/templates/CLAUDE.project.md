# CLAUDE.md — <project-name>

<!-- Copy from .ai-company/templates/CLAUDE.project.md when onboarding.
     Company-wide norms: .delivery/company-os/ (synced from multica HQ).
     This file is PROJECT-SPECIFIC only — stack, commands, forbidden paths. -->

## Meta

| Field | Value |
|-------|-------|
| Project ID | `<registry-id>` |
| Delivery slug | `<slug>` → `.delivery/<slug>/` |
| Tier | production / staging / experiment |
| Repo | `owner/name` |

## Stack

- Runtime:
- Framework:
- Package manager:
- Deploy target:

## Commands (source of truth for Verifier)

Run the **narrowest** set that covers the change; full suite before PR when in doubt.

```bash
# Examples — replace with this repo's real commands
pnpm install
pnpm test
pnpm lint
make test          # if Go backend
make visual-check  # replica / landing only
make check         # full gate before PR
```

## Forbidden paths (unless brief explicitly allows)

Agent-safe tickets must **not** touch:

- `**/migrations/**` (or equivalent DB schema dirs)
- `**/auth/**`, `**/payment/**`, secrets (`.env*`)
- `.github/workflows/**` (human-only)
- Paths listed in `.delivery/<slug>/brief.md` → Out of Scope

## Product truth sources (read before coding)

| Doc | Path |
|-----|------|
| Brief | `.delivery/<slug>/brief.md` |
| Acceptance / DoD | `.delivery/<slug>/accept_cases.md` |
| Replica inventory | `.delivery/<slug>/competitor_inventory.md` (if applicable) |
| Wont do | `.delivery/<slug>/wont_do.md` (if applicable) |
| Human-only queue | `.delivery/<slug>/human-only-queue.md` (if applicable) |

## Company norms (do not duplicate here)

Read synced copy:

- `.delivery/company-os/README.md` — index
- `.delivery/company-os/docs/06-task-grading.md`
- `.delivery/company-os/docs/07-quality-gates.md`
- `.delivery/company-os/docs/18-definition-of-done.md`
- `.delivery/company-os/docs/20-issue-brief-style-guide.md`
- `.delivery/company-os/docs/21-label-state-machine.md`
- `.delivery/company-os/docs/28-norm-layers.md`

Refresh: `bash /path/to/multica/scripts/ai-company/sync-company-norms.sh --id <registry-id>`

## Agent pipeline

- Stages: `.cursor/agents/` (planner → implementer → verifier → reviewer)
- Kickoff: `.delivery/prompts/orchestrator-kickoff.md`
- Merge policy: `.delivery/config/merge-policy.json`
- Delivery README: `.delivery/README.md`

## Hard rules

1. CI / Verifier exit code ≠ 0 → not done.
2. Ambiguous requirements → `NEED_CLARIFY`, stop coding.
3. Do not expand scope beyond Issue + brief + accept_cases.
4. English code comments; UI copy per project locale.

## Links

- HQ playbook (after sync): `.delivery/company-os/docs/27-norm-sync.md`
- Onboard: `.delivery/company-os/runbooks/onboard-new-project.md`
