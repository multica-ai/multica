import { app, BrowserWindow, ipcMain, net, utilityProcess } from "electron";
import { join } from "path";
import {
  MAX_VOICE_SAMPLES,
  VOICE_HISTORY_AUDIO_CHANNEL,
  VOICE_HISTORY_CHANGED_CHANNEL,
  VOICE_HISTORY_CLEAR_CHANNEL,
  VOICE_HISTORY_LIST_CHANNEL,
  VOICE_HISTORY_REMOVE_CHANNEL,
  VOICE_HISTORY_RETENTION_CHANNEL,
  VOICE_HISTORY_SAVE_CHANNEL,
  VOICE_HISTORY_UPDATE_CHANNEL,
  VOICE_MODEL_CANCEL_CHANNEL,
  VOICE_MODEL_DELETE_CHANNEL,
  VOICE_MODEL_DOWNLOAD_CHANNEL,
  VOICE_MODELS_CHANGED_CHANNEL,
  VOICE_MODELS_LIST_CHANNEL,
  VOICE_PREPARE_CHANNEL,
  VOICE_SAMPLE_RATE,
  VOICE_TRANSCRIBE_CHANNEL,
  type VoiceHistoryAudio,
  type VoiceHistoryEntry,
  type VoiceHistoryStatus,
  type VoiceModelsSnapshot,
  type VoiceTranscribeResult,
} from "../../shared/voice";
import { findVoiceModel } from "./catalog";
import { VoiceEngine } from "./engine";
import { MAX_HISTORY_AUDIO_BYTES, VoiceHistoryStore } from "./history-store";
import { ModelStore } from "./model-store";
import { hotwordList, type LoadSpec } from "./worker-protocol";
import workerPath from "./worker?modulePath";

const HISTORY_STATUSES = new Set<VoiceHistoryStatus>(["transcribing", "done", "failed", "interrupted"]);

function keepDaysOf(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : null;
}

function broadcast(channel: string, payload?: unknown) {
  for (const win of BrowserWindow.getAllWindows()) {
    if (!win.isDestroyed()) win.webContents.send(channel, payload);
  }
}

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
    onChange: () => broadcast(VOICE_MODELS_CHANGED_CHANNEL, snapshot()),
  });
  const ready = store.init();
  const history = new VoiceHistoryStore({
    root: join(app.getPath("userData"), "voice-history"),
    onChange: () => broadcast(VOICE_HISTORY_CHANGED_CHANNEL),
  });
  const historyReady = history.init().catch(() => {});

  const loadSpec = (request: unknown): LoadSpec | null => {
    if (!request || typeof request !== "object") return null;
    const { modelId, language, hotwords } = request as { modelId?: unknown; language?: unknown; hotwords?: unknown };
    const model = typeof modelId === "string" ? findVoiceModel(modelId) : undefined;
    if (!model) return null;
    const hint = model.languageHint && typeof language === "string" && LANGUAGES.has(language) ? language : "auto";
    const words = model.hotwords ? hotwordList(hotwords) : "";
    return {
      key: `${model.id}@${model.revision}:${hint}:${words}`,
      recognizer: model.recognizer,
      dir: store.modelDir(model.id),
      language: hint,
      hotwords: words,
    };
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

  ipcMain.handle(VOICE_HISTORY_LIST_CHANNEL, async (): Promise<VoiceHistoryEntry[]> => {
    await historyReady;
    await history.expire();
    return history.list();
  });
  ipcMain.handle(VOICE_HISTORY_SAVE_CHANNEL, async (_event, request: unknown): Promise<VoiceHistoryEntry | null> => {
    await historyReady;
    const { audio, mimeType, durationMs, keepDays } = (request ?? {}) as Record<string, unknown>;
    if (!(audio instanceof Uint8Array) || audio.byteLength === 0 || audio.byteLength > MAX_HISTORY_AUDIO_BYTES) return null;
    if (typeof mimeType !== "string" || typeof durationMs !== "number" || !Number.isFinite(durationMs)) return null;
    return history.save(audio, mimeType.slice(0, 100), Math.max(0, durationMs), keepDaysOf(keepDays));
  });
  ipcMain.handle(VOICE_HISTORY_UPDATE_CHANNEL, async (_event, id: unknown, patch: unknown) => {
    await historyReady;
    const { status, text } = (patch ?? {}) as Record<string, unknown>;
    if (typeof id !== "string" || !HISTORY_STATUSES.has(status as VoiceHistoryStatus)) return;
    await history.update(id, { status: status as VoiceHistoryStatus, text: typeof text === "string" ? text : undefined });
  });
  ipcMain.handle(VOICE_HISTORY_AUDIO_CHANNEL, async (_event, id: unknown): Promise<VoiceHistoryAudio | null> => {
    await historyReady;
    return typeof id === "string" ? history.audio(id) : null;
  });
  ipcMain.handle(VOICE_HISTORY_REMOVE_CHANNEL, async (_event, id: unknown) => {
    await historyReady;
    if (typeof id === "string") await history.remove(id);
  });
  ipcMain.handle(VOICE_HISTORY_RETENTION_CHANNEL, async (_event, keepDays: unknown) => {
    await historyReady;
    if (keepDays === null || typeof keepDays === "number") await history.setRetention(keepDaysOf(keepDays));
  });
  ipcMain.handle(VOICE_HISTORY_CLEAR_CHANNEL, async () => {
    await historyReady;
    await history.clear();
  });

  app.on("will-quit", () => engine.stop());
}
