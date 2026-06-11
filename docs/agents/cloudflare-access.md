# Cloudflare Access for `multica-api.firtal.com` (TECH-3396)

Put the Multica API behind Cloudflare Access so only approved people and
machines can reach it. People sign in with Google; each machine carries its own
service token that can be revoked one at a time. A stolen credential is
worthless from outside the wall.

This doc covers the **Cloudflare-side setup** (requires Cloudflare admin) and
the **env wiring** that activates the code. The code is already shipped and
**inert until both server env vars below are set**, so raising the wall is a
config step, not a deploy.

## Architecture (two layers, don't conflate them)

1. **Cloudflare Access = the wall (network gate).** Decides whether a request
   may reach the API at all. A human passes it with Google login; a machine
   passes it with a service token (`CF-Access-Client-Id` / `-Secret` headers).
   Cloudflare rejects anyone else at the edge — that is the "unknown machine is
   rejected" guarantee. It is enforced by Cloudflare, not by Multica.
2. **Multica token / CF stamp = identity (who am I).** After the edge, Multica
   still needs to know which user/agent. Daemon and CLI keep sending their
   normal `Authorization: Bearer` token for identity. For a human who only
   passed the Google login, Multica trusts Cloudflare's `Cf-Access-Jwt-Assertion`
   stamp and maps its email to a user.

The code half lives in `server/internal/cerebro/cfaccess` and is wired through
`CEREBRO-PATCH(cf-access-auth)` (server) and `CEREBRO-PATCH(cf-access-client)`
(daemon/CLI). See `docs/cerebro-patches.md`.

## Cloudflare setup (admin)

1. **Access application** over `multica-api.firtal.com` (Zero Trust →
   Access → Applications → Add → Self-hosted).
2. **Identity policy:** allow the Firtal Google accounts (email domain or a
   group). This is the human login path.
3. **Service-token policy:** add a second policy on the same application that
   allows "Service Auth" (Access → Service Auth → create one token **per
   machine**, named e.g. `laptop-jeh`, `runtime-sliplane-1`). Each machine gets
   its own token so it can be revoked individually.
4. **Bypass the health paths** so Sliplane's health probe is not redirected to
   the Google login (which would mark the container as down). Add a policy with
   action **Bypass** for these paths on the application:
   - `/health`
   - `/healthz`
   - `/readyz`
   These are mounted outside the auth middleware in `router.go`, so bypassing
   them at the edge is safe.
5. **Copy the AUD tag** of the application (Application → Overview → "Application
   Audience (AUD) Tag"). This is what the server validates.

## Env wiring

Server (the API process — Sliplane prod + staging mac mini):

```
CEREBRO_CF_ACCESS_TEAM_DOMAIN=firtal            # or firtal.cloudflareaccess.com
CEREBRO_CF_ACCESS_AUD=<application-AUD-tag>      # comma-separated if more than one
```

Until both are set the verifier is nil and the server behaves exactly as today.

Each machine (laptop / cloud runtime) — either profile config or env:

- Laptop: store in the profile config `~/.multica/.../config.json`:
  ```json
  { "cf_access_client_id": "<token-id>.access", "cf_access_client_secret": "<token-secret>" }
  ```
- Cloud runtime: set env (via Infisical / Sliplane):
  ```
  CEREBRO_CF_ACCESS_CLIENT_ID=<token-id>.access
  CEREBRO_CF_ACCESS_CLIENT_SECRET=<token-secret>
  ```

The CLI/daemon prefer the profile config and fall back to env.

## Rollout order (no lockout)

1. Ship the code (done — inert).
2. Provision one service token per machine and set its client id/secret.
3. Create the Access application **in Bypass/test mode** or with a generous
   policy, confirm a laptop daemon + CLI still reach the API with the service
   token attached.
4. Set `CEREBRO_CF_ACCESS_TEAM_DOMAIN` + `_AUD` on the server.
5. Tighten the Access policy to Google + service-token only.
6. Verify: a laptop reaches the API without a manual key; a machine with no
   service token is rejected at the edge.

## Revoking a machine

Delete its service token in Cloudflare (Access → Service Auth). That machine is
locked out immediately; every other machine keeps working.
