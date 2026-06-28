---
name: multica-personal-browser
description: Use when a task needs a real browser that is ALREADY LOGGED IN as the user — opening a site behind the user's saved login, reading or acting inside an authenticated account (dashboards, webmail, admin panels, a SaaS the user is signed into) that a fresh throwaway browser could not reach. This drives the in-app PERSONAL BROWSER (the Multica-owned browser pane in the desktop app), NOT the user's system Chrome/Safari. Do NOT use it for anonymous fetches or public pages — use the `agent-browser` skill or WebFetch for those. Requires the `tools:personal-browser` capability. You start it yourself with `multica cerebro-browser open` (launches the desktop app if it is closed and opens the Browser tab) — you no longer need the user to open it.
user-invocable: false
allowed-tools: Bash(multica cerebro-browser *)
---

# Personal browser (in-app, logged-in)

The personal browser is a **Multica-owned** Chromium pane that lives inside the
desktop app, with the user's **saved logins persisting across restarts**. You
drive the *same* pane the user is signed into — so you can act inside the user's
authenticated accounts without ever touching their real system browser.

It is the logged-in sibling of the `agent-browser` skill:

| Use the **personal browser** (this skill) | Use **`agent-browser`** instead |
|---|---|
| The page needs the user's existing login | Anonymous / public page |
| Read or act inside the user's account | A throwaway session is fine |
| "Open my dashboard / inbox / admin" | "Scrape this public site" |

If a public fetch or a fresh login would do, prefer `agent-browser` or WebFetch.
This skill acts inside the user's real sessions — reach for it only when that is
the point.

## Access — gated, conditional, and audited

Two things must both be true, and they are checked **per action**:

- **The feature must be ON for the workspace** (an admin enables the Browser
  feature). If it is off, the CLI fails fast — tell the user it needs enabling.
- **You must be allowed the `tools:personal-browser` capability** (Settings →
  Permissions). Permission resolves through every layer — workspace, runtime,
  agent, group, user — and is **Deny by default**. An admin can also limit you to
  **specific domains** (a host condition): then you may only drive the browser on
  those sites, and a `navigate`/`snapshot`/`click`/`fill` on any other site is
  refused with a clear "blocked by policy" message.

Because the check is per action against the **current site**, a refusal is normal
and expected on a disallowed domain — do not try to work around it; tell the user
which domain was blocked and that it needs granting.

- The browser must be **running**, and you start it yourself: `multica cerebro-browser
  open` launches the Multica desktop app if it is closed and opens the Browser tab for
  the user to watch — you do **not** ask the user to open anything. Every other action
  also fails with a clear "not running" message if the browser is down; run `open` and
  retry.
- **Every action is audited** twice — locally on the machine and centrally on the
  Multica server (which agent, which host, allowed or not; never the typed value).
  Treat the user's logged-in accounts with care: never log out, change settings,
  send messages, or make purchases unless the task explicitly asks for it.

## How to drive it

**Step 0 — start the browser (do this first).** `open` launches the desktop app
if it is closed and opens & focuses the Browser tab, so the user can watch, log
in, or take over. Optionally pass a URL to land the page there.

```bash
multica cerebro-browser open                                  # launch + open the tab
multica cerebro-browser open https://app.example.com/login    # …and land on a URL
```

Then work in two steps: **snapshot to see, then act by ref.** You act on
elements by a stable `@ref` (e.g. `@e12`) from the snapshot — never by guessing
CSS selectors or pixel coordinates.

```bash
# 1. Read the current page as a ref-based accessibility tree.
multica cerebro-browser snapshot

# 2. Act on elements by their @ref from that snapshot.
multica cerebro-browser click @e12
multica cerebro-browser fill @e7 "search text"

# Load a page.
multica cerebro-browser navigate https://app.example.com/dashboard
```

After any `click`/`fill`/`navigate` that changes the page, take a **fresh
`snapshot`** — refs are only valid for the snapshot they came from.

Each command prints a JSON result to stdout. `snapshot` returns
`{ url, title, nodes: [{ ref, role, name, value? }] }`. The action commands
return `{ ok: true }` on success.

## Sessions — isolated logins

Each **session** is its own isolated browser with its own cookies, so you can be
logged into two different accounts of the same site at once. Omit `--session`
to use the default session; pass `--session <id>` to target a named one.

```bash
multica cerebro-browser sessions                      # list open sessions
multica cerebro-browser snapshot --session work        # act in the "work" session
multica cerebro-browser navigate https://x.com --session personal
```

A session id is a short slug (`[a-z0-9-]`). A session that does not exist yet is
created the first time you target it (logged out, on a blank page) — so the user
can then log into it.

## Logging out / clearing cookies

Only when the task asks (e.g. "switch accounts", "clear my login"):

```bash
multica cerebro-browser logout                  # wipe cookies + storage + cache, default session
multica cerebro-browser logout --session work   # …for a named session
multica cerebro-browser clear-cookies           # cookies only (lighter than logout)
```

`logout` only ever wipes the personal browser's own session — never the user's
system browser and never another session.

## Hard rules

- **Never** assume a ref from an old snapshot is still valid — re-snapshot after
  every change.
- **Never** type secrets you were not given. If a site needs a login the user
  has not saved, run `multica cerebro-browser open <url>` to bring the browser up
  on that page and ask the user to log in there; do not attempt to enter
  credentials yourself.
- **Never** take a destructive or outward-facing action (send, pay, delete, post,
  change account settings, log out) unless the task explicitly requests it —
  this is the user's real, logged-in account.
- If a command reports **"not granted"**, relay that to the user plainly — it is
  not something you can fix by retrying. If it reports **"not running"**, run
  `multica cerebro-browser open` first, then retry the command.

Every command and contract above is traced to source in
`references/personal-browser-source-map.md`.
