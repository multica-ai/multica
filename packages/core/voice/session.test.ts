/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { setVoiceContextWords } from "./hotwords";
import { useVoiceModelsStore, type VoiceModel, type VoiceModels } from "./models";
import {
  cancelVoice,
  connectVoiceRuntime,
  getVoiceState,
  noteVoiceFocus,
  pressVoice,
  registerVoiceTarget,
  releaseVoice,
  retryVoice,
  toggleVoice,
  transcribeVoiceClip,
  type VoiceTarget,
} from "./session";
import { DEFAULT_VOICE_SETTINGS, useVoiceSettingsStore } from "./store";
import { createShortcutChord } from "../shortcuts/definitions";
import { useShortcutStore } from "../shortcuts/store";

const MODEL: VoiceModel = {
  id: "sensevoice-small",
  name: "SenseVoice Small",
  author: "FunAudioLLM",
  licenseUrl: "https://example.com/license",
  languages: ["zh", "en"],
  languageHint: true,
  sizeBytes: 1,
  status: "ready",
  receivedBytes: 1,
};
const INSTALLED: VoiceModels = { engineAvailable: true, models: [MODEL] };

const clip = { durationMs: 1500, heardSpeech: true };
const recordings = { started: 0, discarded: 0 };
const engine = { transcribe: vi.fn(), prepare: vi.fn() };
const history = { save: vi.fn(), update: vi.fn() };

beforeAll(() => {
  connectVoiceRuntime(() => ({
    record: () => ({
      start: () => {
        recordings.started++;
        return Promise.resolve();
      },
      finish: () => Promise.resolve({ blob: new Blob(["audio"], { type: "audio/webm" }), ...clip }),
      discard: () => {
        recordings.discarded++;
      },
    }),
    prepare: (...args) => engine.prepare(...args),
    transcribe: (...args) => engine.transcribe(...args),
    watchModels: (onChange) => onChange(INSTALLED),
    history: {
      save: (...args) => history.save(...args),
      update: (...args) => history.update(...args),
    },
  }));
});

const unregister: Array<() => void> = [];

function target(id: string, overrides: Partial<VoiceTarget> = {}) {
  const inserted: string[] = [];
  const submit = vi.fn();
  const activate = vi.fn();
  const insert = vi.fn((text: string) => {
    inserted.push(text);
    return true;
  });
  unregister.push(registerVoiceTarget(id, { insert, submit, activate, ...overrides }));
  return { inserted, submit, activate, insert };
}

async function untilPhase(phase: string) {
  await vi.waitFor(() => expect(getVoiceState().phase).toBe(phase));
}

function key(type: "keydown" | "keyup", init: KeyboardEventInit) {
  window.dispatchEvent(new KeyboardEvent(type, { bubbles: true, cancelable: true, ...init }));
}

function shortcut() {
  key("keydown", { key: " ", altKey: true });
  key("keyup", { key: " ", altKey: true });
}

beforeEach(() => {
  clip.durationMs = 1500;
  clip.heardSpeech = true;
  recordings.started = 0;
  recordings.discarded = 0;
  useVoiceModelsStore.getState().setModels(INSTALLED);
  engine.prepare.mockReset();
  engine.transcribe.mockReset();
  engine.transcribe.mockResolvedValue({ ok: true, text: "hello world" });
  history.save.mockReset();
  history.save.mockResolvedValue("h1");
  history.update.mockReset();
  useVoiceSettingsStore.getState().update(DEFAULT_VOICE_SETTINGS);
  useShortcutStore.getState().setShortcut("voiceInput", createShortcutChord("Space", { alt: true }));
});

afterEach(() => {
  useShortcutStore.getState().resetShortcut("voiceInput");
  cancelVoice();
  unregister.splice(0).forEach((fn) => fn());
  vi.useRealTimers();
});

describe("voice session", () => {
  it("a tap opens the strip and a second tap inserts into that composer", async () => {
    const chat = target("chat");
    pressVoice("chat");
    releaseVoice();
    await untilPhase("recording");
    expect(getVoiceState()).toMatchObject({ targetId: "chat", mode: "strip" });
    expect(chat.activate).toHaveBeenCalled();
    expect(engine.prepare).toHaveBeenCalledWith(MODEL, { language: "auto", hotwords: [] });

    pressVoice("chat");
    await untilPhase("idle");
    expect(chat.inserted).toEqual(["hello world"]);
    expect(chat.submit).not.toHaveBeenCalled();
  });

  it("holding switches to the HUD and releasing inserts", async () => {
    vi.useFakeTimers();
    const chat = target("chat");
    pressVoice("chat");
    await vi.advanceTimersByTimeAsync(300);
    expect(getVoiceState()).toMatchObject({ phase: "recording", mode: "hud" });
    releaseVoice();
    vi.useRealTimers();
    await untilPhase("idle");
    expect(chat.inserted).toEqual(["hello world"]);
  });

  it("Enter during the strip inserts and sends; Escape cancels without transcribing", async () => {
    const chat = target("chat");
    pressVoice("chat");
    releaseVoice();
    await untilPhase("recording");
    key("keydown", { key: "Enter" });
    await untilPhase("idle");
    expect(chat.inserted).toEqual(["hello world"]);
    expect(chat.submit).toHaveBeenCalledTimes(1);

    pressVoice("chat");
    releaseVoice();
    await untilPhase("recording");
    key("keydown", { key: "Escape" });
    expect(getVoiceState().phase).toBe("idle");
    expect(engine.transcribe).toHaveBeenCalledTimes(1);
  });

  it("has no shortcut until the user assigns one", () => {
    useShortcutStore.getState().resetShortcut("voiceInput");
    target("chat");
    noteVoiceFocus("chat");
    shortcut();
    expect(getVoiceState().phase).toBe("idle");
    expect(recordings.started).toBe(0);
  });

  it("the shortcut dictates into the composer focused last", async () => {
    const chat = target("chat");
    const comment = target("comment");
    noteVoiceFocus("chat");
    shortcut();
    await untilPhase("recording");
    expect(getVoiceState().targetId).toBe("chat");
    key("keydown", { key: " ", altKey: true });
    await untilPhase("idle");
    expect(chat.inserted).toEqual(["hello world"]);
    expect(comment.inserted).toEqual([]);
  });

  it("the shortcut skips composers that are off screen", async () => {
    target("comment");
    target("chat", { isShown: () => false });
    noteVoiceFocus("chat");
    shortcut();
    await untilPhase("recording");
    expect(getVoiceState().targetId).toBe("comment");
  });

  it("the shortcut does nothing when every composer is off screen", () => {
    target("chat", { isShown: () => false });
    shortcut();
    expect(getVoiceState().phase).toBe("idle");
    expect(recordings.started).toBe(0);
  });

  it("does not transcribe silence", async () => {
    clip.heardSpeech = false;
    target("chat");
    pressVoice("chat");
    releaseVoice();
    await untilPhase("recording");
    pressVoice("chat");
    await untilPhase("error");
    expect(getVoiceState().error).toBe("no-speech");
    expect(engine.transcribe).not.toHaveBeenCalled();
  });

  it("keeps a failed clip and retries it without recording again", async () => {
    engine.transcribe.mockResolvedValueOnce({ ok: false, reason: "failed", error: "decoder crashed" });
    const chat = target("chat");
    pressVoice("chat");
    releaseVoice();
    await untilPhase("recording");
    pressVoice("chat");
    await untilPhase("error");
    expect(getVoiceState()).toMatchObject({ error: "transcription-failed", canRetry: true });

    retryVoice();
    await untilPhase("idle");
    expect(chat.inserted).toEqual(["hello world"]);
    expect(engine.transcribe).toHaveBeenCalledTimes(2);
    expect(recordings.started).toBe(1);
  });

  it("waits for a lazily mounted editor before inserting", async () => {
    let ready = false;
    const inserted: string[] = [];
    target("reply", {
      insert: (text) => {
        if (!ready) return false;
        inserted.push(text);
        return true;
      },
    });
    pressVoice("reply");
    releaseVoice();
    await untilPhase("recording");
    pressVoice("reply");
    setTimeout(() => {
      ready = true;
    }, 150);
    await untilPhase("idle");
    expect(inserted).toEqual(["hello world"]);
  });

  it("ignores another composer while one is recording, and cancels when its composer unmounts", async () => {
    target("chat");
    target("comment");
    pressVoice("chat");
    releaseVoice();
    await untilPhase("recording");
    pressVoice("comment");
    expect(getVoiceState()).toMatchObject({ targetId: "chat", phase: "recording" });

    unregister.shift()?.();
    expect(getVoiceState().phase).toBe("idle");
    expect(recordings.discarded).toBeGreaterThan(0);
    expect(engine.transcribe).not.toHaveBeenCalled();
  });

  it("drops a transcript still on its way when its composer unregisters", async () => {
    let resolve!: (value: unknown) => void;
    engine.transcribe.mockReturnValueOnce(new Promise((done) => (resolve = done)));
    const chat = target("chat");
    pressVoice("chat");
    releaseVoice();
    await untilPhase("recording");
    key("keydown", { key: "Enter" });
    await untilPhase("transcribing");

    unregister.shift()?.();
    const next = target("chat");
    resolve({ ok: true, text: "meant for the old conversation" });
    await Promise.resolve();
    await Promise.resolve();
    expect(getVoiceState().phase).toBe("idle");
    expect(chat.inserted).toEqual([]);
    expect(next.inserted).toEqual([]);
    expect(next.submit).not.toHaveBeenCalled();
  });

  it("opens setup instead of recording when no model is installed", async () => {
    useVoiceModelsStore.getState().setModels({ engineAvailable: true, models: [{ ...MODEL, status: "absent" }] });
    const chat = target("chat");
    pressVoice("chat");
    releaseVoice();
    expect(getVoiceState()).toMatchObject({ targetId: "chat", phase: "setup" });
    expect(recordings.started).toBe(0);
    expect(chat.activate).not.toHaveBeenCalled();

    pressVoice("chat");
    expect(getVoiceState().phase).toBe("setup");

    useVoiceModelsStore.getState().setModels(INSTALLED);
    toggleVoice("chat");
    await untilPhase("recording");
    expect(getVoiceState()).toMatchObject({ targetId: "chat", mode: "strip" });
  });

  it("returns to setup when the model was deleted mid-dictation", async () => {
    engine.transcribe.mockResolvedValueOnce({ ok: false, reason: "model-missing" });
    target("chat");
    pressVoice("chat");
    releaseVoice();
    await untilPhase("recording");
    pressVoice("chat");
    await untilPhase("setup");
  });

  it("spells vocabulary words the user's way and drops fillers", async () => {
    engine.transcribe.mockResolvedValueOnce({ ok: true, text: "um, assign it to lambda" });
    useVoiceSettingsStore.getState().update({ vocabulary: ["Lambda"] });
    const chat = target("chat");
    pressVoice("chat");
    releaseVoice();
    await untilPhase("recording");
    pressVoice("chat");
    await untilPhase("idle");
    expect(chat.inserted).toEqual(["assign it to Lambda"]);
  });

  it("offers vocabulary and workspace names to the model as hotwords", async () => {
    useVoiceSettingsStore.getState().update({ vocabulary: ["Multica"], language: "zh" });
    setVoiceContextWords(["Lambda", "multica", "Jiayuan"]);
    target("chat");
    pressVoice("chat");
    releaseVoice();
    await untilPhase("recording");
    pressVoice("chat");
    await untilPhase("idle");
    const options = { language: "zh", hotwords: ["Multica", "Lambda", "Jiayuan"] };
    expect(engine.prepare).toHaveBeenCalledWith(MODEL, options);
    expect(engine.transcribe).toHaveBeenCalledWith(expect.any(Blob), MODEL, options);
    setVoiceContextWords([]);
  });
});

describe("voice history", () => {
  async function dictate(id = "chat") {
    pressVoice(id);
    releaseVoice();
    await untilPhase("recording");
    pressVoice(id);
  }

  it("keeps the recording before transcribing, then its text as inserted", async () => {
    engine.transcribe.mockResolvedValueOnce({ ok: true, text: "呃，assign it to lambda" });
    useVoiceSettingsStore.getState().update({ vocabulary: ["Lambda"], keepHistory: "7d" });
    target("chat");
    await dictate();
    await untilPhase("idle");
    expect(history.save).toHaveBeenCalledWith(expect.objectContaining({ durationMs: 1500 }), 7);
    await vi.waitFor(() => expect(history.update).toHaveBeenCalledWith("h1", "done", "assign it to Lambda"));
  });

  it("marks a failed transcription failed, and a retry runs on the same entry", async () => {
    engine.transcribe.mockResolvedValueOnce({ ok: false, reason: "failed" });
    target("chat");
    await dictate();
    await untilPhase("error");
    await vi.waitFor(() => expect(history.update).toHaveBeenCalledWith("h1", "failed", undefined));
    retryVoice();
    await untilPhase("idle");
    expect(history.save).toHaveBeenCalledTimes(1);
    await vi.waitFor(() => expect(history.update).toHaveBeenLastCalledWith("h1", "done", "hello world"));
  });

  it("keeps a recording cancelled mid-transcription as interrupted", async () => {
    engine.transcribe.mockReturnValueOnce(new Promise(() => {}));
    target("chat");
    await dictate();
    await untilPhase("transcribing");
    await vi.waitFor(() => expect(history.save).toHaveBeenCalled());
    cancelVoice();
    await vi.waitFor(() => expect(history.update).toHaveBeenCalledWith("h1", "interrupted", undefined));
  });

  it("keeps nothing when history is off", async () => {
    useVoiceSettingsStore.getState().update({ keepHistory: "off" });
    target("chat");
    await dictate();
    await untilPhase("idle");
    expect(history.save).not.toHaveBeenCalled();
  });

  it("transcribes a kept recording again outside any composer", async () => {
    engine.transcribe.mockResolvedValueOnce({ ok: true, text: "嗯，再试一次" });
    await expect(transcribeVoiceClip(new Blob(["audio"]))).resolves.toBe("再试一次");
    engine.transcribe.mockResolvedValueOnce({ ok: false, reason: "failed" });
    await expect(transcribeVoiceClip(new Blob(["audio"]))).resolves.toBeNull();
  });
});
