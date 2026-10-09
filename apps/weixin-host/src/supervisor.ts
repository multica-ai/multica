import { createChannelRuntime } from "./channel-runtime.ts";
import type { Installation } from "./installation-store.ts";
import { MulticaClient } from "./multica-client.ts";
import type { LoadedPlugin } from "./plugin.ts";
import type { SessionStore } from "./session-store.ts";

const RESTART_DELAY_MS = 10_000;

interface Running {
  abort: AbortController;
  done: Promise<void>;
  lastError?: string;
}

/**
 * Runs the plugin's receive loop for every installation — one loop per bound
 * Weixin account — and restarts a loop that ends unexpectedly.
 */
export class Supervisor {
  private readonly loaded: LoadedPlugin;
  private readonly sessions: SessionStore;
  private readonly apiUrl: string;
  private readonly stateDir: string;
  private readonly replyTimeoutMs: number;
  private readonly log: (msg: string) => void;
  private readonly running = new Map<string, Running>();

  constructor(opts: {
    loaded: LoadedPlugin;
    sessions: SessionStore;
    apiUrl: string;
    stateDir: string;
    replyTimeoutMs: number;
    log: (msg: string) => void;
  }) {
    this.loaded = opts.loaded;
    this.sessions = opts.sessions;
    this.apiUrl = opts.apiUrl;
    this.stateDir = opts.stateDir;
    this.replyTimeoutMs = opts.replyTimeoutMs;
    this.log = opts.log;
  }

  /** Whether the plugin still holds credentials for the account. */
  hasAccount(accountId: string): boolean {
    const { plugin, readConfig } = this.loaded;
    try {
      return plugin.config.resolveAccount(readConfig(), accountId).configured;
    } catch {
      return false;
    }
  }

  health(id: string): { running: boolean; last_error: string | null } {
    const run = this.running.get(id);
    return { running: Boolean(run), last_error: run?.lastError ?? null };
  }

  start(installation: Installation): void {
    if (this.running.has(installation.id)) return;
    const abort = new AbortController();
    const run: Running = { abort, done: Promise.resolve() };
    run.done = this.loop(installation, run);
    this.running.set(installation.id, run);
  }

  async stop(id: string): Promise<void> {
    const run = this.running.get(id);
    if (!run) return;
    this.running.delete(id);
    run.abort.abort(new Error("stopped"));
    await run.done;
  }

  async stopAll(): Promise<void> {
    await Promise.all([...this.running.keys()].map((id) => this.stop(id)));
  }

  private async loop(installation: Installation, run: Running): Promise<void> {
    const { plugin, readConfig } = this.loaded;
    const client = new MulticaClient({
      apiUrl: this.apiUrl,
      token: installation.token,
      workspaceId: installation.workspace_id,
    });
    const tag = `[${installation.account_id} → agent ${installation.agent_id}]`;
    const log = (msg: string) => this.log(`${tag} ${msg}`);

    while (!run.abort.signal.aborted) {
      try {
        const cfg = readConfig();
        const account = plugin.config.resolveAccount(cfg, installation.account_id);
        if (!account.configured) {
          throw new Error("Weixin credentials are gone; reconnect from the agent's Integrations tab");
        }
        const agent = await client.getAgent(installation.agent_id);
        const ctx = {
          account,
          cfg,
          abortSignal: run.abort.signal,
          channelRuntime: createChannelRuntime({
            client,
            sessions: this.sessions,
            agentId: installation.agent_id,
            agentName: agent.name,
            stateDir: this.stateDir,
            replyTimeoutMs: this.replyTimeoutMs,
            log,
          }),
          runtime: { log, error: log },
          log: { info: log, warn: log, error: log },
          setStatus: () => {},
        };
        log(`relaying to ${agent.name}`);
        run.lastError = undefined;
        try {
          await plugin.gateway.startAccount(ctx);
        } finally {
          await plugin.gateway.stopAccount(ctx).catch(() => {});
        }
      } catch (err) {
        if (run.abort.signal.aborted) break;
        run.lastError = err instanceof Error ? err.message : String(err);
        log(`receive loop failed: ${run.lastError}`);
      }
      if (!run.abort.signal.aborted) await delay(RESTART_DELAY_MS, run.abort.signal);
    }
  }
}

function delay(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal.addEventListener(
      "abort",
      () => {
        clearTimeout(timer);
        resolve();
      },
      { once: true },
    );
  });
}
