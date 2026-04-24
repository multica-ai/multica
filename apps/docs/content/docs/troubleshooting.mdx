---
title: Troubleshooting
description: The top 7 common issues when self-hosting Multica — symptoms, causes, how to diagnose, how to fix.
---

import { Callout } from "fumadocs-ui/components/callout";

Look up issues by symptom. Each entry gives you **symptom / likely causes / how to diagnose / how to fix**. If your situation isn't listed, open an issue on [GitHub](https://github.com/multica-ai/multica/issues).

## Daemon can't connect to the server

**Symptom**: [`multica daemon`](/cli)'s `status` command shows `offline` or `connection refused`; the server logs show no `/api/daemon/register` or `/api/daemon/heartbeat` requests. For how the daemon mechanism works, see [Daemon and runtimes](/daemon-runtimes).

**Likely causes**:

1. **`MULTICA_SERVER_URL` points at the wrong address** — default is `ws://localhost:8080/ws`; self-host must change it to your server address
2. **Network / firewall blocking** — the daemon and server aren't on the same network, or outbound traffic is blocked
3. **Token expired or invalid** — you never ran `multica login`, or the PAT was revoked
4. **Server rejected registration** — the account you signed in with isn't in the target workspace (register returns 403)
5. **DNS resolution failure** — the hostname doesn't resolve on the daemon machine

**How to diagnose**:

```bash
multica daemon logs --lines 100    # look for daemon-side errors
echo $MULTICA_SERVER_URL          # confirm the address is set
curl -i http://<server-host>:8080/health   # hit the server directly
curl -i http://<server-host>:8080/readyz  # include DB + migration readiness
cat ~/.multica/config.json        # verify api_token exists
multica workspace list            # confirm you're a member of the target workspace
```

**How to fix**: address each cause above. The two most common fixes are **changing `MULTICA_SERVER_URL` and restarting the daemon** (`multica daemon restart`) and **signing in again** (`multica logout && multica login`).

## Tasks stuck in `queued`

**Symptom**: after assigning an issue to an agent, the issue status flips to `in_progress` immediately, but a long time passes with no sign of agent execution on the page; `multica daemon status` shows the daemon `online`.

**Likely causes** (ordered by frequency):

1. **Agent concurrency limit reached** — this agent's `max_concurrent_tasks` (default 6) is fully occupied by other running tasks
2. **Another task from the same agent is still running on the same issue** — same agent × same issue is forced to run sequentially (prevents duplicate execution)
3. **Agent has been archived** — after archival, new tasks still enqueue but can't be claimed, and they time out after 5 minutes (code-issue G-01)
4. **Daemon hasn't registered this runtime in the current workspace** — restart the daemon or reselect the runtime in the UI
5. **Daemon disconnected** — no heartbeat in the last 45 seconds. `daemon status` reporting `online` may reflect a very recent disconnect

**How to diagnose**:

```bash
multica daemon status --output json       # runtime list + last_seen_at
multica agent list                         # check agent archived state
multica issue show <issue-id>             # inspect task history
```

On the server side (self-host), grep for `"no_tasks"` / `"no_capacity"` to see the claim outcome.

**How to fix**:

- Concurrency full → wait for running tasks to finish, or `multica agent update <id> --max-concurrent-tasks 10` to raise the ceiling
- Same-issue serialization → wait for the previous task to finish, or reassign to a different agent
- Agent archived → `multica agent restore <id>`
- Runtime not registered → `multica daemon restart`, and the daemon will re-register

## WebSocket can't connect

**Symptom**: the browser console logs `WebSocket is closed`; the page doesn't show real-time updates (task progress, comments, inbox), and a refresh is needed to see them; backend tasks still execute.

**Likely causes**:

1. **Origin check failure** — your frontend domain isn't in the server's CORS allowlist. The default allowlist only includes `localhost:3000/5173/5174`; self-hosting on the public internet requires `FRONTEND_ORIGIN`
2. **Protocol mismatch** — frontend on `https://` needs `wss://`; HTTP uses `ws://`
3. **Reverse proxy doesn't enable WebSocket upgrade** — Nginx / Envoy / HAProxy don't forward the `Upgrade` header by default
4. **JWT cookie expired or missing** — no re-sign-in after the 30-day expiry

**How to diagnose**:

- Browser DevTools → Network → filter by "WS" and check connection state and status code
- Grep server logs for `"rejected origin"` / `"websocket"` — an origin issue spells itself out
- `curl -i http://<server-host>:8080/ws` should return `101 Switching Protocols` (with the `Upgrade` header)

**How to fix**:

- Wrong origin → set `FRONTEND_ORIGIN=https://multica.yourdomain.com` in the server's `.env` (or comma-separated `CORS_ALLOWED_ORIGINS`) and restart the server
- Protocol mismatch → make sure `FRONTEND_ORIGIN`'s protocol matches the frontend's
- Reverse proxy → in Nginx, add `proxy_http_version 1.1; proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection "upgrade";`
- Cookie expired → refresh the page and sign in again

## Emails not received

**Symptom**: after submitting an email during sign-in or invite acceptance, neither the inbox nor the spam folder has the verification code.

**Likely causes**:

1. **`RESEND_API_KEY` not set** — the server silently falls back and **writes the code to its own stdout** without error. Easy to trip over in production
2. **Resend API key invalid / out of quota** — server logs show `"failed to send verification code"`
3. **`RESEND_FROM_EMAIL`'s domain not verified in Resend** — Resend refuses to send
4. **Email was sent but flagged as spam by the recipient's ISP** — check the Resend dashboard and the spam folder

**How to diagnose**:

- Grep server logs for `"[DEV] Verification code for"` — if present, Resend isn't configured and the code was written to stdout
- [Resend dashboard](https://resend.com/) → Emails for send history
- Confirm `RESEND_FROM_EMAIL`'s domain appears in the Resend console's "Verified Domains" list

**How to fix**:

- Missing API key → follow [Sign-in and signup configuration → How email works](/auth-setup#how-email--verification-code-sign-in-works) to configure and restart the server
- Domain not verified → run the DNS verification flow in the Resend console (add SPF / DKIM records)
- In an emergency (internal testing) → copy the code printed under `[DEV]` from the server logs

## Verification code `888888` doesn't work

**Symptom**: on a self-hosted instance, you try to sign in with the development-only master code `888888` and it's rejected with `invalid or expired code`.

**Likely causes** (mutually exclusive):

1. **`APP_ENV=production`** — this is the **correct** production configuration; `888888` is **disabled** when `APP_ENV=production`. Intentional design, not a bug
2. **You received a real code via Resend** — if Resend is configured, the server sent an actual email; `888888` is only a dev fallback

**How to diagnose**:

```bash
cat .env | grep APP_ENV       # inspect current config
docker exec <container> env | grep APP_ENV   # docker deployment
```

Check your inbox (including spam) for the real verification code.

**How to fix**:

- In production, you shouldn't be using `888888` at all — configure Resend and use real codes
- **For local development or internal testing**, if you need `888888`, ensure `APP_ENV` is unset or not `production` — but **never** run a public instance this way (see [Sign-in and signup configuration → The 888888 trap](/auth-setup#the-888888-trap))

## Port conflicts

**Symptom**: `multica server` or `multica daemon start` fails with `address already in use`.

**Likely causes**:

1. **Server port taken** (default `8080`)
2. **Daemon health port taken** (default `19514`, offset by a hash per profile)
3. **Web dev server port conflict** (`3000` / `5173`)
4. **Insufficient privileges for the port** (binding a privileged port `< 1024` requires sudo)

**How to diagnose**:

```bash
lsof -i :8080        # macOS / Linux
netstat -ano | findstr :8080    # Windows
```

**How to fix**:

- Kill the conflicting process (`kill -9 <PID>`), or change ports via `PORT=9000`
- To use 80 / 443 → don't bind directly; put a reverse proxy (Nginx / Caddy) in front, forwarding to a high port

## Where to find logs

| Component | Location | Command |
|---|---|---|
| **Daemon** | `~/.multica/daemon.log` (background mode) or foreground stdout | `multica daemon logs -f --lines 100` |
| **Server (Docker)** | Container stdout | `docker logs -f <container>` |
| **Server (systemd)** | journal | `journalctl -u multica-server -f` |
| **Frontend (dev)** | Terminal running `pnpm dev` | Read directly |
| **Frontend (browser)** | DevTools → Console | Press `F12` |

For more detailed daemon logs, move it from background to foreground: `multica daemon stop && multica daemon start --foreground`.
