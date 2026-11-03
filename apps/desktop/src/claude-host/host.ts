import { createInterface } from "node:readline";
import type { Readable, Writable } from "node:stream";
import desktopPackage from "../../package.json";
import { ClaudeAdapter, type ClaudeSdk } from "./adapter";
import { FrameWriter, WriteBacklogError } from "./frame-writer";
import { CLAUDE_ADAPTER_CAPABILITIES, CLAUDE_ADAPTER_PROTOCOL_VERSION, daemonMessageSchema } from "./protocol";

const SDK_VERSION = desktopPackage.dependencies["@anthropic-ai/claude-agent-sdk"];
const MAX_QUEUED_BYTES = 16 << 20;
const DRAIN_TIMEOUT_MS = 30_000;
const MAX_INPUT_LINE_BYTES = 64 << 20;

export interface HostOptions {
  input: Readable;
  output: Writable;
  sdk: () => Promise<ClaudeSdk>;
  /** Called once with the process exit code. */
  exit(code: number): void;
  log?(message: string): void;
}

/**
 * Runs the adapter over NDJSON stdio. A missing or unsupported protocol
 * version, a daemon that stops reading, or a broken stdout ends the process;
 * the daemon restarts the adapter for the conversation's next turn.
 */
export function runClaudeHost(options: HostOptions): void {
  const writer = new FrameWriter(options.output, { maxQueuedBytes: MAX_QUEUED_BYTES, drainTimeoutMs: DRAIN_TIMEOUT_MS });
  const adapter = new ClaudeAdapter(options.sdk, writer);
  let exited = false;
  const exit = (code: number, reason?: string) => {
    if (exited) return;
    exited = true;
    if (reason) options.log?.(reason);
    options.exit(code);
  };
  const fatal = (error: unknown) => exit(error instanceof WriteBacklogError ? 3 : 1, error instanceof Error ? error.message : String(error));
  const send = (frame: Parameters<FrameWriter["write"]>[0]) => writer.write(frame).catch(fatal);

  void send({ version: CLAUDE_ADAPTER_PROTOCOL_VERSION, type: "ready", sdk_version: SDK_VERSION, capabilities: CLAUDE_ADAPTER_CAPABILITIES });

  const lines = createInterface({ input: options.input, crlfDelay: Infinity });
  lines.on("line", (line) => {
    if (!line.trim()) return;
    if (Buffer.byteLength(line) > MAX_INPUT_LINE_BYTES) { void send({ version: 1, type: "error", code: "frame_too_large", message: "daemon frame exceeds the input limit" }); return; }
    let raw: unknown;
    try { raw = JSON.parse(line); } catch { void send({ version: 1, type: "error", code: "invalid_json", message: "daemon frame is not JSON" }); return; }
    const version = (raw as { version?: unknown } | null)?.version;
    if (version !== CLAUDE_ADAPTER_PROTOCOL_VERSION) {
      void writer.write({ version: 1, type: "error", code: "unsupported_version", message: `adapter speaks protocol ${CLAUDE_ADAPTER_PROTOCOL_VERSION}, got ${String(version)}` })
        .finally(() => exit(2));
      return;
    }
    const parsed = daemonMessageSchema.safeParse(raw);
    if (!parsed.success) {
      const id = typeof (raw as { id?: unknown }).id === "string" ? (raw as { id: string }).id : undefined;
      void send({ version: 1, type: "error", ...(id ? { id } : {}), code: "invalid_request", message: parsed.error.issues[0]?.message ?? "invalid request" });
      return;
    }
    void adapter.handle(parsed.data).then(() => { if (parsed.data.type === "close") exit(0); }, fatal);
  });
  lines.on("close", () => { void adapter.close().finally(() => exit(0)); });
  options.output.on("error", fatal);
}
