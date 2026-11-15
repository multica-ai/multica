// @vitest-environment node
import { EventEmitter } from "events";
import { afterEach, describe, expect, it, vi } from "vitest";
import { VoiceEngine, type WorkerProcess } from "./engine";
import type { LoadSpec, WorkerRequest } from "./worker-protocol";

const SPEC: LoadSpec = { key: "m@1:auto:", recognizer: "sense-voice", dir: "/models/m", language: "auto", hotwords: "" };

class FakeWorker extends EventEmitter implements WorkerProcess {
  sent: WorkerRequest[] = [];
  killed = false;
  constructor(private readonly reply: (request: WorkerRequest) => unknown) {
    super();
  }
  postMessage(message: unknown) {
    const request = message as WorkerRequest;
    this.sent.push(request);
    const response = this.reply(request);
    if (response) queueMicrotask(() => this.emit("message", response));
  }
  kill() {
    this.killed = true;
    queueMicrotask(() => this.emit("exit", 0));
    return true;
  }
}

function engine(reply: (request: WorkerRequest) => unknown, options: { idleMs?: number; timeoutMs?: number } = {}) {
  const workers: FakeWorker[] = [];
  const voice = new VoiceEngine({
    engineDir: "/engine",
    fork: (args) => {
      expect(args).toEqual(["/engine"]);
      const worker = new FakeWorker(reply);
      workers.push(worker);
      return worker;
    },
    ...options,
  });
  return { voice, workers };
}

afterEach(() => {
  vi.useRealTimers();
});

describe("VoiceEngine", () => {
  it("keeps one worker warm across dictations", async () => {
    const { voice, workers } = engine((request) => ({ id: request.id, ok: true, text: "hi" }));
    voice.prepare(SPEC);
    const samples = new Float32Array(16000);
    await expect(voice.transcribe(SPEC, samples, 16000)).resolves.toBe("hi");
    await expect(voice.transcribe(SPEC, samples, 16000)).resolves.toBe("hi");
    expect(workers).toHaveLength(1);
    expect(workers[0]!.sent.map((r) => r.type)).toEqual(["load", "transcribe", "transcribe"]);
  });

  it("surfaces worker errors", async () => {
    const { voice } = engine((request) => ({ id: request.id, ok: false, error: "bad model" }));
    await expect(voice.transcribe(SPEC, new Float32Array(10), 16000)).rejects.toThrow("bad model");
  });

  it("restarts after the worker dies or hangs", async () => {
    vi.useFakeTimers();
    let hang = true;
    const { voice, workers } = engine((request) => (hang ? null : { id: request.id, ok: true, text: "ok" }), {
      timeoutMs: 1000,
    });
    const first = voice.transcribe(SPEC, new Float32Array(10), 16000);
    const assertion = expect(first).rejects.toThrow("timed out");
    await vi.advanceTimersByTimeAsync(1001);
    await assertion;
    expect(workers[0]!.killed).toBe(true);

    hang = false;
    await expect(voice.transcribe(SPEC, new Float32Array(10), 16000)).resolves.toBe("ok");
    expect(workers).toHaveLength(2);
  });

  it("stops the worker once idle", async () => {
    vi.useFakeTimers();
    const { voice, workers } = engine((request) => ({ id: request.id, ok: true, text: "" }), { idleMs: 5000 });
    await voice.transcribe(SPEC, new Float32Array(10), 16000);
    await vi.advanceTimersByTimeAsync(4999);
    expect(workers[0]!.killed).toBe(false);
    await vi.advanceTimersByTimeAsync(1);
    expect(workers[0]!.killed).toBe(true);
  });
});
