# Schedule Pulse

A real scheduled plugin, not a log line. Multica POSTs the plugin's own HTTPS
endpoint every five minutes. The handler records each unique `delivery_id` in
workspace storage **before** it answers, so a retry of the same planned wake
does not look like a second pulse.

The issue panel reads that storage row. After install you should see the count
go up about once every five minutes, not on every HTTP attempt.

## What it verifies

| Host behavior | What this plugin does |
| --- | --- |
| Durable schedule trigger | Declares `triggers: ["schedule"]` and a five-minute cron |
| At-least-once delivery | Persists `delivery_id` first; a retry with the same id returns `{duplicate: true}` |
| Plugin actor | Writes as the installation through the callback token, into `storage:workspace` |
| Consent | `net:pulse.example.com` is the only outbound host; schedule is not a data scope |

It does not create issues. That is a later Plugin Action API, not this trigger.

## Running the handler

```bash
MULTICA_SIGNING_SECRET=whsec_… node server/handler.mjs
```

The signing secret is shown once, next to the install token, when an admin
rotates the plugin's token in workspace settings.

Point `transport.url` at this process and declare the matching `net:` scope.
Local development also needs:

```bash
export MULTICA_PLUGIN_DEV_ORIGINS=https://localhost:8787
```

Without that opt-in the host refuses a private address.

## Why storage, not a log

A log line cannot prove idempotency. The panel shows `count`, `delivery_id`,
`planned_at`, and `attempt`. If the host retries the same plan, `attempt`
changes and `count` does not.

The callback token is revoked when this HTTP request returns. All storage
writes happen before the 200. Work that must continue after the response
should use the standing `mpi_` install token instead.
