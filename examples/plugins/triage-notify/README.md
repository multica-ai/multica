# Triage Notify

Demonstrates the hook engine: Multica calling **out** to a plugin's own server,
and that server calling back in.

The three previous examples only ever ran inbound — a sandboxed panel asking the
host for things. This one has a backend, which is what makes every check in the
hook engine necessary.

## The four triggers

The triage hook is reached three ways; the heartbeat adds a fourth, scheduled
trigger. What differs is who caused each call and therefore whose name any
resulting comment carries.

| Trigger | Where it comes from | Blocks? | Comment author |
| --- | --- | --- | --- |
| `ui` | The **Triage this issue** button in the panel | The panel only | The person who clicked |
| `manual` | The **Triage this issue** entry in the issue actions menu | That menu only | The person who picked it |
| `event` | Automatically, when an issue is created | **Never** | The plugin itself |
| `schedule` | Every five minutes in UTC | **Never** | The plugin itself |

`event` never blocking is not a performance tuning choice. The event bus runs its
listeners inline on the goroutine of the request that published, so a hook
dialled from there would put `triage.example.com` on the critical path of
creating an issue — and an issue would fail to be created because somebody
else's server was slow.

## Running the handler

```bash
# The signing secret is shown once, next to the install token, when an admin
# rotates the plugin's token in workspace settings.
MULTICA_SIGNING_SECRET=whsec_… node server/handler.mjs
```

`server/handler.mjs` is the interesting file if you are writing a plugin. It
shows the four things a handler must do and one the host cannot do for you:

1. **Verify against the raw bytes.** Parse *after* verifying. Re-serializing the
   JSON and signing that instead is the classic way to make valid signatures
   fail and invalid ones pass — key order and whitespace are not preserved.
2. **Check the timestamp.** It is inside the signature, so a captured request
   expires. Five minutes is the host's tolerance.
3. **Compare in constant time.** A byte-at-a-time comparison leaks how much of a
   guessed signature was right, which is enough to recover the rest.
4. **Remember signatures inside the window.** *This is the part the host cannot
   do for you.* The timestamp bounds a replay to a few minutes; only the
   receiver can close the gap entirely by refusing bytes it has already seen.

## Calling back

Each request carries a `callback_token` and a `callback_url`. The token is
scoped to that one invocation and expires in minutes — spend it on the work at
hand rather than storing it.

You do not choose what identity the callback writes as. That was decided when
the hook was dispatched: a `ui`/`manual` call writes as the person who triggered
it (recorded with `via_plugin_id`), an `event` call writes as the plugin. A
handler cannot elect to write as somebody else, which is the reason the callback
token exists instead of just handing out the install token.

The callback token is revoked when this HTTP request returns and is held by one
Multica server instance. Use it only for work completed before responding. A
scheduled integration that needs to continue asynchronously or reconcile a
large external backlog should store the `mpi_` install token shown to the admin
at token rotation and use that standing credential instead.

## Scheduled delivery identity

The `scheduled_heartbeat` Hook is intentionally side-effect free. It logs the
stable `delivery_id`, per-attempt `invocation_id` and `attempt`, plus
`schedule.planned_at`. A real scheduled Hook must persist `delivery_id` before
performing side effects: network timeouts and stale-lease recovery can deliver
the same planned occurrence more than once.

## Where a plugin can send data

`net:triage.example.com` in the manifest is the whole answer, and the admin sees
that exact line on the consent screen. It is an exact host match, never a
suffix: a plugin that needs `api.triage.example.com` declares that too. The same
list becomes the panel's CSP `connect-src`, so one string means one thing in
both places.

Nothing a deployment can configure widens it. `MULTICA_PLUGIN_DEV_ORIGINS` lets
an author point a hook at a local server during development, but the `net:`
check still runs — the operator can relax where the network guard applies, not
what the admin approved.

## Local development

```bash
export MULTICA_PLUGIN_DEV_ORIGINS=https://localhost:8787
```

Then point `transport.url` at your local handler and declare the matching
`net:localhost` scope. Without the opt-in the host refuses to dial a private
address, which is the SSRF guard doing its job.
