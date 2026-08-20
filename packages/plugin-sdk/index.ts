/**
 * @multica/plugin-sdk — what a plugin surface imports.
 *
 * A surface is an ordinary script running in a sandboxed iframe. It holds no
 * credential and cannot reach Multica's API directly: every call here becomes a
 * message to the host page, which performs the call on the signed-in user's own
 * session and sends the result back. That indirection is the whole security
 * story — a plugin can never do more than the person looking at it, and there
 * is no token in the frame to leak.
 *
 * Zero runtime dependencies, and deliberately no import of `@multica/core` or
 * `@multica/ui`: this ships to third parties.
 */

import {
  BRIDGE_INIT_MESSAGE,
  BRIDGE_PROTOCOL_VERSION,
  BRIDGE_READY_MESSAGE,
  isBridgeEvent,
  isBridgeResponse,
  type BridgeMethod,
  type BridgeRequest,
  type ThemeTokens,
} from "./protocol";

export type { ThemeTokens } from "./protocol";

export interface PluginContext {
  workspace: { id: string; name: string; slug: string };
  user: { id: string; name: string };
  issue?: { id: string; identifier: string; title: string };
  /** Non-secret installation configuration. Secrets never reach the frame. */
  config: Record<string, unknown>;
  /** Domains this installation was granted via `net:` scopes. */
  granted_net_domains: string[];
}

export interface Issue {
  id: string;
  identifier: string;
  title: string;
  description?: string | null;
  status: string;
  priority: string;
}

export interface Comment {
  id: string;
  author_type: string;
  author_id: string;
  content: string;
  type: string;
  parent_id?: string;
  created_at: string;
}

/** One completed hook call. `status` is the host's verdict, not the endpoint's. */
export interface HookResult {
  status: string;
  output?: unknown;
  error?: string;
  latency_ms: number;
  hook_key: string;
  trigger: string;
  attempts: number;
}

export interface StorageKey {
  key: string;
  size_bytes: number;
  updated_at: string;
}

/** Thrown when the host refuses or the call fails. `status` mirrors HTTP. */
export class MulticaPluginError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "MulticaPluginError";
    this.status = status;
  }
}

const DEFAULT_TIMEOUT_MS = 15_000;
const ANNOUNCE_INTERVAL_MS = 120;
const ANNOUNCE_ATTEMPTS = 50;

type Pending = {
  resolve: (value: unknown) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
};

class Bridge {
  private port: MessagePort | null = null;
  private readonly pending = new Map<string, Pending>();
  private readonly queued: BridgeRequest[] = [];
  private ready: Promise<void>;
  private markReady!: () => void;
  private sequence = 0;
  private theme: ThemeTokens = {};
  private readonly themeListeners = new Set<(theme: ThemeTokens) => void>();

  constructor() {
    this.ready = new Promise((resolve) => {
      this.markReady = resolve;
    });
    if (typeof window !== "undefined") {
      window.addEventListener("message", this.onWindowMessage);
      this.announce();
    }
  }

  /**
   * Announces until the host answers with a port.
   *
   * One announcement is not enough in either direction: a srcdoc frame can
   * finish executing before the embedder's effect attaches its listener, and a
   * host that waits on the iframe load event can miss it entirely. Repeating
   * until the port arrives makes the handshake independent of who is ready
   * first, which is the only ordering nobody controls.
   */
  private announce() {
    if (typeof window === "undefined" || !window.parent) return;
    let attempts = 0;
    const beat = () => {
      if (this.port || attempts++ > ANNOUNCE_ATTEMPTS) return;
      // targetOrigin "*" because this frame has an opaque origin and cannot
      // know the embedder's. The message carries nothing but the signal.
      window.parent.postMessage({ type: BRIDGE_READY_MESSAGE }, "*");
      setTimeout(beat, ANNOUNCE_INTERVAL_MS);
    };
    beat();
  }

  private onWindowMessage = (event: MessageEvent) => {
    const data = event.data as { type?: string; version?: number; theme?: ThemeTokens } | null;
    if (!data || data.type !== BRIDGE_INIT_MESSAGE) return;
    const port = event.ports?.[0];
    if (!port) return;
    // Only the embedder may hand this frame a port.
    //
    // Sibling frames are mutually opaque, but `parent.frames[i]` is an indexed
    // cross-origin access the spec allows, so another plugin on the same page
    // can postMessage into this one. Without this check it could deliver its own
    // MessagePort and become this surface's "host": feed it fabricated issue
    // data and read everything the surface writes, including whatever it puts in
    // storage:user. Origin cannot be compared — a sandboxed frame sees "null" —
    // so the embedder is identified by window reference, mirroring the host's
    // own check on the readiness signal.
    if (event.source !== window.parent) return;
    // First init wins. A hijacker that lost the race could otherwise send a
    // second init and take the channel over afterwards.
    if (this.port) return;
    if (data.version !== BRIDGE_PROTOCOL_VERSION) {
      // A host newer or older than this SDK: fail loudly rather than guess at a
      // shape neither side agreed on.
      return;
    }
    this.port = port;
    port.onmessage = this.onPortMessage;
    port.start?.();
    if (data.theme) this.applyTheme(data.theme);
    for (const request of this.queued.splice(0)) port.postMessage(request);
    this.markReady();
  };

  private onPortMessage = (event: MessageEvent) => {
    const message = event.data;
    if (isBridgeEvent(message)) {
      this.applyTheme(message.theme);
      return;
    }
    if (!isBridgeResponse(message)) return;
    const pending = this.pending.get(message.id);
    if (!pending) return;
    this.pending.delete(message.id);
    clearTimeout(pending.timer);
    if (message.ok) pending.resolve(message.data);
    else pending.reject(new MulticaPluginError(message.status, message.error));
  };

  private applyTheme(theme: ThemeTokens) {
    this.theme = theme;
    if (typeof document !== "undefined") {
      for (const [name, value] of Object.entries(theme)) {
        document.documentElement.style.setProperty(name, value);
      }
    }
    for (const listener of this.themeListeners) listener(theme);
  }

  onThemeChange(listener: (theme: ThemeTokens) => void): () => void {
    this.themeListeners.add(listener);
    if (Object.keys(this.theme).length > 0) listener(this.theme);
    return () => this.themeListeners.delete(listener);
  }

  /** Fire-and-forget: the host never answers these. */
  notify(request: BridgeRequest) {
    if (this.port) this.port.postMessage(request);
    else this.queued.push(request);
  }

  async request<T>(method: BridgeMethod, path: string, body?: unknown): Promise<T> {
    await this.ready;
    const id = `r${++this.sequence}`;
    const request: BridgeRequest = { id, kind: "action", method, path, body };
    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new MulticaPluginError(408, `Multica did not answer ${method} ${path} in time`));
      }, DEFAULT_TIMEOUT_MS);
      this.pending.set(id, { resolve: resolve as (value: unknown) => void, reject, timer });
      this.port?.postMessage(request);
    });
  }
}

const bridge = new Bridge();

function storageApi(scope: "workspace" | "user") {
  return {
    async list(): Promise<StorageKey[]> {
      const result = await bridge.request<{ keys: StorageKey[] }>("GET", `/storage/${scope}`);
      return result.keys ?? [];
    },
    async get(key: string): Promise<string | null> {
      try {
        const result = await bridge.request<{ value: string }>("GET", `/storage/${scope}/${encodeURIComponent(key)}`);
        return result.value;
      } catch (error) {
        // A missing key is an ordinary outcome, not an error to handle.
        if (error instanceof MulticaPluginError && error.status === 404) return null;
        throw error;
      }
    },
    async set(key: string, value: string): Promise<void> {
      await bridge.request<void>("PUT", `/storage/${scope}/${encodeURIComponent(key)}`, { value });
    },
    async delete(key: string): Promise<void> {
      await bridge.request<void>("DELETE", `/storage/${scope}/${encodeURIComponent(key)}`);
    },
  };
}

let cachedContext: PluginContext | null = null;

export const multica = {
  context: {
    /** Who is looking, where, and which issue this surface is mounted on. */
    async get(force = false): Promise<PluginContext> {
      if (cachedContext && !force) return cachedContext;
      cachedContext = await bridge.request<PluginContext>("GET", "/context");
      return cachedContext;
    },
  },

  issue: {
    /** Defaults to the issue this surface is mounted on. */
    async get(issueId?: string): Promise<Issue> {
      const id = issueId ?? (await requireIssueId());
      return bridge.request<Issue>("GET", `/issues/${encodeURIComponent(id)}`);
    },
    async update(patch: { title?: string; description?: string }, issueId?: string): Promise<Issue> {
      const id = issueId ?? (await requireIssueId());
      return bridge.request<Issue>("PATCH", `/issues/${encodeURIComponent(id)}`, patch);
    },
    async comments(issueId?: string): Promise<Comment[]> {
      const id = issueId ?? (await requireIssueId());
      const result = await bridge.request<{ comments: Comment[] }>("GET", `/issues/${encodeURIComponent(id)}/comments`);
      return result.comments ?? [];
    },
    async comment(input: { body: string; parentId?: string }, issueId?: string): Promise<Comment> {
      const id = issueId ?? (await requireIssueId());
      return bridge.request<Comment>("POST", `/issues/${encodeURIComponent(id)}/comments`, {
        content: input.body,
        parent_id: input.parentId,
      });
    },
  },

  storage: {
    workspace: storageApi("workspace"),
    user: storageApi("user"),
  },

  hooks: {
    /**
     * Invokes one of THIS plugin's hooks — the `ui` trigger.
     *
     * The surface does not call its own backend directly, and this is not a
     * detour for its own sake: routing through the host is what makes the call
     * signed, rate limited, bounded to the granted `net:` domains, and issued a
     * short-lived callback token. A surface that fetched its own server would
     * have none of that, and the user would have no record it happened.
     *
     * The host refuses any hook this plugin's manifest did not declare with the
     * `ui` trigger.
     */
    async invoke(hookKey: string, input?: unknown): Promise<HookResult> {
      const issue = (await multica.context.get()).issue;
      return bridge.request<HookResult>("POST", `/hooks/${encodeURIComponent(hookKey)}`, {
        trigger: "ui",
        issue_id: issue?.id,
        input,
      });
    },
  },

  ui: {
    /** Ask the host for a different frame height, in CSS pixels. */
    resize(height: number) {
      bridge.notify({ id: `resize${Date.now()}`, kind: "ui.resize", height: Math.max(0, Math.round(height)) });
    },
    /** Current design tokens, and a subscription for theme switches. */
    onThemeChange(listener: (theme: ThemeTokens) => void): () => void {
      return bridge.onThemeChange(listener);
    },
  },
};

async function requireIssueId(): Promise<string> {
  const context = await multica.context.get();
  if (!context.issue) {
    throw new MulticaPluginError(400, "This surface is not mounted on an issue; pass an issue id explicitly.");
  }
  return context.issue.id;
}

export default multica;
