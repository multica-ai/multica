# Inbox, wakeups, PR links

## Inbox (raw API — the CLI has no inbox commands)

Needs a user PAT and the `X-Workspace-ID` header (see api.md):

```bash
curl -s "$MULTICA_API_URL/api/inbox" \
  -H "Authorization: Bearer $MULTICA_TOKEN" \
  -H "X-Workspace-ID: <workspace-uuid>"
```

| What | Endpoint |
| --- | --- |
| List inbox items | `GET /api/inbox` |
| Unread count | `GET /api/inbox/unread-count` |
| Cross-workspace unread summary | `GET /api/inbox/unread-summary` |
| Mark one read/unread | `POST /api/inbox/{id}/read` / `/unread` |
| Mark all read | `POST /api/inbox/mark-all-read` |
| Archive one / all-read / completed | `POST /api/inbox/{id}/archive`, `POST /api/inbox/archive-all-read`, `POST /api/inbox/archive-completed` |
| Archived list | `GET /api/inbox/archived` |

Typical flow for "anything new?": unread-count → list → open the referenced
issue with the CLI → mark read only with user confirmation.

## Scheduled wakeups (multica CLI)

```bash
multica issue wakeup list <issue-id> --output json
multica issue wakeup create <issue-id> ...      # fetch one existing wakeup first and mirror its shape
multica issue wakeup trigger <issue-id> <wakeup-id>   # wake the target now
multica issue wakeup runs <issue-id> <wakeup-id>
multica issue wakeup disable / delete / checkin ...
multica issue wakeup events                     # supported event types and filters
```

Wakeups re-invoke an issue's agent on a schedule or condition — the tool for
"have the agent follow up tomorrow / when the PR merges".

## Pull requests

```bash
multica issue pull-requests <issue-id>          # list linked PRs (CLI)
```

Linking/unlinking and auto-complete are raw API (see api.md):

| What | Endpoint |
| --- | --- |
| Link a PR | `POST /api/issues/{id}/pull-requests` |
| Unlink | `DELETE /api/issues/{id}/pull-requests/{prId}` |
| Auto-complete on merge | `PUT /api/issues/{id}/pr-auto-complete` |

Useful alongside a git hosting CLI: find the MR/PR URL, link it, and set
auto-complete so the issue closes itself on merge.
