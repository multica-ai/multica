# Projects and resources

A project groups work and carries durable resources. A resource is not just
display metadata; it is context later injected into task briefs and
`.multica/project/resources.json`. A project also carries markdown notepads,
whose contents — unlike resources — are stored by Multica itself.

- [Core model](#core-model)
- [CLI](#cli)
- [local_directory execution modes](#local_directory-execution-modes)
- [Referring to a project in a comment](#referring-to-a-project-in-a-comment)
- [When to add a resource](#when-to-add-a-resource)
- [Notes](#notes)
- [Debugging wrong context](#debugging-wrong-context)
- [Side effects](#side-effects)

## Core model

Projects are durable context containers. Resources attached to a project can
affect future agent tasks.

```bash
multica project list --output json
multica project get <project-id> --output json
multica project resource list <project-id> --output json
```

Project resources are mutated through project resource commands/endpoints. Issue
comments do not create durable project resources.

A project's `description` is also durable context: when an issue (or a
quick-create task) is bound to a project, the project description is injected
into the agent's brief under `## Project Context` and written to
`.multica/project/resources.json` as `project_description`. Use it for
project-wide rules/context that should apply to every task in the project.

Common resource types:

- `github_repo` — durable GitHub repo context, with `resource_ref.url`, optional
  checkout `ref`, and optional prompt-only `default_branch_hint`;
- `local_directory` — daemon-local path context, with `resource_ref.local_path`,
  `daemon_id`, optional label, and optional `execution_mode` (`in_place`, the
  default, or `worktree`).

## CLI

```bash
multica project list --output json
multica project get <project-id> --output json
multica project create --title "<title>" --repo <github-url> --output json
multica project create --title "<title>" --start-date 2026-03-01 --due-date 2026-03-31 --output json
multica project update <project-id> --title "<title>" --output json
multica project update <project-id> --due-date 2026-04-15 --output json
multica project update <project-id> --start-date "" --output json   # clear the start date
multica project status <project-id> in_progress --output json
multica project resource list <project-id> --output json
multica project resource add <project-id> --type github_repo --url <github-url> --output json
multica project resource add <project-id> --type github_repo --url <github-url> --ref <branch-or-sha> --output json
multica project resource add <project-id> --type local_directory --local-path <abs-path> --daemon-id <daemon-id> --output json
multica project resource add <project-id> --type local_directory --local-path <abs-path> --daemon-id <daemon-id> --execution-mode worktree --output json
multica project resource update <project-id> <resource-id> --execution-mode in_place --output json
multica project resource update <project-id> <resource-id> --url <new-github-url> --output json
multica project resource update <project-id> <resource-id> --ref <branch-or-sha> --output json
multica project resource remove <project-id> <resource-id> --output json
```

For `github_repo`, non-JSON `--ref` sets `resource_ref.ref`, the default
checkout branch/tag/SHA for future tasks in that project. JSON `--ref '<json>'`
remains the escape hatch for full payloads or resource types not covered by
shortcuts. `project resource update` merges shortcut edits with the existing
`resource_ref`, so a partial edit does not clobber required fields.

`--start-date` / `--due-date` are optional calendar days (`YYYY-MM-DD`, like
issue dates). On `project update`, pass an empty string (`--start-date ""`) to
clear a date; an unset flag leaves it untouched.

## local_directory execution modes

`--execution-mode` decides how tasks share a `local_directory`.

`in_place` (default) runs the agent in the user's directory, one task at a time;
a second task waits in `waiting_local_directory`.

`worktree` gives each task its own git worktree of that repo, so tasks run
concurrently and each delivers its work as a branch in the user's repo instead
of editing the working copy. Every task of one conversation shares that branch —
`agent/<agent>/<issue>` for an issue, `agent/<agent>/chat-<session>` for a chat
— and each turn's worktree starts from the previous turn's work rather than from
`HEAD`; a task with no conversation behind it gets `agent/<agent>/<task>`.

Continuation is decided by an ownership record
(`refs/multica/local-state/<branch>`, which holds the owning conversation, the
snapshot of the user's directory the branch already carries, and the branch tip
it was recorded at), never by the branch name. A same-named branch the user
created — or one that no longer contains the recorded commit, i.e. deleted and
recreated or force-moved — is left alone and the task falls back to
`agent/<agent>/<issue>-<id>`.

A turn replays only what the user changed since that snapshot; when those edits
conflict with the branch's own work the worktree is handed to the agent
mid-merge and the run delivers nothing until the agent resolves it.

`worktree` requires the path to be a git repository with at least one commit;
tasks fail with an explicit error otherwise. The gate is the `local-worktree-v1`
capability the daemon advertises — not its version string — and it is checked
twice: at save time, and again against the daemon that claims each task, so a
machine whose runtime cannot do worktrees gets its tasks cancelled rather than
run in place. Saving `worktree` is refused (HTTP 422, code
`daemon_version_unsupported`) while the daemon on that machine does not
advertise the capability — the fix is updating the Multica app there, then
retrying. Pass an empty value to clear it back to the default.

## Referring to a project in a comment

A project has no `MUL-123`-style identifier, so writing its title as prose
produces dead text — there is nothing for the reader's client to autolink. Use
the mention-link form instead, with the project UUID from
`multica project list --output json`:

    [Roadmap](mention://project/<project-id>)

Every client makes it navigable, with different presentation: web and desktop
render a chip carrying the project's icon and current title, while mobile
renders an ordinary link that opens the project on tap. Unlike `@agent` /
`@squad`, it is a pure link: the mention parser does not recognize `project` at
all, so it enqueues nothing and notifies nobody — the same no-side-effect
contract as an `issue` mention.

Prefer this form over pasting the project's URL. Web and desktop do unfurl a
bare in-app project URL into that same chip, but mobile does not — there a
pasted URL is handed to the system browser and takes the reader out of the app.

## When to add a resource

Add/update a project resource when the user asks for durable project context:
"把这个 GitHub repo 绑到项目上", "以后都用这个 repo", "agent 总是拿不到这个项目的
仓库", or "这个项目要在我的本地目录里跑".

Project resources are durable and affect future tasks. `multica repo checkout`
is task-local checkout state.

## Notes

Markdown notepads attached to a project. A project can have as many as its
members want: one for a daily journal, one for conclusions, one for scratch
working notes. Unlike a resource, which only points at something living in
another system, a note's content is stored by Multica.

### Their contents are not in your brief

When a task belongs to a project, the brief's Project Context section names the
note commands and the project id — so you know the notepads exist. What it does
NOT contain is any note's contents, or even the list of titles. Both grow with
the number of notepads, and the brief is injected verbatim on every task with no
length ceiling, so carrying them would let that section crowd out the task
itself.

The consequence: you do not know what notes exist until you list them. If a task
smells like it depends on prior project context ("continue the migration", "what
did we conclude about X"), listing is cheap and reading one pad is cheaper than
re-deriving the answer.

### Reading

```bash
multica project note list <project-id>
multica project note get <project-id> <note-id>
```

`list` returns titles, byte sizes, and last-updated times — never bodies. That
is what makes it safe to call speculatively: a project with thirty long notes
still lists in a few hundred bytes.

The ids `list` prints are abbreviated. Pass them straight back to `get` /
`append` / `update` / `delete` — those commands resolve an abbreviated id the
same way `<project-id>` is resolved. An ambiguous prefix is reported as such
rather than guessed at; `--full-id` prints untruncated ids if you want them.

`get` prints the raw markdown body to stdout by default (no JSON envelope to
unwrap). Pass `--output json` if you need the metadata alongside it.

### Writing

```bash
# Add to a pad, keeping what is already there — use this for a journal
multica project note append <project-id> <note-id> --body "## 2026-08-07
Shipped the migration. Rollback plan in the runbook."

# Create a new pad (body optional — omit it for an empty one)
multica project note create <project-id> --title "Release journal"

# Replace the entire body (destructive)
multica project note update <project-id> <note-id> --body @final.md
```

`--body` accepts three forms: literal text, `@path` to read a file, or `-` to
read stdin. Use the file or stdin form for anything multi-line — shell-quoting a
markdown document is a good way to lose a backtick. A literal leading `@` is
escaped as `@@`.

### append vs update

Reach for `append` by default. The concatenation happens server-side in a single
SQL statement, so if a human is editing the pad in the web UI while you append,
both survive. `update` is a read-modify-write from your side: it replaces the
body with exactly what you send, discarding anything added since you last read
it.

`update` is the right call only when you genuinely mean "this pad's content is
now this" — rewriting a conclusions page after the conclusion changed, for
instance. If you are adding an entry, `append`.

### What belongs in a note

The mechanics above are contract. This part is judgment.

A note is for context that outlives one issue and belongs to the project:
decisions and why they were made, a chronological record of what shipped,
conclusions that later tasks should not have to re-derive, environment quirks
specific to this project's work.

It is not a place for: per-issue status (that is a comment on the issue), things
only this run cares about, secrets or tokens of any kind, or a transcript of
what you just did — if it reads like a log, it probably does not earn a
permanent home.

### Errors you may hit

- `project note not found` — the note id is real but belongs to a different
  project or workspace. Both are checked; guessing an id from elsewhere fails.
- `title is required and must be at most 200 characters` — `create` will not
  invent a title. The limit counts characters, not bytes, so a 200-character
  Chinese title is accepted. An untitled pad in a list of twenty is unfindable.
- `--body resolved to empty content` — an append whose body is whitespace-only
  is rejected rather than silently appending blank lines.

## Debugging wrong context

1. `multica project get <project-id> --output json`.
2. `multica project resource list <project-id> --output json`.
3. Check `github_repo.resource_ref.url`, optional `ref`, `default_branch_hint`,
   and `local_directory.resource_ref.daemon_id`.
4. Updating resources is a durable mutation. After an update, listing the
   resource is the verification path.
5. If resources match the expected task context, inspect runtime/repo checkout
   path next.

## Side effects

Project create/update/delete/status and project resource add/update/remove
mutate durable workspace state and affect future tasks. Ask before changing
`local_directory` unless the user explicitly requested that exact local path.

`project note create/append/update/delete` is also durable: a note outlives the
task that wrote it and every later task in the project can read it. `update` and
`delete` discard content you cannot recover — prefer `append`, and do not delete
a pad you did not create without being asked to.

Deleting a project deletes its notes with it, in the same transaction.
