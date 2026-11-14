/**
 * Utility-process entry that runs sherpa-onnx. Kept out of the main process
 * because loading a model and decoding are synchronous native calls that
 * would freeze every window while they run. argv[2] is the folder holding
 * this platform's `sherpa-onnx.node` and its libraries.
 */
import { availableParallelism } from "os";
import { createRequire } from "module";
import { join } from "path";
import {
  joinTranscripts,
  recognizerConfig,
  splitAtPauses,
  type LoadSpec,
  type WorkerRequest,
  type WorkerResponse,
} from "./worker-protocol";

interface SherpaAddon {
  createOfflineRecognizer(config: Record<string, unknown>): unknown;
  createOfflineStream(recognizer: unknown): unknown;
  acceptWaveformOffline(stream: unknown, wave: { samples: Float32Array; sampleRate: number }): void;
  decodeOfflineStream(recognizer: unknown, stream: unknown): void;
  getOfflineStreamResultAsJson(stream: unknown): string;
}

const engineDir = process.argv[2] ?? "";
const numThreads = Math.max(1, Math.min(4, Math.floor(availableParallelism() / 2)));

let addon: SherpaAddon | null = null;
let loaded: { key: string; recognizer: unknown } | null = null;

function recognizerFor(spec: LoadSpec): unknown {
  if (loaded?.key === spec.key) return loaded.recognizer;
  addon ??= createRequire(__filename)(join(engineDir, "sherpa-onnx.node")) as SherpaAddon;
  loaded = null;
  loaded = { key: spec.key, recognizer: addon.createOfflineRecognizer(recognizerConfig(spec, numThreads)) };
  return loaded.recognizer;
}

function decode(recognizer: unknown, samples: Float32Array, sampleRate: number): string {
  const stream = addon!.createOfflineStream(recognizer);
  addon!.acceptWaveformOffline(stream, { samples, sampleRate });
  addon!.decodeOfflineStream(recognizer, stream);
  const result = JSON.parse(addon!.getOfflineStreamResultAsJson(stream)) as { text?: unknown };
  return typeof result.text === "string" ? result.text : "";
}

function handle(request: WorkerRequest): WorkerResponse {
  const recognizer = recognizerFor(request.spec);
  if (request.type === "load") return { id: request.id, ok: true };
  const parts = splitAtPauses(request.samples, request.sampleRate).map((segment) =>
    decode(recognizer, segment, request.sampleRate),
  );
  return { id: request.id, ok: true, text: joinTranscripts(parts) };
}

process.parentPort.on("message", (event) => {
  const request = event.data as WorkerRequest;
  let response: WorkerResponse;
  try {
    response = handle(request);
  } catch (err) {
    response = { id: request.id, ok: false, error: err instanceof Error ? err.message : String(err) };
  }
  process.parentPort.postMessage(response);
});
