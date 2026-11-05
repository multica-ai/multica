# Local conversation protocols

Draft wire contracts for the Go-owned local conversation service. The plan is
in `docs/engineering/local-conversation-go-sqlite-plan.md`.

| Schema | Between | Plan section |
| --- | --- | --- |
| `schema/local-v2.schema.json` | daemon ↔ Desktop (HTTP / WS `/local/v2`) | §5, §7 |
| `schema/claude-adapter-v1.schema.json` | daemon ↔ per-conversation Claude adapter (stdio NDJSON) | §4.4 |

The schema files are the single definition source. Go and TypeScript types
will be generated from them once the generator is chosen; until then the
schemas are validated only by the golden samples.

## Golden samples

`testdata/<schema>/valid/<def>__<case>.json` must validate against
`$defs/<def>`, and `testdata/<schema>/invalid/<def>__<case>.json` must be
rejected. `go test ./internal/localconversation/protocol` checks both. Add a
sample with every schema change, including an invalid one when a constraint
is tightened.

## Conversation navigation

`GET /local/v2/sessions/{id}/navigation` is shared by local desktop, linked
desktops and mobile device links. Without a selector it reads only `node_count`.
Use one of `node_index`, `turn_index`, `before_index`, or `after_index` to read a
window. Centered windows accept `before`/`after` (default 4/5); adjacent pages
accept `limit` (default 10). At most 20 turns are returned. `include_body=false`
reads bounded node metadata without tool output. `directory_range` gives the
turn-index bounds and paging flags for this metadata, including ranges containing
no user nodes. It is independent of `range`, which is null when bodies are omitted.
Clients render all `node_count` dots, seed the hover list with the latest metadata
page, and page that list without changing the selected transcript window.
Send `store_generation` from
the current snapshot; a replaced store returns `generation_changed` (409).
Node indexes enumerate user messages, excluding hidden turns; they are not turn
indexes. Count, node mapping, body and `snapshot_seq` share one read transaction.
Reading a window never advances the event subscription cursor. Clients keep
disconnected body ranges distinct and locate by message ID after rendering.
The hosting device advertises `conversation-navigation-v1` in `/hello`.
Clients use their existing history navigation on devices without this capability.

## Event model summary

- Events are persisted with a per-conversation `seq`; `turn_seq` keeps the
  per-turn order the GUI uses.
- Output is grouped into items by `item_id`. `item.delta` appends text;
  `item.updated` replaces state without ending the item (background tasks);
  `item.completed` is the single final event with the complete item state.
- `user.message`, `turn.completed`, `item.completed` and
  `interaction.resolved` are final; everything else is not.
- The Claude adapter forwards raw SDK messages (`sdk_message`); the daemon
  normalizes them into these events.
