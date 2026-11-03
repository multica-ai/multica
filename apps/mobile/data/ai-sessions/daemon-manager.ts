import { z } from "zod";
import type { DeviceEndpoint } from "./client";

export interface NativeDaemon {
  start(configuration: string): Promise<string>;
  stop(): Promise<void>;
  updateToken(token: string): Promise<void>;
  networkChanged(): Promise<void>;
  /** Holds a background task; the link keeps running until iOS suspends the app. */
  enterBackground(): Promise<void>;
  /** Verifies connections after a possible suspension and reports whether the link is still running. */
  enterForeground(): Promise<boolean>;
}
export interface DaemonConfiguration { registryUrl: string; apiUrl: string; token: string; userId: string; deviceName: string }
export interface DaemonState { status: "stopped" | "starting" | "ready" | "failed"; endpoint: DeviceEndpoint | null; error: string | null }
const sameLink = (a: DaemonConfiguration, b: DaemonConfiguration) => a.userId === b.userId && a.registryUrl === b.registryUrl && a.apiUrl === b.apiUrl && a.deviceName === b.deviceName;

/** Changes when the network type or its details do; `isInternetReachable` flips alone do not rebuild connections. */
export function networkSignature(state: { type: string; isConnected: boolean | null; details: unknown }) {
  return JSON.stringify([state.type, state.isConnected, state.details]);
}

const endpointSchema = z.object({ url: z.string().refine((url) => /^http:\/\/127\.0\.0\.1:\d+$/.test(url)), token: z.string().min(1) });

/** Serializes native lifecycle operations; stale starts never expose credentials. */
export class EmbeddedDaemonManager {
  private state: DaemonState = { status: "stopped", endpoint: null, error: null };
  private listeners = new Set<() => void>();
  private revision = 0;
  private desired: DaemonConfiguration | null = null;
  private queue: Promise<void> = Promise.resolve();
  private timer?: ReturnType<typeof setTimeout>;
  private backoff = 1000;
  private foreground = true;
  private parked = false;
  private running: DaemonConfiguration | null = null;
  private healthRun = 0;

  constructor(private native: NativeDaemon | null, private fetcher: typeof fetch = fetch) {}
  snapshot = () => this.state;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  private publish(state: DaemonState) { this.state = state; for (const listener of this.listeners) listener(); }
  private enqueue(work: (native: NativeDaemon) => Promise<void>) {
    const native = this.native;
    if (native) this.queue = this.queue.catch(() => {}).then(() => work(native));
    return this.queue;
  }

  configure(configuration: DaemonConfiguration | null): Promise<void> {
    this.desired = configuration;
    const running = this.running;
    if (!configuration || !running || !sameLink(running, configuration) || this.state.status !== "ready") return this.apply(configuration);
    if (running.token === configuration.token) return this.queue;
    this.running = configuration;
    const revision = this.revision;
    return this.enqueue(async (native) => {
      try { await native.updateToken(configuration.token); }
      catch { if (revision === this.revision) void this.apply(this.desired); }
    });
  }

  /** Replaces the running link with the configuration, or stops it while the app is in the background. */
  private apply(requested: DaemonConfiguration | null): Promise<void> {
    const configuration = this.foreground ? requested : null;
    const revision = ++this.revision;
    this.healthRun++;
    this.running = null;
    this.parked = false;
    clearTimeout(this.timer);
    this.publish({ status: configuration ? "starting" : "stopped", endpoint: null, error: null });
    this.queue = this.queue.catch(() => {}).then(async () => {
      if (!this.native) {
        if (revision === this.revision && configuration) this.publish({ status: "failed", endpoint: null, error: "native_unavailable" });
        return;
      }
      try { await this.native.stop(); }
      catch {
        if (revision === this.revision) this.publish({ status: "failed", endpoint: null, error: "connection_failed" });
        return;
      }
      if (revision !== this.revision || !configuration) return;
      try {
        const raw = await this.native.start(JSON.stringify(configuration));
        if (revision !== this.revision) { await this.native.stop(); return; }
        const endpoint = endpointSchema.parse(JSON.parse(raw));
        this.running = configuration;
        this.publish({ status: "ready", endpoint, error: null });
        this.backoff = 1000;
        this.health();
      } catch (error) {
        if (revision !== this.revision) return;
        console.warn("[device-link] start failed", error);
        await this.native.stop().catch(() => {});
        this.publish({ status: "failed", endpoint: null, error: "connection_failed" });
        this.timer = setTimeout(() => { if (revision === this.revision) void this.apply(this.desired); }, this.backoff);
        this.backoff = Math.min(this.backoff * 2, 30_000);
      }
    });
    return this.queue;
  }
  restart = () => this.apply(this.desired);
  stop = () => this.configure(null);

  /** A ready link is left running in the background; iOS decides when it stops. */
  setForeground(foreground: boolean): Promise<void> {
    if (this.foreground === foreground) return this.queue;
    this.foreground = foreground;
    if (!foreground) {
      if (this.state.status !== "ready") return this.apply(this.desired);
      this.parked = true;
      this.healthRun++;
      clearTimeout(this.timer);
      return this.enqueue(async (native) => { await native.enterBackground().catch(() => {}); });
    }
    if (!this.parked) return this.apply(this.desired);
    this.parked = false;
    const revision = this.revision;
    return this.enqueue(async (native) => {
      const alive = await native.enterForeground().catch(() => false);
      if (revision !== this.revision) return;
      if (alive) this.health(0); else void this.apply(this.desired);
    });
  }

  networkChanged(): Promise<void> {
    if (this.state.status !== "ready") return this.queue;
    return this.enqueue(async (native) => { await native.networkChanged().catch(() => {}); });
  }

  /** iOS may have reclaimed the local server while suspended, so a return checks it at once. */
  private health(delay = 15_000) {
    const run = ++this.healthRun;
    this.timer = setTimeout(() => {
      const endpoint = this.state.endpoint;
      if (run !== this.healthRun || !endpoint) return;
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 5000);
      void this.fetcher(`${endpoint.url}/health`, { headers: { Authorization: `Bearer ${endpoint.token}` }, signal: controller.signal })
        .then((response) => {
          if (run !== this.healthRun) return;
          if (!response.ok) void this.restart(); else this.health();
        }).catch(() => { if (run === this.healthRun) void this.restart(); })
        .finally(() => clearTimeout(timeout));
    }, delay);
  }
}
