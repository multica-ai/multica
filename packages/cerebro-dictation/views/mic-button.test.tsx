import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MicButton } from "./mic-button";

const mocks = vi.hoisted(() => ({
  useFeatureFlag: vi.fn((key: string) =>
    key === "cerebro_voice_dictation_enabled" ||
    key === "cerebro_voice_output_enabled"
  ),
  useDictation: vi.fn(),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: mocks.useFeatureFlag,
}));

vi.mock("../use-dictation", () => ({
  useDictation: mocks.useDictation,
}));

describe("MicButton", () => {
  beforeEach(() => {
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

  it("starts on pointer down and stops on pointer up", () => {
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
    });

    render(<MicButton onTranscribed={vi.fn()} />);

    const button = screen.getByRole("button", { name: "Start dictation" });
    fireEvent.pointerDown(button);
    fireEvent.pointerUp(button);
    fireEvent.click(button);

    expect(start).toHaveBeenCalledTimes(1);
    expect(stop).toHaveBeenCalledTimes(1);
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
    });

    render(<MicButton onTranscribed={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: "Start dictation" }));

    expect(start).toHaveBeenCalledTimes(1);
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
    });

    render(<MicButton onTranscribed={vi.fn()} />);

    expect(screen.getByRole("button")).toBeDisabled();
  });
});
