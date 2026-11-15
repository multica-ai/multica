import { act, screen } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import {
  DEFAULT_VOICE_SETTINGS,
  cancelVoice,
  connectVoiceRuntime,
  getVoiceState,
  registerVoiceTarget,
  toggleVoice,
  useVoiceModelsStore,
  useVoiceSettingsStore,
  type VoiceState,
  type VoiceStreamHandlers,
} from "@multica/core/voice";
import { renderWithI18n } from "../test/i18n";
import type { ComposerVoice } from "./use-composer-voice";
import { VoiceStrip } from "./voice-ui";

let handlers: VoiceStreamHandlers | null = null;
let endStream: () => void = () => {};

beforeAll(() => {
  connectVoiceRuntime(() => ({
    record: () => {
      throw new Error("a streaming runtime records no clips");
    },
    prepare: () => {},
    transcribe: () => Promise.resolve({ ok: false, reason: "engine-unavailable" }),
    watchModels: () => {},
    stream: (next) => {
      handlers = next;
      return {
        start: () => Promise.resolve(),
        finish: () => new Promise<void>((resolve) => (endStream = resolve)),
        discard: () => {},
      };
    },
  }));
});

let unregister = () => {};

beforeEach(() => {
  useVoiceSettingsStore.getState().update({ ...DEFAULT_VOICE_SETTINGS, enabled: true });
  useVoiceModelsStore.getState().setModels({
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
  });
  unregister = registerVoiceTarget("box", { insert: () => true });
});

afterEach(() => {
  act(() => cancelVoice());
  unregister();
});

function voice(session: VoiceState): ComposerVoice {
  return {
    id: "box",
    available: true,
    session,
    busyElsewhere: false,
    showStrip: true,
    showHud: false,
    holding: false,
    showSetup: false,
    canSubmit: false,
    onFocusCapture: () => {},
    rootRef: () => {},
  };
}

async function recording() {
  toggleVoice("box");
  await act(async () => {});
  expect(getVoiceState().phase).toBe("recording");
}

describe("VoiceStrip", () => {
  it("shows a stream's sentence in progress, faded, in place of the waveform", async () => {
    await recording();
    const { container, rerender } = renderWithI18n(<VoiceStrip voice={voice(getVoiceState())} />);
    expect(container.querySelector("canvas")).not.toBeNull();

    act(() => handlers!.onInterim("Assign the issue"));
    rerender(<VoiceStrip voice={voice(getVoiceState())} />);
    expect(screen.getByText("Assign the issue").parentElement).toHaveClass("text-faint-foreground");
    expect(container.querySelector("canvas")).toBeNull();

    act(() => handlers!.onTurn("Assign the issue to Alice."));
    expect(screen.queryByText("Assign the issue")).not.toBeInTheDocument();
    expect(container.querySelector("canvas")).not.toBeNull();
  });

  it("keeps showing the last sentence while the stream finishes", async () => {
    await recording();
    act(() => handlers!.onInterim("Ship it"));
    act(() => toggleVoice("box"));
    expect(getVoiceState().phase).toBe("transcribing");
    renderWithI18n(<VoiceStrip voice={voice(getVoiceState())} />);
    expect(screen.getByText("Ship it")).toBeInTheDocument();
    await act(async () => endStream());
  });
});
