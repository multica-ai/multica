---
name: multica-operational-workflow
description: "Use for business workflow tasks assigned to an operational or hybrid agent. Read the Multica issue first, act through approved business tools, and report evidence without assuming that mode grants permission."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Operational workflow

This skill applies only when the agent is configured for operational or hybrid
work. Operating mode describes product workflow intent only. It does not grant authorization,
approve a tool call, widen policy, or replace any runtime
approval requirement.

Every contract below is traced in
`references/operational-workflow-source-map.md`.

## Read before acting

Start with the assigned issue and its relevant discussion:

```bash
multica issue get <issue-id> --output json
multica issue comment list <issue-id> --roots-only --summary --compact --output json
multica issue comment list <issue-id> --thread <thread-id> --tail 30 --output json
```

Use the issue title, description, and comments to identify the requested
business outcome, available evidence, constraints, and completion standard.
Do not infer authority from the agent's operating mode.

## Execute the business task

Use approved MCP business tools exposed to this run, not raw external APIs.
Follow each tool's current authorization and approval flow. If the required
tool is unavailable or a required approval is not granted, do not simulate the
action. Record what is blocked and what evidence is still needed.

Do not write or modify code unless the issue explicitly asks for code. An
operational task can involve documents, records, research, coordination, or
other business work without becoming a repository task.

## Report and close honestly

Write the result to a UTF-8 file and post it with `--content-file`:

```bash
multica issue comment add <issue-id> --content-file ./result.md
```

Post a concise result comment that states what was completed, the evidence,
and any remaining blocker. Move the issue to done only when evidence supports
the requested outcome. Otherwise leave an honest status and the next required
step.
