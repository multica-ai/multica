---
name: multica-cli
description: Use the multica CLI to authenticate, manage workspaces, issues, agents, projects, skills, and daemon/runtime state.
---

# Multica CLI

Use the `multica` CLI instead of guessing API calls when you need to operate Multica from a coding session.

## When to use

- Managing issues, projects, agents, or skills
- Checking daemon or runtime health
- Bootstrapping a new machine with `multica setup`
- Importing a skill pack from GitHub or local files
- Looking up workspace members, runs, comments, or attachments

## Working style

- Start with `multica auth status` if login state is unclear.
- Prefer `--output json` when another tool or script will consume the result.
- Use `multica <group> <command> --help` before assuming flag names.
- Inspect `multica daemon status` and `multica daemon logs` instead of assuming the daemon is online.

## Authentication and setup

```bash
multica login
multica login --token
multica auth status
multica auth logout
multica setup cloud
multica setup self-host
multica config show
multica config set server_url https://api.example.com
multica config set app_url https://app.example.com
multica config set workspace_id <workspace-id>
```

- `multica login` opens a browser and stores a PAT in `~/.multica/config.json`.
- Use `multica login --token` in CI or headless environments.
- `multica setup cloud` and `multica setup self-host` are the fastest ways to bootstrap a machine.

## Workspaces and members

```bash
multica workspace list
multica workspace get <workspace-slug-or-id>
multica workspace members
multica workspace watch <workspace-id>
multica workspace unwatch <workspace-id>
```

Use this group to confirm which workspace you are operating in before creating or assigning work.

## Issues

```bash
multica issue list
multica issue list --status in_progress
multica issue list --limit 20 --output json
multica issue get <issue-id>
multica issue create --title "..." --description "..."
multica issue update <issue-id> --status in_review --priority high
multica issue assign <issue-id> --agent <agent-slug>
multica issue status <issue-id> --set done
multica issue search <query>
multica issue runs <issue-id>
multica issue rerun <issue-id>
multica issue comment list <issue-id>
multica issue comment add <issue-id> --content "..."
multica issue subscriber list <issue-id>
multica issue subscriber add <issue-id>
multica issue subscriber remove <issue-id>
```

Issue work is the center of most operational flows. List, inspect, assign, then check runs when an agent has already executed work.

## Projects

```bash
multica project list
multica project get <project-id>
multica project create --title "..." --icon "..."
multica project update <project-id> --status in_progress
multica project status <project-id> --set completed
multica project delete <project-id>
```

Use projects to group related issues or sprint workstreams.

## Agents

```bash
multica agent list
multica agent get <agent-slug>
multica agent create --name "..." --runtime-id <runtime-id>
multica agent update <agent-slug> --instructions "..."
multica agent archive <agent-slug>
multica agent restore <agent-slug>
multica agent tasks <agent-slug>
multica agent skills list <agent-slug>
multica agent skills set <agent-slug> --skill-ids <id1,id2>
```

Use agent commands when you need to inspect configuration, attach skills, or change an agent's working instructions.

## Skills

```bash
multica skill list
multica skill get <skill-id>
multica skill create --name "..." --description "..." --content "<markdown>"
multica skill update <skill-id> --content "<markdown>"
multica skill delete <skill-id>
multica skill import https://github.com/owner/repo/tree/main/path/to/skill
multica skill files list <skill-id>
multica skill files upsert <skill-id> --path SKILL.md --content "<markdown>"
multica skill files delete <skill-id> <file-id>
```

Use skill import when a ready-made GitHub skill pack already exists. Use `skill files` when editing a skill in place.

## Autopilots

```bash
multica autopilot list
multica autopilot get <autopilot-id>
multica autopilot create --title "..." --agent "<agent-name>" --mode create_issue
multica autopilot update <autopilot-id> --status paused
multica autopilot delete <autopilot-id>
multica autopilot runs <autopilot-id>
multica autopilot trigger <autopilot-id>
multica autopilot trigger-add <autopilot-id> --cron "0 9 * * 1-5" --timezone "America/New_York"
multica autopilot trigger-update <autopilot-id> <trigger-id> --enabled=false
multica autopilot trigger-delete <autopilot-id> <trigger-id>
```

Use autopilots for scheduled or recurring issue-creation workflows.

## Daemon and runtimes

```bash
multica daemon start
multica daemon start --foreground
multica daemon stop
multica daemon restart
multica daemon status
multica daemon status --output json
multica daemon logs
multica daemon logs -f
multica runtime list
multica runtime usage <runtime-id>
multica runtime activity <runtime-id>
multica runtime ping <runtime-id>
multica runtime update <runtime-id> --target-version <version>
```

- The daemon runs in the background by default.
- Use `--foreground` only when debugging startup or runtime registration problems.

## Attachments, repo checkout, and utility commands

```bash
multica attachment download <attachment-id>
multica repo checkout <repo-url>
multica version
multica update
```

Use `repo checkout` when preparing a local clone for agents to work against.

## Common flows

### Triage urgent work

```bash
multica issue list --priority urgent --output json
multica issue get <issue-id>
multica issue assign <issue-id> --agent <agent-slug>
multica issue runs <issue-id>
```

### Create and start a new agent task

```bash
multica issue create --title "Fix flaky CI" --description "Investigate failing smoke test"
multica issue assign <issue-id> --agent <agent-slug>
multica issue comment list <issue-id>
multica issue runs <issue-id>
```

### Import a reusable skill from GitHub

```bash
multica skill import https://github.com/owner/repo/tree/main/path/to/skill
multica skill list
multica agent skills set <agent-slug> --skill-ids <skill-id>
```

### Debug a daemon that is not picking up work

```bash
multica auth status
multica daemon status --output json
multica runtime list
multica daemon logs -f
```

## Gotchas

- Prefer `--output json` when the next step is another tool, script, or parser.
- `multica login` uses a browser by default; use `multica login --token` for headless environments.
- `multica daemon start` backgrounds itself unless `--foreground` is provided.
- Skill updates only affect newly started tasks. Already-running tasks continue with the earlier skill version.
- If a command shape is unclear, use `multica <group> <command> --help` instead of guessing flags.

## References

- Official CLI overview: `apps/docs/content/docs/cli.mdx`
- Detailed CLI and daemon guide: `CLI_AND_DAEMON.md`
