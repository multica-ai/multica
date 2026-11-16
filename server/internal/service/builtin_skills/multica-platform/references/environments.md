# Local environments

An environment is a dev server or app that runs for an issue and is listed in
the issue sidebar's **Environments** section. It comes from one of two places:

- **Declared**: the repository describes it in `.multica/environments.toml`,
  and `multica env start <name>` starts it under the daemon. The daemon keeps
  its log, keeps it running after your run ends, stops it at its TTL, and lets
  members stop, restart and read its logs from the sidebar.
- **Detected**: any process listening on a TCP port whose working directory is
  inside an issue run's directory, seen on two scans about 30 seconds apart.
  Members can only stop it; there is no log and no restart.

Both work on macOS and Linux only. A `localhost` URL opens only on the machine
running it.

## CLI

```bash
multica env start <name>          # waits until ready, prints the URL
multica env start <name> --wait=false
multica env list --output json
multica env stop <name>
```

These need the run's `MULTICA_DAEMON_PORT` and `MULTICA_TOKEN`, so they work
only inside a run. Run `start` from inside the checkout: it looks for
`.multica/environments.toml` in the current directory and its parents, up to
the run's directory. Environments belong to the issue, so a later run can
list and stop what an earlier run started.

`start` reuses a running environment only when the issue, the machine, the
name and the checkout all match. From a different checkout, the same name
starts a second instance on its own ports, not the service an earlier run
started; run `multica env list --output json` first and use the running one's
URL when that is what you need. With several instances under one name,
`multica env stop <name>` stops one of them, whichever it finds first.

Each environment's processes stop with it, even when another environment
talks to them. Put servers that must keep running together, such as a web app
and its API, in one environment (see the recipes).

`start` fails, and prints the last lines of the log, when the start command
exits before it is ready. When it is still not ready after 10 minutes, `start`
returns with the environment still starting; read the log path it printed.

## Writing `.multica/environments.toml`

Put the file at the repository root, and commit it on the branch runs check
out. The sidebar's start menu only finds it at the root of a repository checked
out into the run's `workdir`, or one directory below that root, and skips a
file that does not parse; `multica env start` reports the parse error.

```toml
[environments.web]                  # name: a-z 0-9 - _, at most 40, starts with a letter or digit
label = "Web app"                   # optional; shown in the sidebar
start = "npm ci && npx vite --port $MULTICA_PORT --strictPort"   # required
# url = "http://localhost:$MULTICA_PORT"   # the default; url = "" for no URL
# ready = "http://localhost:$MULTICA_PORT/health"   # defaults to url
# stop = "docker compose down"      # optional; runs before the processes are signalled
ttl = "24h"                         # 1m to 168h, default 24h
```

| Field | Contract |
|---|---|
| `start` | Run with `/bin/sh -c` from the directory that holds `.multica/`, as the leader of a new process group. The environment lives as long as any process in that group does, and stopping it signals that group. A command that hands its work to something outside the group, such as `docker compose up -d`, a system service, or `setsid`, ends the environment as `exited` at once, and stopping it would not reach that work. |
| `url` | Where a member opens it. `$MULTICA_PORT` or `${MULTICA_PORT}` is substituted; no other variable or arithmetic is. Set `url = ""` for a worker or a desktop app. |
| `ready` | Polled once a second until it answers with any status below 500 (a 404 counts). With no `ready` and no `url`, the environment is running as soon as `start` launches. |
| `stop` | Run with the same directory and variables, with a one-minute limit. Afterwards the process group gets `SIGTERM`, then `SIGKILL` 10 seconds later. Needed whenever `start` creates something the process group does not own, such as containers. |
| `ttl` | A Go duration: `30m`, `8h`, `168h`. `7d` is rejected. |

The start and stop commands receive:

- `MULTICA_PORT`: the first of ten free ports reserved for this environment,
  `MULTICA_PORT` through `MULTICA_PORT+9`. Never hard-code a port: parallel
  runs on one machine must not collide. Derive extra ports in the shell, for
  example `API_PORT=$((MULTICA_PORT + 1))`.
- `MULTICA_ENVIRONMENT`: the environment's name.
- The caller's `PATH`, and the daemon's other variables. No other
  `MULTICA_*` variable is passed, credentials included, so the environment
  cannot act as the run.

A run's checkout starts without installed dependencies. Install them in `start`
(`npm ci && ...`); the 10-minute readiness window includes that time.

Output goes to a log file on the machine, and members can read its last 500
lines from the sidebar. Do not print secrets in it.

### Recipes

```toml
# Next.js
[environments.web]
label = "Web app"
start = "npm ci && npx next dev --port $MULTICA_PORT"

# An API that reads PORT and serves a health check
[environments.api]
label = "API"
start = "npm ci && PORT=$MULTICA_PORT npm start"
ready = "http://localhost:$MULTICA_PORT/healthz"

# Web app plus its API, on two ports of one block
[environments.app]
label = "Web + API"
start = """
API_PORT=$((MULTICA_PORT + 1))
(cd api && npm ci && PORT=$API_PORT npm start) &
cd web && npm ci && VITE_API_URL=http://localhost:$API_PORT npx vite --port $MULTICA_PORT --strictPort
"""

# docker compose: stays in the foreground, and the project name keeps runs apart.
# compose.yaml maps the port with "${MULTICA_PORT:-3000}:3000".
[environments.stack]
label = "Full stack"
start = "docker compose -p env-$MULTICA_PORT up --build"
stop = "docker compose -p env-$MULTICA_PORT down"
```

## Boundaries

- Adding or changing `.multica/environments.toml` is a repository change.
  Make it when the task asks for it, and ship it like any other change.
- After writing one, verify it: `multica env start <name>` from the checkout,
  open the URL or `ready` endpoint, read the log, then `multica env stop <name>`
  unless the user wants it kept running.
- When the repository declares nothing and the user wants a service kept
  running, start it yourself under the runtime brief's persistent-service
  rules; the daemon only reports it as a detected server.
- Do not start environments to test something you can test inside your own
  run. Each one holds ports and keeps running until its TTL.
