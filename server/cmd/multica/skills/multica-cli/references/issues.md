# Issues and run side effects

## Read before writing

```bash
multica issue get <issue-id> --output json
multica issue metadata list <issue-id> --output json
multica issue search <query> --include-closed --output json
multica issue comment list <issue-id> --roots-only --summary --compact --output json
multica issue comment list <issue-id> --thread <comment-id> --tail 30 --compact --output json
```

Scan comment roots, then expand the relevant threads. `--recent N` limits
threads, not individual comments, so it can still return large histories.
Resolved threads are folded by default; use `--full` only when needed.

Before creating or assigning repeatable code work, read
`multica skills read multica-cli references/workspaces.md`. Bind the issue to
the intended project; a project name in prose does not attach its resources.

## Authorized changes

Write long bodies into UTF-8 files inside the working directory. Passing a
file preserves newlines, backticks and shell expressions without interpolation:

```bash
multica issue create --title "<title>" --project <project-id> --description-file ./description.md --output json
multica issue comment add <issue-id> --parent <trigger-comment-id> --content-file ./reply.md
```

When a comment triggered the task, reply to that comment with `--parent`.
For a new top-level discussion, omit it. Do not attach replies to unrelated
threads. File arguments outside the working directory are rejected; use
`--allow-external-file` only for an outside file the user specifically chose.

Status and assignment changes can enqueue runs. For bookkeeping on work that
is already underway, use `--no-start`:

```bash
multica issue update <issue-id> --status <status-key> --no-start
multica issue assign <issue-id> --to-id <agent-id> --no-start
```

Use the issue's actual workflow status keys. A project workflow can attach a
handler to a status and turn entering it into a handoff; do not assume a
built-in status or a status name's meaning applies to every project. Omit
`--no-start` only when starting the corresponding work is intended.

Mention links have effects: `mention://agent/<id>` and
`mention://squad/<id>` can enqueue work; `mention://member/<id>` notifies a
person, and `mention://all/all` broadcasts. Do not mention an agent to thank it
or acknowledge its reply. Issue/project mention links are navigational.

## Inspect outcomes

```bash
multica issue runs <issue-id> --output json
multica issue run-messages <run-id> --issue <issue-id> --output json
multica issue pull-requests <issue-id> --output json
```

Inspect execution evidence before rerunning or cancelling work. Those actions
must be requested or otherwise authorized; never launch a paid probe just to
verify configuration. Report successful writes separately from actual run
results. A recorded project binding is not proof a run used that directory.
