/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { useVoiceModelsStore, type VoiceModels } from "./models";
import {
  cancelVoice,
  connectVoiceRuntime,
  getVoiceState,
  pressVoice,
  registerVoiceTarget,
  releaseVoice,
  retryVoice,
  stopVoice,
  useVoiceSessionStore,
  VoiceRecorderError,
  type VoiceStreamHandlers,
  type VoiceTarget,
} from "./session";
import { DEFAULT_VOICE_SETTINGS, useVoiceSettingsStore } from "./store";

const MODELS: VoiceModels = {
  engineAvailable: true,
  models: [
    {
      id: "server",
      name: "Server",
      author: "",
      licenseUrl: "",
      languages: ["en"],
      languageHint: false,
      sizeBytes: 0,
      status: "ready",
      receivedBytes: 0,
      streams: true,
    },
  ],
};

/** The open stream, driven by each test as the provider would. */
const stream = {
  handlers: null as VoiceStreamHandlers | null,
  start: vi.fn<() => Promise<void>>(),
  finish: vi.fn<() => Promise<void>>(),
  discard: vi.fn(),
  record: vi.fn(),
};

beforeAll(() => {
  connectVoiceRuntime(() => ({
    record: (...args) => stream.record(...args),
    prepare: () => {},
    transcribe: () => Promise.resolve({ ok: false, reason: "engine-unavailable" }),
    watchModels: (onChange) => onChange(MODELS),
    stream: (handlers) => {
      stream.handlers = handlers;
      return { start: () => stream.start(), finish: () => stream.finish(), discard: () => stream.discard() };
    },
  }));
});

const unregister: Array<() => void> = [];

function target(id: string, overrides: Partial<VoiceTarget> = {}) {
  const inserted: string[] = [];
  const submit = vi.fn();
  const insert = vi.fn((text: string) => {
    inserted.push(text);
    return true;
  });
  unregister.push(registerVoiceTarget(id, { insert, submit, ...overrides }));
  return { inserted, submit, insert };
}

async function untilPhase(phase: string) {
  await vi.waitFor(() => expect(getVoiceState().phase).toBe(phase));
}

function interim() {
  return useVoiceSessionStore.getState().interim;
}

function tap(id: string) {
  pressVoice(id);
  releaseVoice();
}

beforeEach(() => {
  useVoiceSettingsStore.getState().update({ ...DEFAULT_VOICE_SETTINGS, enabled: true, activation: "toggle" });
  useVoiceModelsStore.getState().setModels(MODELS);
  stream.handlers = null;
  stream.start.mockReset().mockResolvedValue(undefined);
  stream.finish.mockReset().mockResolvedValue(undefined);
  stream.discard.mockReset();
  stream.record.mockReset();
});

afterEach(() => {
  cancelVoice();
  while (unregister.length) unregister.pop()!();
});

describe("a streaming runtime", () => {
  it("types each finished sentence while recording and shows the one in progress", async () => {
    const box = target("box");
    tap("box");
    await untilPhase("recording");
    expect(stream.record).not.toHaveBeenCalled();

    stream.handlers!.onInterim("Assign the");
    expect(interim()).toBe("Assign the");
    expect(box.inserted).toEqual([]);

    stream.handlers!.onTurn("Assign the issue to Alice.");
    await vi.waitFor(() => expect(box.inserted).toEqual(["Assign the issue to Alice."]));
    expect(interim()).toBe("");
    expect(getVoiceState().phase).toBe("recording");

    stream.handlers!.onInterim("Then um close");
    stream.handlers!.onTurn("Then um close it.");
    await vi.waitFor(() => expect(box.inserted).toEqual(["Assign the issue to Alice.", "Then close it."]));
  });

  it("tapping again lets the provider end the last sentence, then stops", async () => {
    const box = target("box");
    tap("box");
    await untilPhase("recording");
    let endLast!: () => void;
    stream.finish.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          endLast = () => {
            stream.handlers!.onTurn("Ship it.");
            resolve();
          };
        }),
    );
    stream.handlers!.onInterim("Ship");
    tap("box");
    expect(getVoiceState().phase).toBe("transcribing");
    endLast();
    await untilPhase("idle");
    expect(box.inserted).toEqual(["Ship it."]);
    expect(interim()).toBe("");
  });

  it("stop-and-send submits after the last sentence is typed", async () => {
    const box = target("box");
    tap("box");
    await untilPhase("recording");
    stream.handlers!.onTurn("Looks good.");
    stopVoice(true);
    await untilPhase("idle");
    expect(box.submit).toHaveBeenCalledTimes(1);
  });

  it("Escape drops the sentence in progress but keeps what was typed", async () => {
    const box = target("box");
    tap("box");
    await untilPhase("recording");
    stream.handlers!.onTurn("Keep this.");
    await vi.waitFor(() => expect(box.inserted).toEqual(["Keep this."]));
    stream.handlers!.onInterim("not this");
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
    expect(getVoiceState().phase).toBe("idle");
    expect(stream.discard).toHaveBeenCalled();
    expect(interim()).toBe("");
    stream.handlers!.onTurn("not this.");
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(box.inserted).toEqual(["Keep this."]);
  });

  it("a stream that heard nothing reports no speech", async () => {
    target("box");
    tap("box");
    await untilPhase("recording");
    useVoiceSessionStore.setState({ meter: { levels: [], elapsedMs: 3000 } });
    tap("box");
    await untilPhase("error");
    expect(getVoiceState().error).toBe("no-speech");
  });

  it("a denied microphone is reported as such", async () => {
    target("box");
    stream.start.mockRejectedValue(new VoiceRecorderError("permission-denied"));
    tap("box");
    await untilPhase("error");
    expect(getVoiceState().error).toBe("permission-denied");
  });

  it("a stream that cannot open or breaks can be retried", async () => {
    target("box");
    stream.start.mockRejectedValueOnce(new Error("no token"));
    tap("box");
    await untilPhase("error");
    expect(getVoiceState().error).toBe("stream-failed");

    retryVoice();
    await untilPhase("recording");
    stream.handlers!.onError();
    expect(getVoiceState().phase).toBe("error");
    expect(getVoiceState().error).toBe("stream-failed");
    expect(stream.discard).toHaveBeenCalled();
  });

  it("a composer that goes away ends the stream", async () => {
    target("box");
    tap("box");
    await untilPhase("recording");
    unregister.pop()!();
    expect(getVoiceState().phase).toBe("idle");
    expect(stream.discard).toHaveBeenCalled();
  });

  it("a hold released while only the connection is coming up stops once it is up", async () => {
    useVoiceSettingsStore.getState().update({ activation: "hold" });
    const box = target("box");
    let connected!: () => void;
    stream.start.mockImplementation(() => {
      stream.handlers!.onMicrophone?.();
      return new Promise<void>((resolve) => (connected = resolve));
    });
    stream.finish.mockImplementation(async () => stream.handlers!.onTurn("Too short to say much."));
    pressVoice("box");
    releaseVoice();
    expect(getVoiceState().phase).toBe("requesting");
    connected();
    await untilPhase("idle");
    expect(stream.finish).toHaveBeenCalledTimes(1);
    expect(box.inserted).toEqual(["Too short to say much."]);
  });

  it("a hold released at the permission prompt keeps recording as a tap", async () => {
    useVoiceSettingsStore.getState().update({ activation: "hold" });
    target("box");
    let granted!: () => void;
    stream.start.mockImplementation(() => new Promise<void>((resolve) => (granted = resolve)));
    pressVoice("box");
    releaseVoice();
    granted();
    await untilPhase("recording");
    expect(getVoiceState().mode).toBe("strip");
    expect(stream.finish).not.toHaveBeenCalled();
  });

  it("stop-and-send waits for a sentence the editor was not ready for", async () => {
    let ready = false;
    const box = target("box", {
      insert: vi.fn((text: string) => {
        if (!ready) return false;
        box.inserted.push(text);
        return true;
      }),
    });
    tap("box");
    await untilPhase("recording");
    stream.handlers!.onTurn("First.");
    stream.handlers!.onTurn("Second.");
    stopVoice(true);
    await new Promise((resolve) => setTimeout(resolve, 150));
    expect(box.submit).not.toHaveBeenCalled();
    ready = true;
    await untilPhase("idle");
    expect(box.inserted).toEqual(["First.", "Second."]);
    expect(box.submit).toHaveBeenCalledTimes(1);
  });

  it("a stream that fails while starting stays failed", async () => {
    target("box");
    let connected!: () => void;
    stream.start.mockImplementation(() => new Promise<void>((resolve) => (connected = resolve)));
    tap("box");
    stream.handlers!.onError();
    expect(getVoiceState().error).toBe("stream-failed");
    connected();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(getVoiceState().phase).toBe("error");
  });

  it("records a clip instead when the chosen model is on this device", async () => {
    const local = { ...MODELS.models[0]!, id: "local", name: "Local", streams: undefined };
    useVoiceModelsStore.getState().setModels({ engineAvailable: true, models: [MODELS.models[0]!, local] });
    useVoiceSettingsStore.getState().update({ modelId: "local" });
    stream.record.mockReturnValue({
      start: () => Promise.resolve(),
      finish: () => Promise.resolve({ blob: new Blob(), durationMs: 0, heardSpeech: false }),
      discard: () => {},
    });
    target("box");
    tap("box");
    await untilPhase("recording");
    expect(stream.record).toHaveBeenCalledTimes(1);
    expect(stream.handlers).toBeNull();
  });
});
