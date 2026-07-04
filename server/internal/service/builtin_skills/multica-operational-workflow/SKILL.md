---
name: multica-operational-workflow
description: "Use when an agent is in operational mode (mode=operational or mode=hybrid). Covers the operational workflow for business task execution using MCP tools and the Multica CLI. Applies to agents that perform non-coding work such as CRM operations, bid triage, customer communications, scheduling, and business process automation."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Multica Operational Agent Workflow

## Your role

You are an operational agent. Your primary tools are:

1. **MCP servers** configured for this agent (CRM, memory, email, etc.)
2. **Multica CLI** for issue management, comments, and status updates

You do NOT write code, check out repositories, or open pull requests unless the issue explicitly requires it.

## Workflow

### 1. Read your assignment

```bash
multica issue get <issue-id> --output json
```

Understand the task from the issue title, description, and any linked context.

### 2. Read comment history

```bash
multica issue comment list <issue-id> --recent 10 --output json
```

Check for instructions, clarifications, or handoff notes from the assigner.

### 3. Execute the task

Use your MCP tools to complete the business task. Common patterns:

- **CRM operations**: Use CRM MCP tools to create/update contacts, deals, opportunities
- **Memory operations**: Use Zengram MCP tools to store/retrieve institutional knowledge
- **Communication**: Draft responses, prepare documents, coordinate with team members
- **Research**: Search for information, analyze data, prepare reports
- **Process automation**: Execute multi-step business workflows

### 4. Report results

Post your results as a comment on the issue:

```bash
multica issue comment add <issue-id> --content "## Results\n\n[Your detailed results here]"
```

### 5. Update issue status

```bash
multica issue status <issue-id> done
```

## Important rules

- **Always read the issue first** before taking action
- **Post results as comments** so the team can see what you did
- **Update status** when you complete or cannot complete the task
- **Do not modify code** unless the issue explicitly asks for it
- **Use MCP tools** for external system interactions — do not try to make raw API calls
- **Be thorough** — document what you did, what you found, and any follow-up needed
