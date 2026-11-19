# Inbox

The inbox holds a workspace member's notifications: assignments, mentions,
comments, status changes, failed runs. `multica inbox` reads and changes the
same inbox the app shows, so every change appears in the member's open
windows right away.

## Whose inbox, and when

A task acts on the **runtime owner's** inbox, never anyone else's; no flag
selects another person. The server allows it only while the task is running
and its verified upstream human, the person who triggered it directly or
through a delegation chain, is that owner. A run someone else triggered, a
run without a verifiable human, and cloud-node credentials are refused with
`403` on every inbox command, reads included. That refusal is final for this
run: do not retry with another credential. Tell the person the inbox is only
available in runs they start themselves.

Notifications cannot be deleted from the CLI, and the server refuses deletion
from agent credentials. Archive instead; it is reversible.

## Read first

```bash
multica inbox count --output json                  # unread here and per workspace
multica inbox list --unread --output json          # active notifications
multica inbox list --since 1d --issue MUL-123 --output json
multica inbox list --archived --output json        # archived instead
multica inbox list --snoozed --output json         # snoozed issues instead
```

Each row has the notification `id` (the handle every write takes), `type`,
`title`, `issue_id`, `issue_identifier` (MUL-123), `actor` (`member:Name` or
`agent:Name`), `summary` (the comment or change, shortened), `comment_id`
when it points at a comment, `read` and `created_at`. Listing never changes
read state. `issue_identifier` and `actor` stay empty when their lookup fails;
the row is still valid.

The active list has one row per notification, while the app shows one row per
issue, so several rows can share an issue. The archived list has one row per
issue and is read page by page to the end, so an issue archived long ago is
still found by `--issue`.

## Change

```bash
multica inbox read <id>...                  # and the rest of each issue
multica inbox read --issue MUL-123          # every notification on one issue
multica inbox unread <id>...                # only that notification
multica inbox archive <id>...               # and the rest of each issue
multica inbox unarchive <id>...
multica inbox snooze <id>... --until 3h     # RFC 3339, 30m, 3h, 1d, 1w, tomorrow
multica inbox unsnooze <id>...
```

Ids must be full UUIDs from `list --output json`. Every id is checked before
the first request; after that, a failure on one id does not stop the others.
The JSON result lists `items` that succeeded, each with its own `count`, and
`failed` with each server error; the command exits non-zero when any failed.
Reading or archiving one notification also reads or archives every other
notification on its issue, as opening or archiving it in the app does. So
`count` is the number of notifications actually changed, whole issues
included: a second id on an issue already handled, or a change already in
place, counts 0.

Snoozing takes the whole issue out of the inbox until the time. It comes back
early when someone mentions the owner by name, assigns it to them, a run
fails, an app asks for their action, or all its sub-issues finish. A day or more chosen between 22:00 and
6:00 comes back at 9:00, as in the app.

## Bulk changes

```bash
multica inbox read --all --yes          # mark everything read
multica inbox archive --read --yes      # issues whose newest notification is read
multica inbox archive --completed --yes # issues in a done or closed status
multica inbox archive --all --yes       # everything
```

Without `--yes` they ask for confirmation, and a run, which cannot answer,
fails without changing anything. `count` is the number of notifications the
server changed. `read --all`, `archive --all` and `archive --read` leave
snoozed issues alone; `archive --completed` also archives a done or closed
issue while it is snoozed and ends that snooze, as in the app.

Only run a bulk change when the person asked for that sweep. To clear what
you judged handled, pass those ids; `--all` also takes notifications that
arrived after you listed.

## Going through the inbox for someone

1. `inbox list --unread --output json`, then read the issues that matter.
2. Decide per notification, and say what you decided: handled (`read` or
   `archive`), later (`snooze` or `unread`), needs them (leave it).
3. Apply with the ids, then `inbox list --unread` again to confirm.
4. Report counts and what you left for them, by issue key.
