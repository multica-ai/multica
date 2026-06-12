# Review Team — Multica Setup Guide

## Overview

The Review Team is a specialist review group for PR and branch review work. The standard setup is a single Multica `Review Team` squad led by the Reviewer Orchestrator. Assign review issues to the squad, not to individual specialist agents. The squad leader routes work to Python, .NET, and DevOps reviewers based on the changed files.

## Skills to import

| File | Assign to |
|------|-----------|
| `orchestrator.md` | Reviewer Orchestrator only |
| `python-reviewer.md` | Python Reviewer only |
| `dotnet-reviewer.md` | Dotnet Reviewer only |
| `devops-reviewer.md` | DevOps Reviewer only |

## Deterministic tools to import

Import `dettools/review_scope_partition.go` as the `review_scope_partition`
deterministic tool and enable it for the Reviewer Orchestrator. This tool is required for Review Team routing. If it is unavailable, stop and report that the deterministic tool plane is not enabled.

From the repo root, import or refresh it with:

```bash
multica dettool import-file dettools/review_scope_partition.go --output table
```

`multica dettool import-file` creates the tool on the first run and updates the
existing tool with the same name after source edits.

The daemon must also have the deterministic tool plane enabled, otherwise agent
backend logs will show `mcp_config=false` and the tool will never appear in the
orchestrator's MCP tool list:

```bash
export MULTICA_DETTOOLS_ENABLED=true
multica daemon restart
```

The daemon binary must include `multica mcp-tools serve`. Verify with:

```bash
multica mcp-tools --help
```

## Create the 4 agents

Create each agent in the Multica UI. Suggested names:

| Role | Suggested agent name | Skills |
|------|----------------------|--------|
| Orchestrator / squad leader | `Reviewer Orchestrator` | `orchestrator` |
| Python specialist | `Python Reviewer` | `python-reviewer` |
| .NET specialist | `Dotnet Reviewer` | `dotnet-reviewer` |
| DevOps specialist | `DevOps Reviewer` | `devops-reviewer` |

Set **Max concurrent tasks** to `1` for the orchestrator. Specialist concurrency can be higher if your repository and review process allow parallel reviews.

## Create the Review Team squad

Create the squad with the orchestrator as leader:

```bash
multica squad create \
  --name "Review Team" \
  --leader "Reviewer Orchestrator"
```

Capture the created squad id, then add the specialist agents. Agent ids come from `multica agent list --output json`.

```bash
SQUAD_ID=<review-team-squad-id>
PYTHON_ID=<python-reviewer-agent-id>
DOTNET_ID=<dotnet-reviewer-agent-id>
DEVOPS_ID=<devops-reviewer-agent-id>

multica squad member add "$SQUAD_ID" \
  --member-id "$PYTHON_ID" \
  --type agent \
  --role "Reviews Python code, Python packaging/config, and Python tests. Checks STYLE.md compliance."

multica squad member add "$SQUAD_ID" \
  --member-id "$DOTNET_ID" \
  --type agent \
  --role "Reviews .NET code, project/config files, and .NET tests. Checks STYLE.md compliance."

multica squad member add "$SQUAD_ID" \
  --member-id "$DEVOPS_ID" \
  --type agent \
  --role "Reviews CI/CD, containers, infrastructure, deployment, and operational configuration. Checks STYLE.md compliance."
```

Add squad instructions so the leader routes consistently:

```bash
multica squad update "$SQUAD_ID" --instructions "Route Python files to Python Reviewer, .NET files to Dotnet Reviewer, and CI/CD/container/infrastructure/deployment/operations files to DevOps Reviewer. Use the exact roster mention markdown. Delegate matching scopes, stop after dispatch, and record squad activity. After specialists report back, synthesize one recommendation-only final review on the Multica issue. Never post Azure DevOps PR comments."
```

## Triggering a review

1. Create a Multica issue describing the review target. Include a PR id/URL or a changed-file list.
2. Assign the issue to the `Review Team` squad.
3. The squad leader will classify the changed files, mention the matching specialist reviewers, record squad activity, and stop.
4. Specialist reviewers post findings on the same Multica issue.
5. The leader is re-triggered by specialist updates and publishes one final recommendation-only review.

## Notes

- Squads are the standard Review Team configuration because the work is routing-oriented and naturally fans out to specialists.
- Do not ask specialist reviewers to modify code; they only return findings and recommendations.
- All reviewers must check `STYLE.md` compliance for assigned files.
