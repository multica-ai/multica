import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { normalizeAccountId } from "openclaw/plugin-sdk/account-id";

import { InstallationStore } from "./installation-store.ts";
import { LoginManager } from "./login-manager.ts";
import { loadPlugin } from "./plugin.ts";
import { createControlServer } from "./server.ts";
import { SessionStore } from "./session-store.ts";
import { Supervisor } from "./supervisor.ts";

const APP_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const DEFAULT_REPLY_TIMEOUT_MS = 15 * 60 * 1000;

const USAGE = `Weixin host — relays Weixin chats to Multica agents.

Configuration (environment or apps/weixin-host/.env):
  WEIXIN_HOST_SECRET     Shared secret the Multica backend authenticates with (required)
  WEIXIN_HOST_LISTEN     host:port for the control API (default 127.0.0.1:8790)
  MULTICA_API_URL        Multica backend URL as seen from this host (default http://localhost:8080)
  WEIXIN_HOST_STATE_DIR  Weixin credentials, installations, media (default apps/weixin-host/.state)
`;

const envFile = path.join(APP_DIR, ".env");
if (fs.existsSync(envFile)) process.loadEnvFile(envFile);

const secret = process.env.WEIXIN_HOST_SECRET?.trim();
if (!secret) {
  console.error(`Missing WEIXIN_HOST_SECRET.\n\n${USAGE}`);
  process.exit(1);
}
const [listenHost, listenPort] = (process.env.WEIXIN_HOST_LISTEN?.trim() || "127.0.0.1:8790").split(":");
const stateDir = path.resolve(
  process.env.WEIXIN_HOST_STATE_DIR?.trim() || path.join(APP_DIR, ".state"),
);
const log = (msg: string) => console.log(`[weixin-host] ${msg}`);

const loaded = await loadPlugin(stateDir);
const store = new InstallationStore(stateDir);
const supervisor = new Supervisor({
  loaded,
  sessions: new SessionStore(stateDir),
  apiUrl: process.env.MULTICA_API_URL?.trim() || "http://localhost:8080",
  stateDir,
  replyTimeoutMs: Number(process.env.WEIXIN_HOST_REPLY_TIMEOUT_MS) || DEFAULT_REPLY_TIMEOUT_MS,
  log,
});
const logins = new LoginManager({ plugin: loaded.plugin, normalizeAccountId });

for (const installation of store.list()) supervisor.start(installation);

const server = createControlServer({ secret, store, logins, supervisor, loaded });
server.listen(Number(listenPort), listenHost, () => {
  log(`control API on ${listenHost}:${listenPort}, ${store.list().length} installation(s)`);
});

async function shutdown() {
  server.close();
  await supervisor.stopAll();
  process.exit(0);
}
process.once("SIGINT", () => void shutdown());
process.once("SIGTERM", () => void shutdown());
