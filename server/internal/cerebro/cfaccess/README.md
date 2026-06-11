# server/internal/cerebro/cfaccess

Cloudflare Access integration for the cerebro fork (TECH-3396). Cerebro-only
(L4): imported by upstream-zone files via small `CEREBRO-PATCH(cf-access-*)`
hooks, never imports upstream auth logic.

Two halves at opposite ends of the wall:

- **Server (`Verifier`)** — validates the `Cf-Access-Jwt-Assertion` header that
  Cloudflare injects after a request passes the Access policy, against the
  team's published JWKS + accepted AUD tags, then maps the stamp's email to a
  Multica user. Used as an additive auth path in `middleware.Auth`.
- **Client (`ServiceToken`)** — attaches a machine's per-machine
  `CF-Access-Client-Id` / `CF-Access-Client-Secret` headers to every outbound
  request (CLI, daemon HTTP, wakeup websocket) so Cloudflare lets it through.
  Cloudflare consumes those headers at the edge; they never reach Multica.

**Everything is inert until configured.** A nil `Verifier` (team domain / AUD
unset) and an unconfigured `ServiceToken` (client id / secret unset) both behave
as "feature off", so the code ships safely before the wall is raised and never
locks anyone out of an environment that is not behind Cloudflare.

## Configuration

Server (the API process — Sliplane / staging mac mini):

| Env var | Meaning |
|---|---|
| `CEREBRO_CF_ACCESS_TEAM_DOMAIN` | Team domain — `firtal` or `firtal.cloudflareaccess.com` |
| `CEREBRO_CF_ACCESS_AUD` | One or more Access application AUD tags, comma-separated |

Client (each laptop / cloud runtime) — either profile config or env:

- `~/.multica/.../config.json`: `cf_access_client_id`, `cf_access_client_secret`
- or env: `CEREBRO_CF_ACCESS_CLIENT_ID`, `CEREBRO_CF_ACCESS_CLIENT_SECRET`

The client prefers the profile config and falls back to env (cloud runtimes set
env via Infisical / Sliplane).

See `docs/agents/cloudflare-access.md` for the full Cloudflare-side setup
(Access application, Google policy, per-machine service tokens, health-path
bypass).
