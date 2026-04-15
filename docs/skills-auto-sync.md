# Skills Auto-Sync

Multica can sync a local skills library into a watched workspace automatically through the daemon.

This is useful if you already keep skills in a local directory such as `D:\xiaotian-skills` and want workspace skills to stay up to date without manually importing or editing them in the app.

## How It Works

For each watched workspace with skill sync enabled, the daemon:

1. Scans one local directory.
2. Treats each child directory containing `SKILL.md` as one skill.
3. Syncs `SKILL.md` plus UTF-8 text supporting files.
4. Reconciles those local skills into the existing workspace skill records in Multica.

The daemon runs an automatic background sync for watched workspaces where `skill_sync.enabled=true`.

You can also force a sync manually at any time.

## Setup

1. Watch the workspace:

```bash
multica workspace watch <workspace-id>
```

2. Point that workspace at your local skills directory:

```bash
multica workspace skills-sync set <workspace-id> --dir D:\xiaotian-skills
```

3. Start or restart the daemon:

```bash
multica daemon start
```

Once the daemon is running, it will perform an initial sync and then continue syncing in the background.

## Commands

Configure automatic sync for a watched workspace:

```bash
multica workspace skills-sync set <workspace-id> --dir D:\xiaotian-skills
```

Allow automatic deletion of daemon-managed skills that were removed locally:

```bash
multica workspace skills-sync set <workspace-id> --dir D:\xiaotian-skills --delete-managed
```

Show current sync status:

```bash
multica workspace skills-sync status [workspace-id]
```

Force one immediate sync:

```bash
multica workspace skills-sync run [workspace-id]
```

Disable automatic background sync:

```bash
multica workspace skills-sync disable <workspace-id>
```

`multica workspace skills-sync run` still works even if automatic sync is disabled, as long as a directory is configured.

## Status Fields

`multica workspace skills-sync status` shows:

- `enabled`
- `dir`
- `delete_managed`
- `last_sync_at`
- `last_sync_error`

`last_sync_error` is useful when the daemon cannot scan the directory or cannot reconcile changes with the server.

## V1 Rules and Limits

V1 is intentionally conservative.

- Only child directories containing `SKILL.md` are treated as skills.
- Supporting files must be UTF-8 text.
- Binary files are skipped in V1.
- Generated or junk directories and files are skipped, including common entries such as `__pycache__`, `node_modules`, `dist`, `build`, `.next`, `coverage`, dot-directories, and `.DS_Store`.

## Deletion Safety

Auto-deletion is limited on purpose.

- Only daemon-managed skills are auto-updated or auto-deleted.
- Manual workspace skills are preserved.
- Auto-deletion only applies to daemon-managed skills from the same sync source.
- Auto-deletion happens only when `--delete-managed` is enabled.

This prevents local sync from removing unrelated skills created manually in the workspace.

## Example Layout

Example local library:

```text
D:\xiaotian-skills
├── code-review
│   ├── SKILL.md
│   └── checklist.md
├── deploy-playbook
│   ├── SKILL.md
│   └── runbook.md
└── onboarding
    └── SKILL.md
```

In this layout:

- `code-review`, `deploy-playbook`, and `onboarding` are synced as separate workspace skills.
- `SKILL.md` becomes the main skill content.
- text files such as `checklist.md` and `runbook.md` are synced as supporting files.

## Troubleshooting

If a sync does not do what you expect:

1. Check status:

```bash
multica workspace skills-sync status <workspace-id>
```

2. Run one manual sync:

```bash
multica workspace skills-sync run <workspace-id>
```

3. Inspect `last_sync_error`.

Common causes:

- the configured directory does not exist
- a workspace is not being watched
- the daemon is not running for automatic sync
- the CLI is not authenticated
