import { describe, expect, it } from "vitest";
import { renderHook, screen } from "@testing-library/react";
import type { VoiceState } from "@multica/core/voice";
import { renderWithI18n } from "../../test/i18n";
import type { ComposerVoice } from "../../voice";
import { ComposerToolbar, useComposerExpanded } from "./composer-frame";

function voice(patch: Partial<ComposerVoice> = {}): ComposerVoice {
  return {
    id: "v",
    available: true,
    session: null,
    busyElsewhere: false,
    showStrip: false,
    showHud: false,
    holding: false,
    showSetup: false,
    canSubmit: true,
    onFocusCapture: () => {},
    rootRef: () => {},
    ...patch,
  };
}

const transcribing: VoiceState = { targetId: "v", phase: "transcribing", mode: "strip", error: null, canRetry: false };

describe("ComposerToolbar", () => {
  it("keeps the recipients beside the transcribing strip instead of covering them", () => {
    renderWithI18n(
      <ComposerToolbar
        voice={voice({ session: transcribing, showStrip: true })}
        hasRecipients
        recipients={<span>Lambda · Will start when sent</span>}
        tools={<button type="button">Attach file</button>}
        send={<button type="button">Send</button>}
      />,
    );
    expect(screen.getByText("Lambda · Will start when sent")).toBeVisible();
    expect(screen.getAllByText("Transcribing…").length).toBeGreaterThan(0);
    expect(screen.queryByRole("button", { name: "Send" })).not.toBeInTheDocument();
  });

  it("shows the tools and send when not dictating", () => {
    renderWithI18n(
      <ComposerToolbar
        voice={voice()}
        hasRecipients={false}
        recipients={null}
        tools={<button type="button">Attach file</button>}
        send={<button type="button">Send</button>}
      />,
    );
    expect(screen.getAllByRole("button").map((b) => b.textContent)).toEqual(["Attach file", "Send"]);
  });
});

describe("useComposerExpanded", () => {
  it("holds its shape while the mic is pressed, then follows again", () => {
    const { result, rerender } = renderHook(({ wanted, holding }) => useComposerExpanded(wanted, voice({ holding })), {
      initialProps: { wanted: false, holding: false },
    });
    expect(result.current).toBe(false);
    rerender({ wanted: true, holding: true });
    expect(result.current).toBe(false);
    rerender({ wanted: true, holding: false });
    expect(result.current).toBe(true);
    rerender({ wanted: false, holding: true });
    expect(result.current).toBe(true);
  });
});
