---
name: acli-atlassian
description: Use when an agent needs to interact with JIRA or Confluence via the Atlassian CLI (acli). Covers authentication verification, JIRA work item operations (view/create/edit/transition/search/comment), JIRA project and sprint management, and Confluence page/space/blog operations. Trigger on any task that mentions JIRA, Confluence, Atlassian, work items, tickets, sprints, or boards.
---

# Atlassian CLI (acli) Skill

**Trigger on any task mentioning JIRA, Confluence, Atlassian, work items, tickets, sprints, or boards.**

This skill covers interacting with JIRA and Confluence via the `acli` CLI tool.

## Authentication Verification

Before running any commands, verify authentication is working:

```bash
acli jira --action getServerInfo
acli confluence --action getServerInfo
```

If these fail, check that `~/.acli` or the relevant config file has valid credentials (URL, username, API token or password).

## JIRA Work Item Operations

### View an issue

```bash
acli jira --action getIssue --issue PROJ-123
```

### Create an issue

```bash
acli jira --action createIssue \
  --project PROJ \
  --type Bug \
  --summary "Short summary" \
  --description "Detailed description"
```

### Edit/update an issue

```bash
acli jira --action updateIssue --issue PROJ-123 \
  --summary "Updated summary" \
  --description "Updated description"
```

### Transition (change status of) an issue

```bash
# List available transitions
acli jira --action getTransitions --issue PROJ-123

# Perform a transition by name
acli jira --action transitionIssue --issue PROJ-123 --transition "In Progress"
```

### Search for issues (JQL)

```bash
acli jira --action getIssueList \
  --jql "project = PROJ AND status = 'In Progress' AND assignee = currentUser()" \
  --outputFormat 2
```

### Add a comment to an issue

```bash
acli jira --action addComment --issue PROJ-123 \
  --comment "Your comment text here"
```

## JIRA Project and Sprint Management

### List projects

```bash
acli jira --action getProjectList
```

### List sprints for a board

```bash
acli jira --action getSprintList --board 42
```

### Add issue to a sprint

```bash
acli jira --action addIssuesToSprint --sprint 99 --issue PROJ-123
```

## Confluence Page/Space/Blog Operations

### Get a page by title

```bash
acli confluence --action getPage --space MYSPACE --title "Page Title"
```

### Create a page

```bash
acli confluence --action addPage --space MYSPACE \
  --title "New Page Title" \
  --content "Page body in storage format or wiki markup"
```

### Update a page

```bash
acli confluence --action updatePage --space MYSPACE \
  --title "Existing Page Title" \
  --content "Updated content"
```

### Create a blog post

```bash
acli confluence --action addBlogEntry --space MYSPACE \
  --title "Blog Post Title" \
  --content "Blog body"
```

### List spaces

```bash
acli confluence --action getSpaceList
```

## Tips

- Use `--outputFormat 2` with `getIssueList` for a more readable table output.
- API tokens are preferred over passwords for authentication. Set them in the acli config.
- For large JQL result sets, use `--limit` and `--startAt` for pagination.
- Run `acli jira --help` or `acli confluence --help` to see all available actions.
