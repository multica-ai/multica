import { existsSync } from "fs";
import { join } from "path";
import type { LoadSpec, WorkerRequest, WorkerResponse } from "./worker-protocol";

/** The slice of Electron's UtilityProcess the engine uses. */
export interface WorkerProcess {
  postMessage(message: unknown): void;
  kill(): boolean;
  on(event: "message", listener: (message: unknown) => void): unknown;
  on(event: "exit", listener: (code: number) => void): unknown;
}

export interface VoiceEngineOptions {
  /** Folder with `sherpa-onnx.node`, passed to the worker. */
  engineDir: string;
  fork: (args: string[]) => WorkerProcess;
  /** Free the model's memory after this long without a request. */
  idleMs?: number;
  /** Give up on (and restart) a worker that takes longer than this. */
  timeoutMs?: number;
}

interface Pending {
  resolve: (response: WorkerResponse) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

type Request = { type: "load"; spec: LoadSpec } | { type: "transcribe"; spec: LoadSpec; samples: Float32Array; sampleRate: number };

/**
 * Owns the transcription worker: started on first use, kept warm between
 * dictations so the model loads once, and stopped when idle.
 */
export class VoiceEngine {
  private child: WorkerProcess | null = null;
  private readonly pending = new Map<number, Pending>();
  private nextId = 1;
  private idleTimer: ReturnType<typeof setTimeout> | null = null;
  private readonly idleMs: number;
  private readonly timeoutMs: number;

  constructor(private readonly options: VoiceEngineOptions) {
    this.idleMs = options.idleMs ?? 5 * 60_000;
    this.timeoutMs = options.timeoutMs ?? 120_000;
  }

  available(): boolean {
    return existsSync(join(this.options.engineDir, "sherpa-onnx.node"));
  }

  /** Load the model ahead of time so the first transcript comes back fast. */
  prepare(spec: LoadSpec): void {
    this.send({ type: "load", spec }).catch(() => {});
  }

  async transcribe(spec: LoadSpec, samples: Float32Array, sampleRate: number): Promise<string> {
    const response = await this.send({ type: "transcribe", spec, samples, sampleRate });
    if (!response.ok) throw new Error(response.error);
    return response.text ?? "";
  }

  /** Stop the worker, failing anything in flight. */
  stop(): void {
    this.clearIdle();
    const child = this.child;
    this.child = null;
    this.failAll(new Error("voice engine stopped"));
    child?.kill();
  }

  private send(request: Request): Promise<WorkerResponse> {
    this.clearIdle();
    const child = this.ensureChild();
    const id = this.nextId++;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error("voice engine timed out"));
        this.stop();
      }, this.timeoutMs);
      this.pending.set(id, { resolve, reject, timer });
      child.postMessage({ ...request, id } satisfies WorkerRequest);
    });
  }

  private ensureChild(): WorkerProcess {
    if (this.child) return this.child;
    const child = this.options.fork([this.options.engineDir]);
    child.on("message", (message) => {
      const response = message as WorkerResponse;
      const pending = this.pending.get(response.id);
      if (!pending) return;
      this.pending.delete(response.id);
      clearTimeout(pending.timer);
      pending.resolve(response);
      if (this.pending.size === 0) this.scheduleIdle();
    });
    child.on("exit", () => {
      if (this.child !== child) return;
      this.child = null;
      this.clearIdle();
      this.failAll(new Error("voice engine exited"));
    });
    this.child = child;
    return child;
  }

  private failAll(error: Error) {
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(error);
    }
    this.pending.clear();
  }

  private scheduleIdle() {
    this.clearIdle();
    this.idleTimer = setTimeout(() => this.stop(), this.idleMs);
  }

  private clearIdle() {
    if (this.idleTimer !== null) clearTimeout(this.idleTimer);
    this.idleTimer = null;
  }
}
