---
title: Authentication and tokens
description: Multica has three kinds of tokens — one each for the browser, the CLI, and the daemon. When to use which.
---

import { Callout } from "fumadocs-ui/components/callout";

Multica has three kinds of tokens, one for each context: the browser Web UI, the command line and scripts, and the daemon. All three represent the same you, but their scopes and lifetimes differ.

## The three tokens

| Token | Format | Where it's used | Lifetime |
|---|---|---|---|
| **JWT cookie** | `multica_auth` cookie (HttpOnly) | Web browser | 30 days |
| **Personal access token (PAT)** | Prefixed with `mul_` | CLI, scripts, direct API calls | No expiry by default; when you create one via the API you can pass `expires_in_days` |
| **Daemon token** | Prefixed with `mdt_` | Daemon-to-server communication | Managed by the daemon itself |

In day-to-day use you'll only touch the first two directly. The **[daemon](/daemon-runtimes) token** is created and refreshed automatically by `multica daemon login` — you don't have to think about it.

## What each token can hit

| API route | JWT cookie | PAT | Daemon token |
|---|---|---|---|
| `/api/user/*` (user-level actions) | ✓ | ✓ | ✗ |
| `/api/workspaces/:id/*` (workspace-level) | ✓ | ✓ | ✗ |
| `/api/daemon/*` (daemon-only) | ✗ | ✓ | ✓ |
| WebSocket `/ws` (real-time push) | ✓ (cookie) | ✓ (authenticates via first message) | ✗ |

**A PAT can hit almost anything** — it represents "the full you." A daemon token can only do what the daemon needs: fetch tasks and report results.

**Both can hit `/api/daemon/*`, but their scopes differ.** A PAT represents an **entire user** — once authenticated, it can see every workspace you belong to. A daemon token is pinned to a single workspace at creation time and can only touch resources in that workspace. In production, run your daemon with a daemon token — don't take the shortcut of using a PAT, or you'll be granting far more privilege than the daemon needs.

## Logging in

### Email + verification code

1. Enter your email; the server sends a 6-digit code.
2. Enter the code; the server issues a JWT cookie (browser) or exchanges it for a PAT (CLI).

<Callout type="warning">
**Self-hosting operators, take note**: if `APP_ENV` is not set to `production`, the verification code is always `888888` — anyone can sign in as anyone. See [Self-host auth configuration](/auth-setup).
</Callout>

### Google OAuth

Click **Sign in with Google** and go through the standard OAuth callback. Self-hosting requires `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and the redirect URI to be configured — see [Self-host auth configuration](/auth-setup).

## Creating, viewing, and revoking a PAT

**Creating** a PAT can be done two ways:

- **Web UI**: Settings → Personal Access Tokens → New token
- **CLI**: `multica login` creates one automatically if there's no local PAT yet

<Callout type="warning">
**The full PAT is displayed exactly once when it's created.** After you refresh or close the dialog, you won't be able to see it again.

Multica stores only the hash of the PAT in the database — not even the server can retrieve the original. Copy and save it immediately. If you lose it, your only option is to revoke it and create a new one.
</Callout>

**Viewing** existing PATs (name, creation time, last-used time — **not** the full token) lives under Settings → Personal Access Tokens.

**Revoking** a PAT: click Revoke in the list. Revocation takes effect immediately — the next request made with that PAT will be rejected with a 401.

## Logging out only deletes the local token

When you run `multica auth logout` or click log out in the Web UI:

- **The local token is cleared** — the CLI removes the PAT from `~/.multica/config.json`; the browser deletes the cookie.
- **The PAT is still valid on the server** — if someone obtained your PAT before you logged out (for example, by copying it to another machine), they **can still use it**.

<Callout type="warning">
**If you suspect your PAT has leaked, don't just log out.** Go to Settings → Personal Access Tokens and **revoke** the token. Only revocation invalidates a leaked token immediately.
</Callout>

## Next steps

- [CLI command reference](/cli) — authentication is automatic for every CLI command
- [Self-host auth configuration](/auth-setup) — how to configure email, OAuth, and signup allowlists when self-hosting
- [Daemon and runtimes](/daemon-runtimes) — where the daemon token comes from
