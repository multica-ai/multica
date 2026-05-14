import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MicButton } from "./mic-button";

const mocks = vi.hoisted(() => ({
  useFeatureFlag: vi.fn(() => true),
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
    mocks.useFeatureFlag.mockReturnValue(true);
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
    mocks.useFeatureFlag.mockReturnValue(false);

    render(<MicButton onTranscribed={vi.fn()} />);

    expect(screen.queryByRole("button")).toBeNull();
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
