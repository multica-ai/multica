import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useDictation } from "./use-dictation";

interface FakeRecorder {
  state: "inactive" | "recording" | "paused";
  mimeType: string;
  start: () => void;
  stop: () => void;
  addEventListener: (event: string, fn: (e: unknown) => void) => void;
  emit: (event: string, payload?: unknown) => void;
}

function createFakeRecorder(): FakeRecorder {
  const listeners: Record<string, ((e: unknown) => void)[]> = {};
  const recorder: FakeRecorder = {
    state: "inactive",
    mimeType: "audio/webm",
    start() {
      recorder.state = "recording";
    },
    stop() {
      recorder.state = "inactive";
      // mirrors the browser: dataavailable fires before stop on stop().
      const chunk = new Blob(["fake-audio"], { type: "audio/webm" });
      recorder.emit("dataavailable", { data: chunk });
      recorder.emit("stop");
    },
    addEventListener(event, fn) {
      (listeners[event] ??= []).push(fn);
    },
    emit(event, payload) {
      for (const fn of listeners[event] ?? []) {
        fn(payload as unknown);
      }
    },
  };
  return recorder;
}

const originalMediaRecorder = (globalThis as { MediaRecorder?: unknown })
  .MediaRecorder;
const originalMediaDevices = (navigator as Navigator & {
  mediaDevices?: MediaDevices;
}).mediaDevices;

function installFakeMediaRecorder(recorder: FakeRecorder) {
  // Must be a regular function (not an arrow) so `new MediaRecorder(...)`
  // can call it as a constructor. Arrow functions have no [[Construct]].
  function MockRecorder() {
    return recorder;
  }
  (
    MockRecorder as unknown as { isTypeSupported: (m: string) => boolean }
  ).isTypeSupported = () => true;
  (globalThis as { MediaRecorder?: unknown }).MediaRecorder = MockRecorder;
}

function installGetUserMedia(impl: () => Promise<MediaStream>) {
  Object.defineProperty(navigator, "mediaDevices", {
    configurable: true,
    value: {
      getUserMedia: impl,
    },
  });
}

function fakeStream(): MediaStream {
  const track = { stop: vi.fn() } as unknown as MediaStreamTrack;
  return {
    getTracks: () => [track],
  } as unknown as MediaStream;
}

afterEach(() => {
  if (originalMediaRecorder === undefined) {
    delete (globalThis as { MediaRecorder?: unknown }).MediaRecorder;
  } else {
    (globalThis as { MediaRecorder?: unknown }).MediaRecorder =
      originalMediaRecorder;
  }
  if (originalMediaDevices === undefined) {
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: undefined,
    });
  } else {
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: originalMediaDevices,
    });
  }
  vi.restoreAllMocks();
});

describe("useDictation", () => {
  it("reports unsupported when MediaRecorder is missing", async () => {
    delete (globalThis as { MediaRecorder?: unknown }).MediaRecorder;
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: undefined,
    });

    const { result } = renderHook(() =>
      useDictation({ transcribe: vi.fn() }),
    );

    expect(result.current.isSupported).toBe(false);

    await act(async () => {
      await result.current.start();
    });

    expect(result.current.status).toBe("error");
    expect(result.current.error?.kind).toBe("unsupported");
  });

  it("transitions through recording -> transcribing -> idle on the happy path", async () => {
    const recorder = createFakeRecorder();
    installFakeMediaRecorder(recorder);
    installGetUserMedia(() => Promise.resolve(fakeStream()));
    const transcribe = vi.fn(async () => "hej verden");
    const onTranscribed = vi.fn();

    const { result } = renderHook(() =>
      useDictation({ transcribe, onTranscribed }),
    );

    expect(result.current.isSupported).toBe(true);

    await act(async () => {
      await result.current.start();
    });
    expect(result.current.status).toBe("recording");

    await act(async () => {
      await result.current.stop();
    });

    await waitFor(() => {
      expect(result.current.status).toBe("idle");
    });
    expect(result.current.lastTranscript).toBe("hej verden");
    expect(onTranscribed).toHaveBeenCalledWith("hej verden");
    expect(transcribe).toHaveBeenCalledTimes(1);
  });

  it("surfaces a permission-denied error when getUserMedia rejects with NotAllowedError", async () => {
    installFakeMediaRecorder(createFakeRecorder());
    installGetUserMedia(() =>
      Promise.reject(
        new DOMException("denied", "NotAllowedError"),
      ),
    );

    const { result } = renderHook(() =>
      useDictation({ transcribe: vi.fn() }),
    );

    await act(async () => {
      await result.current.start();
    });

    expect(result.current.status).toBe("error");
    expect(result.current.error?.kind).toBe("permission-denied");
  });

  it("reports a transcription-failed error when the transcriber throws", async () => {
    const recorder = createFakeRecorder();
    installFakeMediaRecorder(recorder);
    installGetUserMedia(() => Promise.resolve(fakeStream()));
    const transcribe = vi.fn(async () => {
      throw new Error("boom");
    });

    const { result } = renderHook(() => useDictation({ transcribe }));

    await act(async () => {
      await result.current.start();
    });
    await act(async () => {
      await result.current.stop();
    });

    await waitFor(() => {
      expect(result.current.status).toBe("error");
    });
    expect(result.current.error?.kind).toBe("transcription-failed");
    expect(result.current.error?.message).toBe("boom");
  });
});
