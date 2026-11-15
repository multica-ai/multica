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
import { VoiceHud, VoiceStrip } from "./voice-ui";

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

function Composer({ session }: { session: VoiceState }) {
  const v = voice(session);
  return (
    <div className="relative">
      <VoiceHud voice={v} />
      <VoiceStrip voice={v} />
    </div>
  );
}

describe("a stream's sentence in progress", () => {
  it("floats faded above the composer while the strip keeps its waveform", async () => {
    await recording();
    const { container, rerender } = renderWithI18n(<Composer session={getVoiceState()} />);
    expect(container.querySelector("[data-voice-interim]")).toBeNull();

    act(() => handlers!.onInterim("Assign the issue"));
    rerender(<Composer session={getVoiceState()} />);
    const panel = container.querySelector("[data-voice-interim]");
    expect(panel).toHaveTextContent("Assign the issue");
    expect(panel).toHaveClass("w-full", "text-faint-foreground");
    expect(panel?.parentElement).toHaveClass("absolute", "inset-x-0", "bottom-full");
    expect(container.querySelector("canvas")).not.toBeNull();

    act(() => handlers!.onTurn("Assign the issue to Alice."));
    expect(container.querySelector("[data-voice-interim]")).toBeNull();
  });

  it("sits under a tall composer's bottom row and sticks to the bottom of the scroll area", async () => {
    await recording();
    act(() => handlers!.onInterim("Assign the issue"));
    const { container } = renderWithI18n(<VoiceHud voice={voice(getVoiceState())} placement="below" stickAbove={80} />);
    const stack = container.querySelector("[data-voice-interim]")?.parentElement;
    expect(stack).toHaveClass("sticky");
    // Above whatever else is pinned to the bottom (the sticky comment box), not under it.
    expect(stack).toHaveStyle({ bottom: "calc(80px + 0.75rem)" });
    expect(stack).not.toHaveClass("absolute");
    expect(stack).not.toHaveClass("bottom-full");
  });

  it("stays in view while the stream finishes", async () => {
    await recording();
    act(() => handlers!.onInterim("Ship it"));
    act(() => toggleVoice("box"));
    expect(getVoiceState().phase).toBe("transcribing");
    renderWithI18n(<Composer session={getVoiceState()} />);
    expect(screen.getByText("Ship it")).toBeInTheDocument();
    await act(async () => endStream());
  });
});
