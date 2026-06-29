import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MicButton } from "./mic-button";

const mocks = vi.hoisted(() => ({
  useFeatureFlag: vi.fn((key: string) =>
    key === "cerebro_voice_dictation_enabled" ||
    key === "cerebro_voice_output_enabled"
  ),
  useDictation: vi.fn(),
  toastError: vi.fn(),
  toastMessage: vi.fn(),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: mocks.useFeatureFlag,
}));

vi.mock("../use-dictation", () => ({
  useDictation: mocks.useDictation,
}));

vi.mock("sonner", () => ({
  toast: { error: mocks.toastError, message: mocks.toastMessage },
}));

describe("MicButton", () => {
  beforeEach(() => {
    mocks.toastError.mockClear();
    mocks.toastMessage.mockClear();
    mocks.useFeatureFlag.mockImplementation(
      (key: string) =>
        key === "cerebro_voice_dictation_enabled" ||
        key === "cerebro_voice_output_enabled",
    );
    mocks.useDictation.mockReturnValue({
      status: "idle",
      error: null,
      isSupported: true,
      start: vi.fn(),
      stop: vi.fn(),
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
    });
  });

  it("does not render when the dictation flag is disabled", () => {
    mocks.useFeatureFlag.mockImplementation(
      (key: string) => key === "cerebro_voice_output_enabled",
    );

    render(<MicButton onTranscribed={vi.fn()} />);

    expect(screen.queryByRole("button")).toBeNull();
  });

  it("renders on the dictation flag alone, regardless of Cerebro Voice output", () => {
    // Dictation (speech → text) gates only on cerebro_voice_dictation_enabled.
    // The read-aloud flag (cerebro_voice_output_enabled) is a separate TTS
    // feature and must not hide the mic.
    mocks.useFeatureFlag.mockImplementation(
      (key: string) => key === "cerebro_voice_dictation_enabled",
    );

    render(<MicButton onTranscribed={vi.fn()} />);

    expect(
      screen.getByRole("button", { name: "Start dictation" }),
    ).toBeInTheDocument();
  });

  it("starts recording on the first tap", () => {
    const start = vi.fn();
    const stop = vi.fn();
    mocks.useDictation.mockReturnValue({
      status: "idle",
      error: null,
      isSupported: true,
      start,
      stop,
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
    });

    render(<MicButton onTranscribed={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: "Start dictation" }));

    expect(start).toHaveBeenCalledTimes(1);
    expect(stop).not.toHaveBeenCalled();
  });

  it("pre-warms the engine on the press that starts recording (FIR-1637, Jesper)", () => {
    // The cold engine should boot while the user speaks, not after they stop —
    // so warmup fires on the start tap, before start().
    const start = vi.fn();
    const warmup = vi.fn();
    mocks.useDictation.mockReturnValue({
      status: "idle",
      error: null,
      isSupported: true,
      start,
      stop: vi.fn(),
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
    });

    render(<MicButton onTranscribed={vi.fn()} warmup={warmup} />);
    fireEvent.click(screen.getByRole("button", { name: "Start dictation" }));

    expect(warmup).toHaveBeenCalledTimes(1);
    expect(start).toHaveBeenCalledTimes(1);
  });

  it("does not pre-warm on the tap that stops recording (no wasted boot)", () => {
    const warmup = vi.fn();
    mocks.useDictation.mockReturnValue({
      status: "recording",
      error: null,
      isSupported: true,
      start: vi.fn(),
      stop: vi.fn(),
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
    });

    render(<MicButton onTranscribed={vi.fn()} warmup={warmup} />);
    fireEvent.click(screen.getByRole("button", { name: "Stop dictation" }));

    expect(warmup).not.toHaveBeenCalled();
  });

  it("stops recording on the next tap (tap-to-toggle, not press-and-hold)", () => {
    const start = vi.fn();
    const stop = vi.fn();
    mocks.useDictation.mockReturnValue({
      status: "recording",
      error: null,
      isSupported: true,
      start,
      stop,
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
    });

    render(<MicButton onTranscribed={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: "Stop dictation" }));

    expect(stop).toHaveBeenCalledTimes(1);
    expect(start).not.toHaveBeenCalled();
  });

  it("keeps the stop button enabled while recording", () => {
    // The button must stay clickable mid-recording so the user can stop;
    // `disabled` only applies to the idle/busy states.
    mocks.useDictation.mockReturnValue({
      status: "recording",
      error: null,
      isSupported: true,
      start: vi.fn(),
      stop: vi.fn(),
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
    });

    render(<MicButton onTranscribed={vi.fn()} />);

    expect(
      screen.getByRole("button", { name: "Stop dictation" }),
    ).toBeEnabled();
  });

  it("starts on click for keyboard-style activation", () => {
    const start = vi.fn();
    mocks.useDictation.mockReturnValue({
      status: "idle",
      error: null,
      isSupported: true,
      start,
      stop: vi.fn(),
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
    });

    render(<MicButton onTranscribed={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: "Start dictation" }));

    expect(start).toHaveBeenCalledTimes(1);
  });

  it("shows a processing indicator while transcribing", () => {
    // FIR-1637: the gap between releasing the mic and the transcript landing
    // must be visible — a spinning button plus a "Transcribing…" label — so it
    // never looks like nothing is happening.
    mocks.useDictation.mockReturnValue({
      status: "transcribing",
      error: null,
      isSupported: true,
      start: vi.fn(),
      stop: vi.fn(),
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
    });

    render(<MicButton onTranscribed={vi.fn()} />);

    expect(screen.getByText("Transcribing…")).toBeInTheDocument();
    // The button is inert while the clip is in flight.
    expect(screen.getByRole("button")).toBeDisabled();
  });

  it("reports status changes to onStatusChange so hosts can show Forslag B", () => {
    // FIR-1637: a wrapper renders the destination-side skeleton off this
    // callback, so the mic must surface the transcribing state to the host.
    mocks.useDictation.mockReturnValue({
      status: "transcribing",
      error: null,
      isSupported: true,
      start: vi.fn(),
      stop: vi.fn(),
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
    });
    const onStatusChange = vi.fn();

    render(<MicButton onTranscribed={vi.fn()} onStatusChange={onStatusChange} />);

    expect(onStatusChange).toHaveBeenCalledWith("transcribing");
  });

  it("shows a blue recording timer while recording (FIR-1637, Jesper)", () => {
    mocks.useDictation.mockReturnValue({
      status: "recording",
      error: null,
      isSupported: true,
      start: vi.fn(),
      stop: vi.fn(),
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
    });

    render(<MicButton onTranscribed={vi.fn()} />);

    // Counter starts at 0:00 — proof the mic is capturing, next to the wave.
    expect(screen.getByText("0:00")).toBeInTheDocument();
  });

  it("does not show the timer when idle", () => {
    render(<MicButton onTranscribed={vi.fn()} />);
    expect(screen.queryByText(/^\d+:\d{2}$/)).toBeNull();
  });

  it("toasts a visible error when transcription fails (no more silent failures)", () => {
    // FIR-1637 (Jesper): a failed transcription used to insert nothing with no
    // feedback. Now it surfaces a toast.
    mocks.useDictation.mockReturnValue({
      status: "error",
      error: { kind: "transcription-failed", message: "boom" },
      isSupported: true,
      start: vi.fn(),
      stop: vi.fn(),
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
    });

    render(<MicButton onTranscribed={vi.fn()} />);

    expect(mocks.toastError).toHaveBeenCalledTimes(1);
  });

  it("does not toast when the user cancels (aborted is intentional)", () => {
    mocks.useDictation.mockReturnValue({
      status: "idle",
      error: { kind: "aborted", message: "Transcription was cancelled." },
      isSupported: true,
      start: vi.fn(),
      stop: vi.fn(),
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
    });

    render(<MicButton onTranscribed={vi.fn()} />);

    expect(mocks.toastError).not.toHaveBeenCalled();
  });

  it("disables the button when MediaRecorder is unavailable", () => {
    mocks.useDictation.mockReturnValue({
      status: "idle",
      error: null,
      isSupported: false,
      start: vi.fn(),
      stop: vi.fn(),
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
    });

    render(<MicButton onTranscribed={vi.fn()} />);

    expect(screen.getByRole("button")).toBeDisabled();
  });

  it("renders the round grey floating record button on note surfaces (FIR-1748, Jesper)", () => {
    // On notes / documents / quick notes the mic is a distinct round, filled
    // grey button overlaying the field — the primary action, not a ghost corner
    // icon like the composer inputs use.
    render(<MicButton onTranscribed={vi.fn()} appearance="floating" />);

    const button = screen.getByRole("button", { name: "Start dictation" });
    expect(button.className).toContain("rounded-full");
    expect(button.className).toContain("bg-zinc-500");
  });

  it("turns the floating record button red while recording (FIR-1748)", () => {
    mocks.useDictation.mockReturnValue({
      status: "recording",
      error: null,
      isSupported: true,
      start: vi.fn(),
      stop: vi.fn(),
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
    });

    render(<MicButton onTranscribed={vi.fn()} appearance="floating" />);

    const button = screen.getByRole("button", { name: "Stop dictation" });
    expect(button.className).toContain("bg-red-500");
  });

  it("keeps the default inline mic a ghost icon, not a round button", () => {
    render(<MicButton onTranscribed={vi.fn()} />);

    const button = screen.getByRole("button", { name: "Start dictation" });
    expect(button.className).not.toContain("rounded-full");
  });

  it("shows the custom success label once a transcript lands (Added on notes)", () => {
    // The green cue text is configurable: composer inputs say "Inserted", note /
    // document surfaces say "Added" (FIR-1748, Jesper).
    let landed: ((text: string) => void) | undefined;
    mocks.useDictation.mockImplementation(
      (opts: { onTranscribed: (text: string) => void }) => {
        landed = opts.onTranscribed;
        return {
          status: "idle",
          error: null,
          isSupported: true,
          start: vi.fn(),
          stop: vi.fn(),
          cancel: vi.fn(),
          lastTranscript: null,
          mediaStream: null,
        };
      },
    );

    render(<MicButton onTranscribed={vi.fn()} successLabel="Added" />);
    expect(screen.queryByText("Added")).toBeNull();

    act(() => landed?.("hej med dig"));

    expect(screen.getByText("Added")).toBeInTheDocument();
  });

  it("shows a Re-send button after a kept failure and calls resend on click (FIR-2048)", () => {
    const resend = vi.fn();
    mocks.useDictation.mockReturnValue({
      status: "error",
      error: { kind: "warming-up", message: "warming up" },
      isSupported: true,
      start: vi.fn(),
      stop: vi.fn(),
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
      canResend: true,
      resend,
    });

    render(<MicButton onTranscribed={vi.fn()} />);

    const button = screen.getByRole("button", {
      name: "Re-send recording for transcription",
    });
    fireEvent.click(button);
    expect(resend).toHaveBeenCalledTimes(1);
  });

  it("surfaces a warming-up cold start as a calm message, not a red error (FIR-2048)", () => {
    mocks.useDictation.mockReturnValue({
      status: "error",
      error: { kind: "warming-up", message: "warming up" },
      isSupported: true,
      start: vi.fn(),
      stop: vi.fn(),
      cancel: vi.fn(),
      lastTranscript: null,
      mediaStream: null,
      canResend: true,
      resend: vi.fn(),
    });

    render(<MicButton onTranscribed={vi.fn()} />);

    expect(mocks.toastMessage).toHaveBeenCalledTimes(1);
    expect(mocks.toastError).not.toHaveBeenCalled();
  });
});
