/** The native log's write side. The log itself lives in Go, shared with the link. */
export interface LogSink {
  appendLogLines(source: "app" | "marks", lines: string): void;
}

export type LogLevel = "debug" | "info" | "warning" | "error";

const FLUSH_DELAY_MS = 2000;
const MAX_MESSAGE_CHARS = 16 * 1024;
const MAX_PENDING_LINES = 2000;

function describeError(error: Error) {
  return error.stack ? `${error.name}: ${error.message}\n${error.stack}` : `${error.name}: ${error.message}`;
}

export function formatLogArguments(args: readonly unknown[]): string {
  return args.map((arg) => {
    if (typeof arg === "string") return arg;
    if (arg instanceof Error) return describeError(arg);
    try {
      return JSON.stringify(arg, (_key, value: unknown) => value instanceof Error ? describeError(value) : value) ?? String(arg);
    } catch { return String(arg); }
  }).join(" ");
}

/**
 * Batches what the app logs into the native log. Nothing here may throw into the
 * code being logged, or log through the console it captures.
 */
export class LogCapture {
  private pending = { app: [] as string[], marks: [] as string[] };
  private timer?: ReturnType<typeof setTimeout>;
  private workspaceId: string | null = null;

  constructor(private sink: LogSink | null, private schedule: (flush: () => void, delayMs: number) => ReturnType<typeof setTimeout> = setTimeout) {}

  /** Labels later records with where the app was, which is not isolation: reads return every workspace's. */
  setWorkspace(workspaceId: string | null) { this.workspaceId = workspaceId; }

  log(level: LogLevel, args: readonly unknown[]) {
    // Debug is per-event noise, which would push out what a read is looking for.
    if (level === "debug" || !this.sink) return;
    try {
      const message = formatLogArguments(args);
      this.enqueue("app", JSON.stringify({
        at: new Date().toISOString(), source: "app", level,
        ...(this.workspaceId ? { workspaceId: this.workspaceId } : {}),
        message: message.length > MAX_MESSAGE_CHARS ? `${message.slice(0, MAX_MESSAGE_CHARS)}… [truncated]` : message,
      }));
      // A fatal error may be the process's last act.
      if (level === "error") this.flush();
    } catch { /* Diagnostics must not change behavior. */ }
  }

  mark(record: { at: string }) {
    if (!this.sink) return;
    try { this.enqueue("marks", JSON.stringify({ at: record.at, source: "marks", record })); } catch { /* Diagnostics must not change behavior. */ }
  }

  private enqueue(source: "app" | "marks", line: string) {
    const lines = this.pending[source];
    lines.push(line);
    if (lines.length > MAX_PENDING_LINES) lines.shift();
    if (this.timer === undefined) this.timer = this.schedule(() => { this.timer = undefined; this.flush(); }, FLUSH_DELAY_MS);
  }

  flush() {
    for (const source of ["app", "marks"] as const) {
      const lines = this.pending[source];
      if (!lines.length) continue;
      try {
        this.sink?.appendLogLines(source, lines.join("\n"));
        this.pending[source] = [];
      } catch { /* Kept for the next flush, within its bound. */ }
    }
  }

  /** Forgets what has not reached the log, for a sign-out about to clear it. */
  discard() { this.pending = { app: [], marks: [] }; }
}
