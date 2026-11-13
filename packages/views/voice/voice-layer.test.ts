import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DEFAULT_VOICE_SETTINGS, useVoiceSettingsStore } from "@multica/core/voice";

const clip = vi.hoisted(() => ({
  durationMs: 1500,
  heardSpeech: true,
}));
const recordings = vi.hoisted(() => ({ started: 0, discarded: 0 }));

vi.mock("./recorder", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./recorder")>();
  class Recording {
    start() {
      recordings.started++;
      return Promise.resolve();
    }
    finish() {
      return Promise.resolve({ blob: new Blob(["audio"], { type: "audio/webm" }), ...clip });
    }
    discard() {
      recordings.discarded++;
    }
  }
  return { ...actual, Recording };
});

const engine = vi.hoisted(() => ({
  model: { id: "sensevoice-small" } as { id: string } | null,
  transcribe: vi.fn(),
  prepare: vi.fn(),
}));
vi.mock("./engine", () => ({
  activeModel: () => engine.model,
  prepareModel: engine.prepare,
  transcribeClip: engine.transcribe,
}));

import {
  cancelVoice,
  getVoiceState,
  noteVoiceFocus,
  pressVoice,
  registerVoiceTarget,
  releaseVoice,
  retryVoice,
  toggleVoice,
} from "./voice-layer";

const unregister: Array<() => void> = [];

function target(id: string, overrides: { insert?: (text: string) => boolean } = {}) {
  const inserted: string[] = [];
  const submit = vi.fn();
  const activate = vi.fn();
  const insert = vi.fn(
    overrides.insert ??
      ((text: string) => {
        inserted.push(text);
        return true;
      }),
  );
  unregister.push(registerVoiceTarget(id, { insert, submit, activate }));
  return { inserted, submit, activate, insert };
}

async function untilPhase(phase: string) {
  await vi.waitFor(() => expect(getVoiceState().phase).toBe(phase));
}

function key(type: "keydown" | "keyup", init: KeyboardEventInit) {
  window.dispatchEvent(new KeyboardEvent(type, { bubbles: true, cancelable: true, ...init }));
}

beforeEach(() => {
  clip.durationMs = 1500;
  clip.heardSpeech = true;
  recordings.started = 0;
  recordings.discarded = 0;
  engine.model = { id: "sensevoice-small" };
  engine.prepare.mockReset();
  engine.transcribe.mockReset();
  engine.transcribe.mockResolvedValue({ ok: true, text: "hello world" });
  useVoiceSettingsStore.getState().update(DEFAULT_VOICE_SETTINGS);
});

afterEach(() => {
  cancelVoice();
  unregister.splice(0).forEach((fn) => fn());
  vi.useRealTimers();
});

describe("voice layer", () => {
  it("a tap opens the strip and a second tap inserts into that composer", async () => {
    const chat = target("chat");
    pressVoice("chat");
    releaseVoice();
    await untilPhase("recording");
    expect(getVoiceState()).toMatchObject({ targetId: "chat", mode: "strip" });
    expect(chat.activate).toHaveBeenCalled();
    expect(engine.prepare).toHaveBeenCalledWith(engine.model);

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

  it("Enter during the strip inserts and sends; Escape cancels without sending audio", async () => {
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

  it("the shortcut dictates into the composer focused last", async () => {
    const chat = target("chat");
    const comment = target("comment");
    noteVoiceFocus("chat");
    key("keydown", { key: " ", altKey: true });
    key("keyup", { key: " ", altKey: true });
    await untilPhase("recording");
    expect(getVoiceState().targetId).toBe("chat");
    key("keydown", { key: " ", altKey: true });
    await untilPhase("idle");
    expect(chat.inserted).toEqual(["hello world"]);
    expect(comment.inserted).toEqual([]);
  });

  it("does not send silence", async () => {
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

  it("opens setup instead of recording when no model is installed", async () => {
    engine.model = null;
    const chat = target("chat");
    pressVoice("chat");
    releaseVoice();
    expect(getVoiceState()).toMatchObject({ targetId: "chat", phase: "setup" });
    expect(recordings.started).toBe(0);
    expect(chat.activate).not.toHaveBeenCalled();

    pressVoice("chat");
    expect(getVoiceState().phase).toBe("setup");

    engine.model = { id: "sensevoice-small" };
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
});
