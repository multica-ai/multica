import WebSocket from "ws";
import { z } from "zod";
import { COMPUTER_CAPABILITY_VERSION, COMPUTER_HOST_PROTOCOL_VERSION, compatibleVersion } from "./version";

const requestId = z.union([z.string(), z.number()]);
const messageSchema = z.object({
  jsonrpc: z.literal("2.0"),
  id: requestId.optional(),
  method: z.string().optional(),
  params: z.unknown().optional(),
  result: z.unknown().optional(),
  error: z.object({ code: z.number(), message: z.string() }).passthrough().optional(),
});
const controlSchema = z.object({ capabilityVersion: z.string(), envelope: z.unknown() });
const cancelSchema = z.object({ requestId });

/** What Desktop reports to the daemon; client/updated replaces it as a whole. */
export interface ComputerClientState {
  user: { id: string | null };
  capabilities: { computer: { version: string; prompts: { conversation: string; task: string; taskBrief: string }; runtime: { executable: string; entry: string } } };
}
interface Options {
  endpoint(): Promise<{ base: string; token: string }>;
  clientInfo: { name: string; version: string };
  describe(): ComputerClientState;
  handle(envelope: unknown, signal: AbortSignal): Promise<unknown>;
  stop(): void;
  log?(message: string): void;
  timing?: { handshakeMs: number; retryMs: number; incompatibleRetryMs: number };
}
const defaultTiming = { handshakeMs: 5000, retryMs: 2000, incompatibleRetryMs: 30000 };
const INITIALIZE_ID = "initialize";

/**
 * Main connects directly to this device's daemon over the Computer Host
 * protocol (docs/engineering/computer-host-protocol.md); the renderer never
 * relays actions. Nothing is sent or served before the initialize handshake.
 */
export class ComputerChannel {
  private socket?: WebSocket;
  private ready = false;
  private reported = "";
  private controller = new AbortController();
  private pending = new Map<string, AbortController>();
  private timing: typeof defaultTiming;
  constructor(private options: Options) { this.timing = options.timing ?? defaultTiming; }

  async run() {
    while (!this.controller.signal.aborted) {
      let delay = this.timing.retryMs;
      try {
        const { base, token } = await this.options.endpoint();
        if (this.controller.signal.aborted) break;
        delay = await this.connect(base, token);
      } catch { this.options.stop(); }
      if (!this.controller.signal.aborted) await new Promise<void>((resolve) => {
        const done = () => { clearTimeout(timer); this.controller.signal.removeEventListener("abort", done); resolve(); };
        const timer = setTimeout(done, delay);
        this.controller.signal.addEventListener("abort", done, { once: true });
        if (this.controller.signal.aborted) done();
      });
    }
  }

  /** Reports a changed account or capability without reconnecting. */
  update() {
    if (!this.ready) return;
    const state = this.options.describe();
    const key = JSON.stringify(state);
    if (key === this.reported) return;
    this.reported = key;
    this.send({ jsonrpc: "2.0", method: "notifications/client/updated", params: state });
  }

  stopExecution(request: { conversationId: string; turnId: string }) {
    if (this.ready) this.send({ jsonrpc: "2.0", method: "notifications/computer/stopped", params: request });
  }

  close() { this.controller.abort(); this.socket?.terminate(); this.options.stop(); }

  private send(value: unknown) {
    if (this.socket?.readyState === WebSocket.OPEN) this.socket.send(JSON.stringify(value));
  }

  /** Serves one connection and resolves with the delay before reconnecting. */
  private connect(base: string, token: string): Promise<number> {
    return new Promise((resolve) => {
      const socket = new WebSocket(`ws://${base}/local/v2/computer-host`, { headers: { Authorization: `Bearer ${token}` }, maxPayload: 1024 * 1024, handshakeTimeout: 5000 });
      this.socket = socket;
      this.ready = false;
      let retry = this.timing.retryMs;
      let handshake: ReturnType<typeof setTimeout> | undefined;
      const incompatible = (reason: string) => {
        retry = this.timing.incompatibleRetryMs;
        this.options.log?.(`[computer-use] daemon cannot serve Computer Host ${COMPUTER_HOST_PROTOCOL_VERSION}: ${reason}`);
        socket.close();
      };
      socket.once("open", () => {
        const state = this.options.describe();
        this.reported = JSON.stringify(state);
        socket.send(JSON.stringify({ jsonrpc: "2.0", id: INITIALIZE_ID, method: "initialize", params: { protocolVersion: COMPUTER_HOST_PROTOCOL_VERSION, clientInfo: this.options.clientInfo, ...state } }));
        handshake = setTimeout(() => incompatible("no initialize response"), this.timing.handshakeMs);
      });
      socket.on("message", (data) => {
        let raw: unknown;
        try { raw = JSON.parse(data.toString()); } catch { socket.close(); return; }
        const parsed = messageSchema.safeParse(raw);
        if (!parsed.success) { socket.close(); return; }
        const message = parsed.data;
        if (message.method === undefined) {
          if (message.id !== INITIALIZE_ID || this.ready) return;
          clearTimeout(handshake);
          if (message.error) { incompatible(message.error.message); return; }
          this.ready = true;
          socket.send(JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" }));
          const status = (message.result as { capabilities?: { computer?: { status?: unknown; supported?: unknown; reason?: unknown } } } | undefined)?.capabilities?.computer;
          if (status?.status !== "enabled") this.options.log?.(`[computer-use] daemon did not enable Computer ${COMPUTER_CAPABILITY_VERSION}: ${JSON.stringify(status ?? null)}`);
          return;
        }
        if (!this.ready) return;
        if (message.method === "notifications/cancelled") {
          const cancel = cancelSchema.safeParse(message.params);
          if (cancel.success) { this.pending.get(String(cancel.data.requestId))?.abort(); this.options.stop(); }
          return;
        }
        if (message.id === undefined) return;
        const id = message.id;
        const reply = (body: { result: unknown } | { error: { code: number; message: string; data?: unknown } }) => this.send({ jsonrpc: "2.0", id, ...body });
        if (message.method !== "computer/control") { reply({ error: { code: -32601, message: "method_not_found" } }); return; }
        const control = controlSchema.safeParse(message.params);
        if (!control.success) { reply({ error: { code: -32602, message: "invalid_params" } }); return; }
        if (!compatibleVersion(control.data.capabilityVersion, COMPUTER_CAPABILITY_VERSION)) {
          reply({ error: { code: -32000, message: `runtime_outdated: Desktop implements Computer ${COMPUTER_CAPABILITY_VERSION}`, data: { code: "runtime_outdated" } } });
          return;
        }
        const key = String(id);
        if (this.pending.has(key)) { socket.close(); return; }
        const abort = new AbortController(); this.pending.set(key, abort);
        void this.options.handle(control.data.envelope, abort.signal).then(
          (result) => reply({ result: result ?? null }),
          (error: unknown) => {
            const text = error instanceof Error && error.message ? error.message : "computer_action_failed";
            const code = /^[a-z_]+(?=:|$)/.exec(text)?.[0] ?? "computer_action_failed";
            reply({ error: { code: -32000, message: text, data: { code } } });
          },
        ).finally(() => this.pending.delete(key));
      });
      socket.once("error", () => socket.close());
      socket.once("close", () => {
        clearTimeout(handshake);
        this.ready = false;
        for (const pending of this.pending.values()) pending.abort();
        this.pending.clear(); this.options.stop(); resolve(retry);
      });
    });
  }
}
