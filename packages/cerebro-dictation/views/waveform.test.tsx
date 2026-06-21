import { render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Waveform } from "./waveform";

// Minimal Web Audio + MediaStream doubles. jsdom has neither AudioContext nor a
// real canvas backend; the meter's draw loop tolerates a null 2d context, so we
// only need to observe which stream the AnalyserNode is wired to.
function makeAnalyser() {
  return {
    fftSize: 0,
    smoothingTimeConstant: 0,
    frequencyBinCount: 128,
    getByteFrequencyData: vi.fn(),
  };
}

const createMediaStreamSource = vi.fn(() => ({ connect: vi.fn() }));
const audioCtxClose = vi.fn();

class FakeAudioContext {
  createMediaStreamSource = createMediaStreamSource;
  createAnalyser = makeAnalyser;
  resume = vi.fn();
  close = audioCtxClose;
}

function makeStream(label: string) {
  const track = { stop: vi.fn(), label };
  const stream = {
    label,
    getTracks: () => [track],
    clone: vi.fn(),
  } as unknown as MediaStream & { clone: ReturnType<typeof vi.fn> };
  return { stream, track };
}

describe("Waveform", () => {
  beforeEach(() => {
    createMediaStreamSource.mockClear();
    audioCtxClose.mockClear();
    (globalThis as unknown as { AudioContext: unknown }).AudioContext =
      FakeAudioContext;
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("taps a CLONED stream so the recorder keeps the original uncontended (iOS, FIR-1637)", () => {
    // On iOS/WebKit a Web Audio context reading the recording stream starves
    // MediaRecorder — the meter animates but the clip is empty. The meter must
    // therefore analyse a clone, never the stream the recorder is reading.
    const { stream: original } = makeStream("original");
    const { stream: clone, track: cloneTrack } = makeStream("clone");
    (original.clone as ReturnType<typeof vi.fn>).mockReturnValue(clone);

    const { unmount } = render(<Waveform stream={original} />);

    expect(original.clone).toHaveBeenCalledTimes(1);
    expect(createMediaStreamSource).toHaveBeenCalledTimes(1);
    // The AnalyserNode is wired to the clone, not the recorder's stream.
    expect(createMediaStreamSource).toHaveBeenCalledWith(clone);

    unmount();
    // The clone is stopped on teardown; the original (owned by the recorder) is
    // never touched here.
    expect(cloneTrack.stop).toHaveBeenCalledTimes(1);
    for (const t of original.getTracks()) {
      expect(t.stop).not.toHaveBeenCalled();
    }
  });

  it("falls back to the original stream when clone() is unavailable", () => {
    const { stream: original } = makeStream("original");
    // Simulate an environment without MediaStream.clone.
    (original as unknown as { clone: unknown }).clone = undefined;

    render(<Waveform stream={original} />);

    expect(createMediaStreamSource).toHaveBeenCalledWith(original);
  });
});
