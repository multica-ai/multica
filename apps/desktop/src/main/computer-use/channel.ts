import WebSocket from "ws";
import { z } from "zod";

const messageSchema = z.discriminatedUnion("type", [
  z.object({ type: z.literal("request"), id: z.string(), payload: z.unknown() }),
  z.object({ type: z.literal("cancel"), id: z.string() }),
]);
interface Options {
  endpoint(): Promise<{ base: string; token: string }>;
  handle(payload: unknown, signal: AbortSignal): Promise<unknown>;
  stop(): void;
}

/** Main connects directly to this device's daemon; the renderer never relays actions. */
export class ComputerChannel {
  private socket?: WebSocket;
  private controller = new AbortController();
  private pending = new Map<string, AbortController>();
  constructor(private options: Options) {}
  async run() {
    while (!this.controller.signal.aborted) {
      try {
        const { base, token } = await this.options.endpoint();
        if (this.controller.signal.aborted) break;
        await new Promise<void>((resolve) => {
          const socket = new WebSocket(`ws://${base}/local/v2/computer-host`, { headers: { Authorization: `Bearer ${token}` }, maxPayload: 1024 * 1024, handshakeTimeout: 5000 });
          this.socket = socket;
          socket.on("message", (data) => {
            let raw: unknown;
            try { raw = JSON.parse(data.toString()); } catch { socket.close(); return; }
            const parsed = messageSchema.safeParse(raw);
            if (!parsed.success) { socket.close(); return; }
            const message = parsed.data;
            if (message.type === "cancel") { this.pending.get(message.id)?.abort(); this.options.stop(); return; }
            if (this.pending.has(message.id)) { socket.close(); return; }
            const abort = new AbortController(); this.pending.set(message.id, abort);
            const reply = (value: unknown) => { if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify(value)); };
            void this.options.handle(message.payload, abort.signal).then(
              (result) => reply({ id: message.id, result }),
              (error: unknown) => reply({ id: message.id, error: error instanceof Error ? error.message : "computer_action_failed" }),
            ).finally(() => this.pending.delete(message.id));
          });
          socket.once("error", () => socket.close());
          socket.once("close", () => {
            for (const pending of this.pending.values()) pending.abort();
            this.pending.clear(); this.options.stop(); resolve();
          });
        });
      } catch { this.options.stop(); }
      if (!this.controller.signal.aborted) await new Promise<void>((resolve) => {
        const done = () => { clearTimeout(timer); this.controller.signal.removeEventListener("abort", done); resolve(); };
        const timer = setTimeout(done, 2000);
        this.controller.signal.addEventListener("abort", done, { once: true });
        if (this.controller.signal.aborted) done();
      });
    }
  }
  stopExecution(request: { conversationId: string; turnId: string }) {
    if (this.socket?.readyState === WebSocket.OPEN) this.socket.send(JSON.stringify({ type: "stop", ...request }));
  }
  close() { this.controller.abort(); this.socket?.terminate(); this.options.stop(); }
}
