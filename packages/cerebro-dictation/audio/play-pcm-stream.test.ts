import { afterEach, describe, expect, it, vi } from "vitest";
import { playPCMStream } from "./play-pcm-stream";

const originalAudioContext = globalThis.AudioContext;

type MockSource = {
  buffer: AudioBuffer | null;
  connect: ReturnType<typeof vi.fn>;
  onended: (() => void) | null;
  start: ReturnType<typeof vi.fn>;
  stop: ReturnType<typeof vi.fn>;
};

class MockAudioBuffer {
  duration: number;
  private readonly data: Float32Array;

  constructor(length: number, sampleRate: number) {
    this.duration = length / sampleRate;
    this.data = new Float32Array(length);
  }

  getChannelData() {
    return this.data;
  }
}

class MockAudioContext {
  currentTime = 0;
  destination = {};
  sources: MockSource[] = [];
  close = vi.fn(async () => undefined);

  createBuffer(_channels: number, length: number, sampleRate: number) {
    return new MockAudioBuffer(length, sampleRate) as unknown as AudioBuffer;
  }

  createBufferSource() {
    const source: MockSource = {
      buffer: null,
      connect: vi.fn(),
      onended: null,
      start: vi.fn(() => {
        queueMicrotask(() => source.onended?.());
      }),
      stop: vi.fn(),
    };
    this.sources.push(source);
    return source as unknown as AudioBufferSourceNode;
  }
}

function installAudioContext(instances: MockAudioContext[]) {
  function MockAudioContextConstructor() {
    const instance = new MockAudioContext();
    instances.push(instance);
    return instance;
  }

  Object.defineProperty(globalThis, "AudioContext", {
    configurable: true,
    value: vi.fn(MockAudioContextConstructor),
  });
}

function pcmChunk(...samples: number[]): Uint8Array {
  const bytes = new Uint8Array(samples.length * 2);
  const view = new DataView(bytes.buffer);
  samples.forEach((sample, index) => {
    view.setInt16(index * 2, sample, true);
  });
  return bytes;
}

afterEach(() => {
  Object.defineProperty(globalThis, "AudioContext", {
    configurable: true,
    value: originalAudioContext,
  });
  vi.restoreAllMocks();
});

describe("playPCMStream", () => {
  it("schedules each PCM chunk and resolves when the stream ends", async () => {
    const contexts: MockAudioContext[] = [];
    installAudioContext(contexts);
    const onChunkPlayed = vi.fn();
    const onEnded = vi.fn();
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(pcmChunk(0, 1200));
        controller.enqueue(pcmChunk(-1200, 32767));
        controller.close();
      },
    });

    const playback = playPCMStream(stream, {
      sampleRate: 24_000,
      bitDepth: 16,
      channels: 1,
      onChunkPlayed,
      onEnded,
    });

    await playback.whenEnded;

    expect(onChunkPlayed).toHaveBeenNthCalledWith(1, 4);
    expect(onChunkPlayed).toHaveBeenNthCalledWith(2, 4);
    expect(onEnded).toHaveBeenCalledTimes(1);
    expect(contexts[0]?.sources).toHaveLength(2);
    expect(contexts[0]?.sources[0]?.start).toHaveBeenCalledWith(0);
    expect(contexts[0]?.sources[1]?.start).toHaveBeenCalledWith(2 / 24_000);
  });

  it("cancels the reader and stops scheduled playback", async () => {
    const contexts: MockAudioContext[] = [];
    installAudioContext(contexts);
    let cancelCalled = false;
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(pcmChunk(0, 1000));
      },
      cancel() {
        cancelCalled = true;
      },
    });

    const playback = playPCMStream(stream, {
      sampleRate: 24_000,
      bitDepth: 16,
      channels: 1,
    });

    await Promise.resolve();
    playback.stop();
    await playback.whenEnded;

    expect(cancelCalled).toBe(true);
    expect(contexts[0]?.sources[0]?.stop).toHaveBeenCalledTimes(1);
  });
});
