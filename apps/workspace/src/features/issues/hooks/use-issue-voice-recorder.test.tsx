import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useIssueVoiceRecorder } from "./use-issue-voice-recorder";

class MockMediaRecorder {
  static isTypeSupported = vi.fn(() => true);

  mimeType: string;
  state: RecordingState = "inactive";
  ondataavailable: ((event: BlobEvent) => void) | null = null;
  onerror: (() => void) | null = null;
  onstop: (() => void) | null = null;

  constructor(_stream: MediaStream, options?: MediaRecorderOptions) {
    this.mimeType = options?.mimeType ?? "audio/webm";
  }

  start() {
    this.state = "recording";
  }

  stop() {
    this.state = "inactive";
    this.ondataavailable?.({ data: new Blob(["audio"], { type: this.mimeType }) } as BlobEvent);
    this.onstop?.();
  }
}

describe("useIssueVoiceRecorder", () => {
  const stopTrack = vi.fn();

  beforeEach(() => {
    stopTrack.mockReset();
    vi.stubGlobal("MediaRecorder", MockMediaRecorder);
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: {
        getUserMedia: vi.fn().mockResolvedValue({
          getTracks: () => [{ stop: stopTrack }],
        }),
      },
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("records a blob and stops media tracks", async () => {
    const { result } = renderHook(() => useIssueVoiceRecorder());

    await act(async () => {
      await result.current.start();
    });

    expect(result.current.status).toBe("recording");

    act(() => {
      result.current.stop();
    });

    await waitFor(() => expect(result.current.recording).not.toBeNull());
    expect(result.current.status).toBe("idle");
    expect(result.current.recording?.file.type).toBe("audio/webm");
    expect(stopTrack).toHaveBeenCalled();
  });

  it("reports unsupported browsers", () => {
    vi.stubGlobal("MediaRecorder", undefined);
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: undefined,
    });

    const { result } = renderHook(() => useIssueVoiceRecorder());
    expect(result.current.status).toBe("unsupported");
    expect(result.current.supported).toBe(false);
  });
});
