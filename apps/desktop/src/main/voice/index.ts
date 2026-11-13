import { app, BrowserWindow, ipcMain, net, utilityProcess } from "electron";
import { join } from "path";
import {
  MAX_VOICE_SAMPLES,
  VOICE_MODEL_CANCEL_CHANNEL,
  VOICE_MODEL_DELETE_CHANNEL,
  VOICE_MODEL_DOWNLOAD_CHANNEL,
  VOICE_MODELS_CHANGED_CHANNEL,
  VOICE_MODELS_LIST_CHANNEL,
  VOICE_PREPARE_CHANNEL,
  VOICE_SAMPLE_RATE,
  VOICE_TRANSCRIBE_CHANNEL,
  type VoiceModelsSnapshot,
  type VoiceTranscribeResult,
} from "../../shared/voice";
import { findVoiceModel } from "./catalog";
import { VoiceEngine } from "./engine";
import { ModelStore } from "./model-store";
import type { LoadSpec } from "./worker-protocol";
import workerPath from "./worker?modulePath";

/** Matches VOICE_LANGUAGES in @multica/core/voice. */
const LANGUAGES = new Set(["auto", "zh", "en", "ja", "ko", "yue"]);

/**
 * `asarUnpack: resources/**` puts the engine's native files next to the
 * archive; in dev the path has no `app.asar` and the replace is a no-op.
 */
function engineDir(): string {
  return join(app.getAppPath(), "resources", "voice-engine").replace("app.asar", "app.asar.unpacked");
}

export function setupVoiceInput(): void {
  const engine = new VoiceEngine({
    engineDir: engineDir(),
    fork: (args) => utilityProcess.fork(workerPath, args, { serviceName: "Multica Voice" }),
  });
  const snapshot = (): VoiceModelsSnapshot => ({ engineAvailable: engine.available(), models: store.list() });
  const store: ModelStore = new ModelStore({
    root: join(app.getPath("userData"), "voice-models"),
    fetch: (url, init) => net.fetch(url, init),
    onChange: () => {
      const next = snapshot();
      for (const win of BrowserWindow.getAllWindows()) {
        if (!win.isDestroyed()) win.webContents.send(VOICE_MODELS_CHANGED_CHANNEL, next);
      }
    },
  });
  const ready = store.init();

  const loadSpec = (request: unknown): LoadSpec | null => {
    if (!request || typeof request !== "object") return null;
    const { modelId, language } = request as { modelId?: unknown; language?: unknown };
    const model = typeof modelId === "string" ? findVoiceModel(modelId) : undefined;
    if (!model) return null;
    const hint = model.languageHint && typeof language === "string" && LANGUAGES.has(language) ? language : "auto";
    return { key: `${model.id}@${model.revision}:${hint}`, recognizer: model.recognizer, dir: store.modelDir(model.id), language: hint };
  };
  const modelIdOf = (request: unknown) => (request as { modelId: string }).modelId;

  ipcMain.handle(VOICE_MODELS_LIST_CHANNEL, async () => {
    await ready;
    return snapshot();
  });
  ipcMain.handle(VOICE_MODEL_DOWNLOAD_CHANNEL, async (_event, id: unknown) => {
    await ready;
    if (typeof id === "string") void store.download(id);
  });
  ipcMain.handle(VOICE_MODEL_CANCEL_CHANNEL, (_event, id: unknown) => {
    if (typeof id === "string") store.cancel(id);
  });
  ipcMain.handle(VOICE_MODEL_DELETE_CHANNEL, async (_event, id: unknown) => {
    if (typeof id !== "string" || !findVoiceModel(id)) return;
    await ready;
    engine.stop();
    await store.remove(id);
  });
  ipcMain.on(VOICE_PREPARE_CHANNEL, (_event, request: unknown) => {
    void ready.then(() => {
      const spec = loadSpec(request);
      if (spec && engine.available() && store.isReady(modelIdOf(request))) engine.prepare(spec);
    });
  });
  ipcMain.handle(VOICE_TRANSCRIBE_CHANNEL, async (_event, request: unknown): Promise<VoiceTranscribeResult> => {
    await ready;
    const spec = loadSpec(request);
    const samples = (request as { samples?: unknown } | null)?.samples;
    if (!spec || !(samples instanceof Float32Array) || samples.length === 0 || samples.length > MAX_VOICE_SAMPLES) {
      return { ok: false, reason: "failed", error: "invalid request" };
    }
    if (!engine.available()) return { ok: false, reason: "engine-unavailable" };
    if (!store.isReady(modelIdOf(request))) return { ok: false, reason: "model-missing" };
    try {
      return { ok: true, text: await engine.transcribe(spec, samples, VOICE_SAMPLE_RATE) };
    } catch (err) {
      return { ok: false, reason: "failed", error: err instanceof Error ? err.message : String(err) };
    }
  });

  app.on("will-quit", () => engine.stop());
}
