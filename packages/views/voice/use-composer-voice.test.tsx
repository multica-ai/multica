import { act, cleanup, render, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cancelVoice,
  connectVoiceRuntime,
  getVoiceState,
  pressVoice,
  releaseVoice,
  stopVoice,
  useVoiceModelsStore,
  type VoiceModels,
} from "@multica/core/voice";
import { createShortcutChord, useShortcutStore } from "@multica/core/shortcuts";
import { useComposerVoice, type ComposerVoiceOptions } from "./use-composer-voice";

const INSTALLED: VoiceModels = {
  engineAvailable: true,
  models: [
    {
      id: "sensevoice-small",
      name: "SenseVoice Small",
      author: "FunAudioLLM",
      licenseUrl: "https://example.com/license",
      languages: ["zh", "en"],
      languageHint: true,
      sizeBytes: 1,
      status: "ready",
      receivedBytes: 1,
    },
  ],
};

const engine = vi.hoisted(() => ({ transcribe: vi.fn(), started: 0 }));

vi.mock("./recorder", () => ({ voiceInputSupported: () => true }));
vi.mock("./engine", async () => {
  const { useVoiceModelsStore } = await import("@multica/core/voice");
  return { useVoiceModels: () => useVoiceModelsStore((s) => s.models) };
});

beforeAll(() => {
  connectVoiceRuntime(() => ({
    record: () => ({
      start: () => {
        engine.started++;
        return Promise.resolve();
      },
      finish: () => Promise.resolve({ blob: new Blob(["clip"]), heardSpeech: true, durationMs: 1500 }),
      discard: () => {},
    }),
    prepare: () => {},
    transcribe: (...args) => engine.transcribe(...args),
    watchModels: () => {},
  }));
});

beforeEach(() => {
  engine.started = 0;
  engine.transcribe.mockReset();
  act(() => useVoiceModelsStore.getState().setModels(INSTALLED));
  useShortcutStore.getState().setShortcut("voiceInput", createShortcutChord("Space", { alt: true }));
});

afterEach(() => {
  useShortcutStore.getState().resetShortcut("voiceInput");
  act(() => cancelVoice());
  cleanup();
});

function shortcut() {
  window.dispatchEvent(new KeyboardEvent("keydown", { key: " ", altKey: true, bubbles: true }));
  window.dispatchEvent(new KeyboardEvent("keyup", { key: " ", altKey: true, bubbles: true }));
}

describe("useComposerVoice", () => {
  // One ChatInput serves every conversation: what changes is its scope.
  it.each([
    ["switching to another session", "session-a:agent-1", "session-b:agent-1"],
    ["starting a new chat", "session-a:agent-1", "__draft_new__:agent-1"],
    ["switching agent in a new chat", "__draft_new__:agent-1", "__draft_new__:agent-2"],
  ])("drops the transcript and its send after %s", async (_name, from, to) => {
    let resolve!: (value: unknown) => void;
    engine.transcribe.mockReturnValue(new Promise((done) => (resolve = done)));
    const before = { insert: vi.fn(() => true), submit: vi.fn() };
    const after = { insert: vi.fn(() => true), submit: vi.fn() };
    const hook = renderHook((options: ComposerVoiceOptions) => useComposerVoice(options), {
      initialProps: { ...before, scope: from },
    });
    await act(async () => {
      pressVoice(hook.result.current.id);
      releaseVoice();
    });
    await act(async () => stopVoice(true));
    expect(getVoiceState().phase).toBe("transcribing");

    hook.rerender({ ...after, scope: to });
    expect(getVoiceState().phase).toBe("idle");
    await act(async () => resolve({ ok: true, text: "meant for the previous conversation" }));

    expect(getVoiceState().phase).toBe("idle");
    for (const callbacks of [before, after]) {
      expect(callbacks.insert).not.toHaveBeenCalled();
      expect(callbacks.submit).not.toHaveBeenCalled();
    }
  });

  it("keeps dictating when only the callbacks change", async () => {
    engine.transcribe.mockResolvedValue({ ok: true, text: "still here" });
    const insert = vi.fn(() => true);
    const hook = renderHook((options: ComposerVoiceOptions) => useComposerVoice(options), {
      initialProps: { insert: () => true, scope: "session-a:agent-1" } as ComposerVoiceOptions,
    });
    await act(async () => {
      pressVoice(hook.result.current.id);
      releaseVoice();
    });
    hook.rerender({ insert, submit: undefined, scope: "session-a:agent-1" });
    await act(async () => stopVoice(false));
    await waitFor(() => expect(insert).toHaveBeenCalledWith("still here"));
  });

  it("does not listen while hidden, and hiding cancels a recording", async () => {
    const hook = renderHook((options: ComposerVoiceOptions) => useComposerVoice(options), {
      initialProps: { insert: () => true, hidden: true },
    });
    await act(async () => shortcut());
    expect(getVoiceState().phase).toBe("idle");
    expect(engine.started).toBe(0);

    hook.rerender({ insert: () => true, hidden: false });
    await act(async () => shortcut());
    expect(getVoiceState().phase).toBe("recording");

    hook.rerender({ insert: () => true, hidden: true });
    expect(getVoiceState().phase).toBe("idle");
  });

  it("points the shortcut at a composer that is on screen", async () => {
    const offScreen = vi.fn(() => true);
    const onScreen = vi.fn(() => true);
    function Composer({ insert, shown }: { insert: () => boolean; shown: boolean }) {
      const voice = useComposerVoice({ insert });
      return (
        <div
          ref={(element) => {
            if (element) element.checkVisibility = () => shown;
            voice.rootRef(element);
          }}
          onFocusCapture={voice.onFocusCapture}
        >
          <input />
        </div>
      );
    }
    const view = render(
      <>
        <Composer insert={onScreen} shown />
        <Composer insert={offScreen} shown={false} />
      </>,
    );
    act(() => view.container.querySelectorAll("input")[1]!.focus());
    await act(async () => shortcut());
    expect(getVoiceState().phase).toBe("recording");

    engine.transcribe.mockResolvedValue({ ok: true, text: "for the visible one" });
    await act(async () => stopVoice(false));
    await waitFor(() => expect(onScreen).toHaveBeenCalledWith("for the visible one"));
    expect(offScreen).not.toHaveBeenCalled();
  });
});
