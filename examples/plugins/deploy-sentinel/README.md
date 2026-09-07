# Deploy Sentinel

A production-incident plugin. It correlates an incident issue with recent
deploys, lets an agent file a rollback request under the team's own rules, pages
on-call when an incident is filed, and adopts the team's existing metrics MCP
server.

It exists because the other examples in this directory each demonstrate one
thing. This one is the shape of a plugin somebody would actually write: four
triggers, both transports, real business rules that refuse things, and a skill
that teaches an agent when to use any of it.

## What it contributes

| Contribution | Kind | What it demonstrates |
| --- | --- | --- |
| `incident-response` | skill resource | A `SKILL.md` becomes an ordinary workspace skill. Uninstall removes it. |
| `correlate_deploys` | hook, `agent` + `manual` + `ui` | One hook, three callers. The agent gets a tool; a person gets a button; the panel gets a function. |
| `request_rollback` | hook, `agent` + `manual` | A write action that **refuses**: too old, or no written reason. |
| `page_oncall` | hook, `event` | Fires on `issue.created` / `issue.status_changed`. Never blocks anything. |
| `metrics` | hook, `mcp` transport | Adopts an external MCP server's tools, subject to admin approval. |
| `incident` | `issue_panel` surface | The human view, in a sandboxed iframe with no credential. |

## The part worth copying

**`request_rollback` says no.** It refuses a deploy past the configured window,
and it refuses a reason shorter than 20 characters. An agent that has to write
evidence before it can file a change request produces better change requests —
and the refusal text explains why, so the agent can act on it rather than retry
blindly. Hook handlers that only ever succeed are the easy case; this one is the
useful one.

**`correlate_deploys` reports "nothing changed" as an answer**, not as an empty
list. The skill then tells the agent that an empty result means it should stop
looking at deploys. A tool that returns `[]` and a tool that says "no deploys in
this window, this is unlikely to be a deploy" cost the same to write and behave
very differently in an agent's hands.

**The skill and the hooks are designed together.** `SKILL.md` names the tools and
says what order to use them in. Neither half works as well alone.

## Running it locally

Two servers, both the plugin author's own. Multica never runs either.

Both serve HTTPS, because a hook's transport URL must be an `https://` URL or
the manifest will not install. Make a certificate once:

```bash
openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
  -keyout dev-key.pem -out dev-cert.pem \
  -subj "/CN=127.0.0.1" -addext "subjectAltName=IP:127.0.0.1"
```

```bash
# The hook endpoint. The signing secret is the whsec_… shown once when you
# issued the plugin's token in workspace settings.
MULTICA_SIGNING_SECRET=whsec_... node server/handler.mjs      # :8788

# The MCP server behind the `metrics` hook.
METRICS_TOKEN=metrics-dev-token node server/metrics-mcp.mjs   # :8789
```

Both read `TLS_CERT` / `TLS_KEY`, defaulting to `dev-cert.pem` / `dev-key.pem`
in the working directory.

Then point Multica at them. Both endpoints are on loopback, which the outbound
guard refuses by design, so name them explicitly:

```bash
export MULTICA_PLUGIN_DIR=examples/plugins
export MULTICA_PLUGIN_DEV_ORIGINS=https://127.0.0.1:8788,https://127.0.0.1:8789
export MULTICA_PLUGIN_DEV_CA=/path/to/your-dev-ca.pem
```

Publish it from `MULTICA_PLUGIN_DIR` as `deploy-sentinel`, install the version
that appears, fill in the config form, then open the `metrics` approval panel
and tick the tools you want agents to reach. Nothing under `metrics` is callable
until you do — that is the whole difference between the `mcp` transport and an
`http` hook.

Note what `MULTICA_PLUGIN_DIR` does and does not cover. It is a publishing
shortcut for the FRONTEND artifact only; the two servers above are still yours
to run, and the `net:` scopes are still what authorises reaching them.

Both endpoints must be HTTPS even locally: the manifest validator requires it,
and `MULTICA_PLUGIN_DEV_CA` is how you get a self-signed certificate trusted.
The dev switches change *which* certificate is trusted and *whether the address
must be public*. They never disable verification, and they never widen the
`net:` scopes — a hook still cannot reach a host the administrator did not
approve.

## Try breaking it

- Ask an agent to roll back `d-4802`. It is 380 minutes old; the plugin refuses
  and explains why.
- Ask for a rollback with the reason "broken". Refused — too short.
- Add a tool to `server/metrics-mcp.mjs` and reopen the approval panel. It
  appears unapproved, and agents cannot call it until somebody ticks it.
- Change an approved tool's `inputSchema` and restart. The agent's task starts
  fine, but that connection is refused: the schema no longer matches what was
  approved.

## Tested

`server/internal/handler/plugin_example_test.go` installs **this manifest**,
not a copy, and drives it: the skill lands in the skill table, the agent tool
list contains exactly the hooks that declare the `agent` trigger, an agent hook
call goes out signed and comes back, and the `metrics` tools are discovered,
refused before approval, and pinned by schema digest after it.

The `.mjs` files are what you run by hand. The test uses Go servers that answer
the same contract, so CI does not need node.
