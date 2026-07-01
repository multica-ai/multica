---
name: acli-atlassian
description: Use when an agent needs to interact with JIRA or Confluence via the Atlassian CLI (acli). Covers authentication verification, JIRA work item search/view/create/edit/transition, JIRA project and sprint management, and Confluence operations. Trigger on any task that mentions JIRA, Confluence, Atlassian, work items, tickets, sprints, or boards.
---

# Atlassian CLI (acli) Skill

**Trigger on any task mentioning JIRA, Confluence, Atlassian, work items, tickets, sprints, or boards.**

This skill covers interacting with JIRA and Confluence via the `acli` CLI tool.

> **Note:** `acli` uses a subcommand structure (`acli jira <resource> <verb>`), NOT the legacy `--action` flag style. Run `acli jira --help` or `acli jira workitem --help` to discover available subcommands.

## Authentication Verification

Before running any commands, verify authentication is working:

```bash
acli jira workitem search --jql "project = AIPLAT"
```

If this fails, check that the acli config has valid credentials (URL, username, API token).

## JIRA Work Item Operations

### Search for issues (JQL)

```bash
acli jira workitem search --jql "project = PROJ AND status = 'In Progress'"
```

### View an issue

```bash
acli jira workitem view PROJ-123
```

### Create an issue

```bash
acli jira workitem create --project PROJ --type Bug --summary "Short summary"
```

### Transition (change status of) an issue

```bash
acli jira workitem transition PROJ-123 "In Progress"
```

## Tips

- Run `acli jira --help` to see all available resource types.
- Run `acli jira workitem --help` to see all work item subcommands.
- Run `acli confluence --help` to see Confluence subcommands.
- API tokens are preferred over passwords for authentication. Set them in the acli config.
