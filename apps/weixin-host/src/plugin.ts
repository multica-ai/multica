import fs from "node:fs";
import path from "node:path";

const PLUGIN_ENTRY = "@tencent-weixin/openclaw-weixin/dist/index.js";
const PLUGIN_ACCOUNTS = "@tencent-weixin/openclaw-weixin/dist/src/auth/accounts.js";
/** Reported to iLink as `base_info.bot_agent` so traffic is attributed to Multica. */
const BOT_AGENT = "MulticaWeixinHost/0.1.0";

export interface WeixinAccount {
  accountId: string;
  configured: boolean;
}

export interface WeixinPlugin {
  config: {
    listAccountIds(cfg: unknown): string[];
    resolveAccount(cfg: unknown, accountId: string): WeixinAccount;
  };
  gateway: {
    startAccount(ctx: Record<string, unknown>): Promise<void>;
    stopAccount(ctx: Record<string, unknown>): Promise<void>;
    loginWithQrStart(params: {
      accountId?: string;
      force?: boolean;
    }): Promise<{ qrDataUrl?: string; message: string }>;
    loginWithQrWait(params: {
      accountId?: string;
      sessionKey?: string;
      timeoutMs?: number;
    }): Promise<{ connected: boolean; message: string; accountId?: string }>;
  };
}

interface PluginAccounts {
  loadWeixinAccount(accountId: string): { userId?: string } | null;
  clearWeixinAccount(accountId: string): void;
  unregisterWeixinAccountId(accountId: string): void;
}

export interface LoadedPlugin {
  plugin: WeixinPlugin;
  accounts: PluginAccounts;
  /** The plugin's openclaw.json, re-read so login-time writes are picked up. */
  readConfig(): unknown;
}

/**
 * Points the plugin at `stateDir`, seeds its openclaw.json, and loads it.
 * The state dir must be set before the import: the plugin resolves some
 * paths at module load.
 */
export async function loadPlugin(stateDir: string): Promise<LoadedPlugin> {
  fs.mkdirSync(stateDir, { recursive: true });
  process.env.OPENCLAW_STATE_DIR = stateDir;

  const configFile = path.join(stateDir, "openclaw.json");
  const readConfig = () => {
    try {
      return JSON.parse(fs.readFileSync(configFile, "utf-8")) as {
        channels?: Record<string, Record<string, unknown>>;
      };
    } catch {
      return {};
    }
  };
  const config = readConfig();
  const section = config.channels?.["openclaw-weixin"] ?? {};
  if (section.botAgent !== BOT_AGENT) {
    config.channels = { ...config.channels, "openclaw-weixin": { ...section, botAgent: BOT_AGENT } };
    fs.writeFileSync(configFile, `${JSON.stringify(config, null, 2)}\n`);
  }

  const entry = (await import(PLUGIN_ENTRY)).default as {
    register(api: Record<string, unknown>): void;
  };
  let plugin: WeixinPlugin | undefined;
  entry.register({
    registrationMode: "full",
    // No host version: the plugin skips its OpenClaw version check.
    runtime: {},
    registerChannel: ({ plugin: registered }: { plugin: WeixinPlugin }) => {
      plugin = registered;
    },
  });
  if (!plugin) throw new Error("openclaw-weixin did not register its channel");

  const accounts = (await import(PLUGIN_ACCOUNTS)) as PluginAccounts;
  return { plugin, accounts, readConfig };
}
