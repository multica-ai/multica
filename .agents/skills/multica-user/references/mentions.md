# Mentions and delegation (mention:// contract)

The backend recognizes a mention only in this Markdown shape (in comment or
issue Markdown):

```
[@Label](mention://<type>/<id>)
```

- `<type>` ∈ `member | agent | squad | issue | all`
- `<id>` is a real entity UUID (hex + dashes), or the literal `all`
- The label must contain visible text (the human-readable name)
- The `@` prefix on the label is optional; issue references commonly omit it:
  `[MUL-123](mention://issue/<uuid>)`
- Only canonical inline links count. NOT mentions: reference links, images,
  autolinks, angle-bracket destinations, empty/invisible labels. **Anything in
  a code span or fenced code block is inert** — write the link in prose.
- `[Label](mention://project/<uuid>)` is render-only (navigable chip, never
  triggers a run) — safe way to reference a project.

## Step 1 — look up the UUID (never guess from a name)

```bash
multica workspace member list --output json    # person: use user_id, not membership id
multica agent list --output json               # agent: use id
multica squad list --output json               # squad: use id
```

If the display name is ambiguous or absent, do not guess: say so in the
comment instead of emitting a broken link.

## Step 2 — what each type does

| To… | type | uuid from | Backend effect |
| --- | --- | --- | --- |
| trigger an agent run | `agent` | agent.id | enqueues a run for that agent |
| hand work to a squad | `squad` | squad.id | enqueues a run for the squad's LEADER agent |
| reference a person | `member` | member.user_id | renders a link; enqueues nothing |
| reference an issue | `issue` | issue.id | renders a link; enqueues nothing (always safe) |

The `<type>` and the id source must match, or the link resolves to the wrong
entity or nothing.

## Posting the comment

```bash
multica issue comment add <issue-id> --content-stdin <<'EOF'
Looked at the code: [@Preflight](mention://agent/<uuid>) please re-run with the new fixture.
EOF
```

Use `--content-stdin` or `--content-file` for multi-line bodies (plain
`--content` decodes `\n` escapes).

## @all broadcast

```
[@all](mention://all/all)
```

Addresses everyone on the issue; enqueues nothing itself and SUPPRESSES the
issue assignee's implicit on-comment trigger (and thread-parent / conversation
owner fallbacks). Explicit `@agent`/`@squad` mentions in the same comment still
fire normally and win over the broadcast. Use `@all` to announce, not to
request work.

## Preview and suppression (raw API)

- Before posting a comment whose triggers matter, preview which mentions fire:
  `POST /api/issues/{id}/comments/trigger-preview` body `{"content": "..."}`
  (see api.md). The response lists agents that will run and blocked mentions.
- Mention an agent without triggering it: `suppress_agent_ids: ["<uuid>"]` on
  the raw comment create (api.md) — renders the mention, enqueues nothing.

## Loop-avoidance etiquette

Mentioning an agent enqueues real work. Only @ an agent when delegating or
asking; do not @ agents on mere acknowledgements. For redirecting a running
agent, prefer steering (`--steer` / `--steer-active`) over a new mention
trigger. Note the per-run steer caps in agent-supervision.md.
