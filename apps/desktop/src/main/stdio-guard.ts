type StdioStream = {
  on: (event: "error", listener: (error: Error) => void) => unknown;
};

/**
 * Logging must never take the app down. A desktop launch often has no one
 * reading its stdout/stderr (a closed terminal, an exited launcher, a session
 * pipe whose reader is gone), so a write fails with EPIPE/EIO. The failure
 * arrives as a stream `error`, and without a listener it becomes an uncaught
 * exception in the main process. That covers our own `console.*` calls and
 * dependencies that log through `console`, such as electron-updater.
 */
export function ignoreBrokenStdio(
  streams: readonly StdioStream[] = [process.stdout, process.stderr],
): void {
  for (const stream of streams) stream.on("error", () => undefined);
}
