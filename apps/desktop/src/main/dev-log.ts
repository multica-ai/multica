export type DevLog = (tag: string, ...args: unknown[]) => void;

type DevLogSink = {
  readonly destroyed?: boolean;
  readonly writable?: boolean;
  on: (event: "error", listener: (error: Error) => void) => unknown;
  write: (message: string) => unknown;
};

/**
 * Renderer diagnostics are best-effort. A dev launch can outlive its terminal
 * or PTY, so a broken stderr sink must never become a second uncaught error
 * that hides the renderer failure we were trying to report.
 */
export function createBestEffortDevLog(
  sink: DevLogSink = process.stderr,
): DevLog {
  // Stream write failures such as EIO/EPIPE are delivered asynchronously via
  // `error`; without a listener Node treats them as uncaught exceptions.
  sink.on("error", () => undefined);

  return (tag, ...args) => {
    if (sink.destroyed === true || sink.writable === false) return;
    try {
      sink.write(`[renderer ${tag}] ${args.map(String).join(" ")}\n`);
    } catch {
      // Some sinks fail synchronously once their launcher has disappeared.
    }
  };
}
