# Weixin host

Lets members chat with Multica agents from WeChat. It runs Tencent's official
[`@tencent-weixin/openclaw-weixin`](https://github.com/Tencent/openclaw-weixin)
plugin unmodified; the plugin owns QR login, the iLink long-poll, media
crypto, and message delivery.

Members connect from an agent's **Integrations** tab: the backend asks this
host for a QR code, and once the member scans it, mints them a personal access
token and hands it over. The host then relays that WeChat account to the agent
through the Chat API as the member, so each WeChat contact becomes one Chat in
their Multica chat list. Only the WeChat user who scanned can talk to the
agent. Inbound images, files, and voice are not forwarded yet; the agent is
told one arrived.

## How it fits together

- `openclaw-sdk/` implements the small part of the OpenClaw plugin SDK the
  plugin imports. It is linked in as the plugin's `openclaw` peer.
- `src/channel-runtime.ts` implements the per-message `channelRuntime`. Where
  OpenClaw would run its own agent, `dispatchReplyFromConfig` sends the turn to
  a Multica agent and hands the reply back to the plugin.
- `src/login-manager.ts` drives the plugin's terminal-oriented QR login for the
  web UI (it reads the plugin's stdout prompts and feeds the verify code to
  stdin).
- `src/server.ts` is the control API the backend calls
  (`server/internal/handler/weixin.go`), authenticated with a shared secret.
  Browsers never reach it.

## Run

With the source-built self-host stack, set in `.env`:

```bash
COMPOSE_PROFILES=weixin
MULTICA_WEIXIN_HOST_URL=http://weixin-host:8790
MULTICA_WEIXIN_HOST_SECRET=<openssl rand -hex 32>
```

then `make selfhost-build`. For local development, copy `.env.example` to
`.env` and run `pnpm --filter @multica/weixin-host start`.

State (WeChat credentials, installations with their tokens, downloaded media)
lives in `WEIXIN_HOST_STATE_DIR`: a Docker volume, or `.state/` locally.

The SDK surface the plugin depends on is OpenClaw-internal and changes between
OpenClaw releases, so the plugin version is pinned. Check its changelog and
the `openclaw/plugin-sdk/*` imports in `dist/` before upgrading.
