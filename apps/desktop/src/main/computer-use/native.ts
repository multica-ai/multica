import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { createInterface } from "node:readline";
import { z } from "zod";

const responseSchema = z.object({ id: z.string(), result: z.unknown().optional(), error: z.string().optional() });

export class ComputerNative {
  private child?: ChildProcessWithoutNullStreams;
  private pending = new Map<string, { resolve(value: unknown): void; reject(error: Error): void }>();
  private serial: Promise<unknown> = Promise.resolve();
  private sequence = 0;
  constructor(private command: { executable: string; args: string[] }, private platform = process.platform) {}
  call(action: Record<string, unknown>, signal: AbortSignal): Promise<unknown> {
    const work = this.serial.catch(() => {}).then(() => this.perform(action, signal));
    this.serial = work;
    return work;
  }
  private perform(action: Record<string, unknown>, signal: AbortSignal): Promise<unknown> {
    signal.throwIfAborted();
    if (this.platform !== "darwin") return Promise.resolve({ available: false, platform: this.platform, accessibility: false, screenRecording: false });
    if (!this.child) {
      const child = spawn(this.command.executable, this.command.args, { stdio: "pipe", env: { ELECTRON_RUN_AS_NODE: "1", PATH: "/usr/bin:/bin", LANG: "en_US.UTF-8" } });
      this.child = child;
      const lines = createInterface({ input: child.stdout });
      let bytes = 0;
      child.stdout.on("data", (chunk: Buffer) => { bytes += chunk.length; if (bytes > 1024 * 1024) this.stop(); });
      lines.on("line", (line) => {
        bytes = 0;
        try {
          const response = responseSchema.parse(JSON.parse(line)), entry = this.pending.get(response.id);
          if (!entry) return;
          if (response.error) entry.reject(new Error(response.error)); else entry.resolve(response.result);
        } catch { this.stop(); }
      });
      child.stderr.resume();
      child.once("error", () => { if (this.child === child) this.stop(new Error("native_unavailable: Stage the cua-driver SDK")); });
      child.once("exit", () => { if (this.child === child) this.stop(); });
    }
    const id = String(++this.sequence), child = this.child;
    return new Promise((resolve, reject) => {
      const finish = (error?: Error, value?: unknown) => {
        clearTimeout(timer); signal.removeEventListener("abort", abort); this.pending.delete(id);
        if (error) reject(error); else resolve(value);
      };
      const abort = () => this.stop(new Error("execution_ended: Native action cancelled; inspect the application before retrying"));
      const timer = setTimeout(() => this.stop(new Error("outcome_unknown: Native action timed out; inspect the application before retrying")), 15000);
      this.pending.set(id, { resolve: (value) => finish(undefined, value), reject: (error) => finish(error) });
      signal.addEventListener("abort", abort, { once: true });
      if (signal.aborted) { abort(); return; }
      child.stdin.write(JSON.stringify({ id, action }) + "\n", (error) => { if (error) this.stop(); });
    });
  }
  stop(error = new Error("outcome_unknown: Native helper stopped; observe before further actions")) {
    const child = this.child; this.child = undefined;
    child?.kill();
    for (const entry of this.pending.values()) entry.reject(error);
    this.pending.clear();
  }
}
