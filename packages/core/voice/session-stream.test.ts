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
    expect(box.inserted).toEqual(["Assign the issue to Alice."]);
    expect(interim()).toBe("");
    expect(getVoiceState().phase).toBe("recording");

    stream.handlers!.onInterim("Then um close");
    stream.handlers!.onTurn("Then um close it.");
    expect(box.inserted).toEqual(["Assign the issue to Alice.", "Then close it."]);
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
    stream.handlers!.onInterim("not this");
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
    expect(getVoiceState().phase).toBe("idle");
    expect(stream.discard).toHaveBeenCalled();
    expect(interim()).toBe("");
    stream.handlers!.onTurn("not this.");
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
});
