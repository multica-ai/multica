# Jira Link

Links a Multica issue to a Jira issue and moves the Jira issue along as the
Multica one changes status. It is built on the Plugin Action API only — no
changes to Multica itself beyond the issue metadata write action it uses.

## What it does

| Hook | Trigger | What happens |
| --- | --- | --- |
| `link_issue` | `ui`, `manual` | Checks the key exists in Jira, then records the link on the issue. |
| `create_issue` | `ui`, `manual` | Creates a Task in the configured project from the issue's title and description, labelled `multica-<issue id>`, then records the link. A retry finds the labelled issue instead of creating a second one. |
| `sync_status` | `event` (`issue.status_changed`) | Moves the linked Jira issue into the matching status category. |

The panel shows the link and offers **Link**, **Create in Jira** and **Unlink**.

## Where the link lives

The link is issue metadata, written through
`PUT /v1/issues/{issue_ref}/metadata/{key}`:

| Key | Value |
| --- | --- |
| `jira.key` | `OPS-42` |
| `jira.url` | `https://acme.atlassian.net/browse/OPS-42` |
| `jira.status` | The Jira status name the last write saw |

Metadata is what agents already read for pipeline state, so an agent working the
issue sees the Jira key without a plugin-specific lookup. Writing the value that
is already stored is a no-op: no new revision, no event, no agent wake-up — so
a handler can re-record a link safely on retry.

## Status mapping

Jira workflows differ per project, but every Jira status sits in one of three
fixed categories. The handler maps onto the category and picks the first
available transition into it, so it works against any workflow:

| Multica status | Jira status category |
| --- | --- |
| `backlog`, `todo` | `new` (To Do) |
| `in_progress`, `in_review` | `indeterminate` (In Progress) |
| `done` | `done` |
| `cancelled`, `blocked`, anything else | left alone |

If the Jira issue is already in the target category nothing is sent.

## Running the handler

```bash
MULTICA_SIGNING_SECRET=whsec_… \
JIRA_SITE=https://acme.atlassian.net \
JIRA_EMAIL=bot@acme.com \
JIRA_API_TOKEN=… \
node server/handler.mjs
```

Hook results are always `200`; an expected problem such as an unknown key comes
back as `{ "error": "…" }` so the panel can show it. The host treats any other
status as a failed hook and does not pass the body on.

The Jira site and credentials are **environment of the handler**, not plugin
config. Config is editable by any workspace admin and is sent on every hook
call; a site URL there would let an admin point the stored credentials at a host
of their choosing. The manifest's `net:` scope covers only the handler's own
host — Multica never talks to Jira.

Signature verification and replay protection follow
[`triage-notify`](../triage-notify/README.md); read that first if you are
writing a handler.

## Who the change is attributed to

A `ui` or `manual` call writes metadata as the person who pressed the button,
with `via_plugin_id` on the `issue:metadata_changed` event naming the plugin. An
`event` call writes as the plugin installation.

## Not in this example

- Jira → Multica sync. That needs an inbound Jira webhook and a standing
  credential (the `mpi_` install token), because the hook callback token only
  lives for one request.
- Comment mirroring and field mapping beyond status.
