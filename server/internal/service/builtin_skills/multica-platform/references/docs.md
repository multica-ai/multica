# Docs

Docs are the workspace knowledge base: Markdown documents in a folder tree,
edited live by people in the app while you work on them. `multica doc` reads
and writes the same docs the app shows.

Docs can be turned off for a deployment. Then every `multica doc` command,
`doc list` included, fails with the generic permission error; `--debug` shows
the server's `docs_disabled` code. Tell the person Docs are not available here
instead of retrying.

## Read first

```bash
multica doc list --output json                     # every folder and doc
multica doc search "release runbook" --output json # titles and content
multica doc get <doc-id> --output json             # content plus current revision
multica doc get <doc-id> --output content          # Markdown only
multica doc comment list <doc-id>                  # open review threads
multica doc history <doc-id>                       # versions, newest first
multica doc history <doc-id> --version 3 --output content
```

Doc and folder arguments take a full id or a unique id prefix from
`doc list`. A doc's `revision` is what writes check against; a `version` is a
saved snapshot in its history. They are different numbers.

## Write

```bash
multica doc edit <doc-id> --old "<exact current text>" --new "<replacement>" --message "why"
multica doc edit <doc-id> --old-file ./old.md --new-file ./new.md --message "why"
multica doc update <doc-id> --content-file ./doc.md --base-revision <revision> --message "why"
multica doc create --title "Runbook" --folder <folder-id> --content-file ./doc.md --message "why"
multica doc move <doc-id> --folder <folder-id|root>
multica doc folder create --title "Ops" --parent <folder-id|root>
```

People may be typing in the doc while you write. Prefer `doc edit` for a
targeted change: it replaces the text you quote wherever it now is, and fails
only when that text is gone or appears more than once (quote more context, or
pass `--replace-all`). Use `--old-file` / `--new-file` for multi-line text or
anything with code or quotes, so the shell cannot change it. `doc update`
replaces the whole doc and needs the `--base-revision` you read with `doc get`.
Edits made since then are usually merged in, and the update is refused with
nothing written when you and someone else changed the same paragraph. It is
also refused, whatever paragraphs changed, when your revision is too old to
merge from or the live-editing service is unavailable. On any refusal, read the
doc again and redo your change on top of it; never retry the same body with a
newer revision, which would erase their work.

Pass `--message` on every `create`, `edit`, `update` and `restore`. It is what
the doc's history shows next to your name, and your run and issue are recorded
with it. Moves and folder commands do not take it.

## Review comments

Reviewers leave feedback as comment threads anchored to doc text. Before
revising a doc someone reviewed, run `multica doc comment list <doc-id>`.
Resolved threads are hidden unless you pass `--include-resolved`; each root
comment's `anchor_quote` is the text it was left on, and `--output json` has
the full comment text.

You can reply in an existing thread:

```bash
multica doc comment list <doc-id> --thread <comment-id> --output json   # one thread
multica doc comment add <doc-id> --parent <comment-id> --content-file ./reply.md
```

Starting a thread, resolving, reopening and deleting are reserved for people:
they answer `403` to agent credentials. A reply that @mentions an agent starts
a run for it, as an issue comment does. When someone @mentions you in a
thread, the run's prompt names the doc and the thread, and your reply goes
there. When you addressed threads while working on an issue, say which in
your issue comment, so the reviewer can resolve them.

## History and restore

```bash
multica doc history <doc-id>                                   # who saved what, and why
multica doc history <doc-id> --version 3 --output content      # one version's Markdown
multica doc restore <doc-id> --version 3 --base-revision <revision> --message "why"
```

A restore saves the chosen version as a new version, so the content it
replaces stays in the history. It replaces the whole doc, including every edit
made after that version, by anyone. To undo one change and keep the rest, use
`doc edit` instead. Restore only a doc you broke, or when the person asked for
that version back.

A restore is refused with nothing written when the doc changed after the
revision you pass, live edits included. Read the doc again and restore only if
the whole doc should still go back.

## Human-only actions

Deleting a doc or a folder answers `403` to agent credentials, and the CLI has
no delete command. When something should be deleted, say so in your comment
and leave it to a person. Do not empty a doc as a substitute.

There is no CLI command to upload an image or file into a doc. Link to where
the file is delivered instead.

## Linking

`[Doc title](mention://doc/<doc-id>)` is a clickable link with no side effect:
it notifies no one and starts no run.
