---
name: multica-platform
description: "Use for Multica platform actions the runtime brief does not fully cover: issue and PR contracts, mentions, agents, squads, autopilots, projects, runtimes, skill import. Not for the product code you are working on."
user-invocable: false
allowed-tools: Bash(multica *), Bash(git *), Bash(gh *)
---

# Operating Multica

Your runtime brief owns the per-turn workflow: which issue you are on, when to
comment, what status to write. This skill owns the platform contracts behind
it — what a command actually does, what the server validates, and which writes
have consequences you cannot take back.

Read the invariants below, then open the reference(s) your task actually needs
— usually one, sometimes a few. Do not read them all.

## Routing

| Open | When the task is about |
|---|---|
| `references/issues.md` | Issues: PR linking vs close intent, reading a linked PR's state, metadata, custom properties, status side effects, sub-issues and stages, who else is running |
| `references/mentions.md` | Writing a `mention://` link: which types enqueue a run, which are inert, why one silently did nothing |
| `references/agents.md` | Creating, copying or debugging an agent definition: fields, secrets, MCP config, skill binding |
| `references/squads.md` | Squads: leader routing, roster, recording leader activity, why a squad did or did not run |
| `references/autopilots.md` | Autopilots: schedule / webhook / manual triggers, `create_issue` vs `run_only`, why one did not fire |
| `references/projects.md` | Projects and their durable resources (`github_repo`, `local_directory`, worktree mode) |
| `references/runtimes.md` | Runtimes, daemons, `repo checkout`, and the task CLI boundary |
| `references/skill-import.md` | Importing a skill into this workspace from a URL or a local archive |

Open what the task needs. A single-domain task usually needs one; a task that
crosses domains needs each domain it touches — creating a squad, assigning it an
issue, then writing a mention needs `squads.md`, `issues.md` and `mentions.md`,
and skipping one of those means acting on a contract you have not read.

What is never right is reading all eight because you are not sure. Each
reference states its own contracts in full and none depends on another, so
pick by domain and skip the rest.

## Invariants

These hold across every reference and are not repeated there.

**Read before you write.** Start with the read-only commands the reference you
opened names — most domains have a `list` and a `get` that take `--output json`
and have no side effects. Run those before any mutation. When a command's shape
is unclear, `multica <command> --help` beats guessing at flags.

**A name is not an id.** Mention links, assignment, and every `--*-id` flag take
a real UUID from the matching `list --output json`. Never type a display name
where an id belongs, and never invent a UUID: an id that is well-formed but
belongs to nothing fails in ways that read like a permission error, which sends
you debugging access when the real problem was the id.

**`--output json` writes to stdout; warnings and confirmations go to stderr.**
Do not merge them (`2>&1`) into anything that parses the output — that makes a
write which SUCCEEDED look like it failed, and invites a duplicate retry.

**Writes are real.** Creating, updating, deleting, assigning, commenting,
mentioning, triggering and status changes mutate durable workspace state or
start agent runs that cost real budget. Never run one to see what happens. When
the user has not asked for a specific mutation, propose it instead of making it.

**`--no-start` when you are only recording.** Assignment and status writes
normally enqueue a run. When the work is already underway and the write merely
records ownership or progress, pass `--no-start` on EVERY command in that flow —
suppressing the assignment alone does not suppress a later status update.

**Status is a category, not a literal.** A workspace may define custom statuses
beyond the built-ins; each inherits its category's platform behavior in full,
and the runtime brief lists this workspace's catalog. Read `status_category`
rather than matching `status` against built-in names.

**Comment reads stay bounded.** Scan the threads cheaply
(`--roots-only --summary --compact`), then expand only what matters
(`--thread <thread-id> --tail 30`). Never one unbounded pull — a wide read on a
busy issue costs more than the answer is worth and still buries the reply
bodies where triggers and instructions actually live.

## When behavior looks wrong

Classify before concluding: expected behavior, a configuration problem, a
product limitation, or an actual bug. Explain what the platform currently does
rather than defending it; when the behavior is technically correct but bad for
the user, say so and propose a scoped change.

Do not silently alter routing, briefing, or trigger behavior to make a complaint
go away. Those are product contracts, and changing one without confirmation
moves the surprise to somebody else.
