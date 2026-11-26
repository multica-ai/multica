# Issue sync runbook

`issue-sync` mirrors issues between a Multica workspace and one GitLab project.
It is an operator command: it talks to a live deployment over the authenticated
API, so it needs no database access and no migration.

From `server/`:

```bash
go run ./cmd/issue-sync --dry-run \
  --multica-url https://multica.example.com \
  --workspace-id <workspace-uuid> \
  --gitlab-url https://gitlab.example.com \
  --gitlab-project-id 42
```

Credentials come from flags or the environment: `MULTICA_TOKEN`,
`MULTICA_WORKSPACE_ID`, `GITLAB_TOKEN`. A GitLab token needs `api` scope on the
project; a Multica token needs to be able to read and write issues in the
workspace.

## Run it in this order

1. `--dry-run` with no other flags. It lists every pair that would change and
   every issue with no counterpart, and writes nothing.
2. Read the unpaired list. A first run against an existing board reports the
   whole board, because nothing is linked yet.
3. Run again without `--dry-run` to sync the pairs that already exist.
4. Add `--create-missing` only when you want the unpaired issues mirrored too.
   Without it, unpaired issues are reported and left alone.

## What it does not do

- It does not sync comments, attachments, assignees, or checklists. The
  comparison covers title, description, status, priority and due date.
- It does not sync `start_date`. GitLab CE accepts the field, drops it, and
  returns nothing, so including it would leave every issue that has one
  permanently "changed" and rewritten on every run.
- It does not pick a winner in a conflict. Two sides that disagree with equal
  timestamps are reported, because choosing silently is how a mirror loses an
  edit.

## How links survive

Each side carries a hidden HTML comment naming its counterpart, so a pair
survives a lost database, a fresh checkout, and a second machine, and needs no
mapping table. Both directions are read, so a marker that survived on only one
side still re-links the pair.

A counterpart created by `--create-missing` carries its marker from the create
call itself, so the next run links it instead of creating a second copy of it.

## Scheduling

The command is safe to run repeatedly: a second run over unchanged data decides
nothing changed and writes nothing. A run that hits per-pair errors still
finishes the rest of the board and exits non-zero, so a scheduled invocation
surfaces the failure instead of looking like a clean pass.
