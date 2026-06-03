export type PlayPCMStreamOptions = {
  sampleRate: number;
  bitDepth: 16;
  channels: 1;
  signal?: AbortSignal;
  onChunkPlayed?: (bytes: number) => void;
  onEnded?: () => void;
  onError?: (err: unknown) => void;
};

export type PCMStreamPlayback = {
  stop: () => void;
  whenEnded: Promise<void>;
};

type BrowserAudioContext = AudioContext & {
  close?: () => Promise<void>;
};

type AudioContextConstructor = new () => BrowserAudioContext;

type WebkitAudioWindow = Window &
  typeof globalThis & {
    webkitAudioContext?: AudioContextConstructor;
  };

const BYTES_PER_SAMPLE = 2;

export function playPCMStream(
  stream: ReadableStream<Uint8Array>,
  opts: PlayPCMStreamOptions,
): PCMStreamPlayback {
  if (opts.bitDepth !== 16 || opts.channels !== 1) {
    throw new Error("playPCMStream only supports mono 16-bit PCM streams.");
  }

  const AudioContextCtor =
    globalThis.AudioContext ??
    (globalThis as WebkitAudioWindow).webkitAudioContext;

  if (!AudioContextCtor) {
    throw new Error("Web Audio API is not available in this browser.");
  }

  const audioContext = new AudioContextCtor();
  const reader = stream.getReader();
  const sources = new Set<AudioBufferSourceNode>();
  let stopped = false;
  let streamFinished = false;
  let nextStartTime = audioContext.currentTime;
  let trailingBytes = new Uint8Array(0);
  let resolveWhenEnded!: () => void;
  let rejectWhenEnded!: (err: unknown) => void;

  const whenEnded = new Promise<void>((resolve, reject) => {
    resolveWhenEnded = resolve;
    rejectWhenEnded = reject;
  });

  const finishIfDone = () => {
    if (streamFinished && sources.size === 0 && !stopped) {
      opts.onEnded?.();
      void audioContext.close?.();
      resolveWhenEnded();
    }
  };

  const fail = (err: unknown) => {
    if (stopped) {
      return;
    }
    stopped = true;
    opts.onError?.(err);
    void audioContext.close?.();
    rejectWhenEnded(err);
  };

  const stop = () => {
    if (stopped) {
      return;
    }
    stopped = true;
    void reader.cancel();
    for (const source of sources) {
      source.onended = null;
      try {
        source.stop();
      } catch {
        // A source that has already ended may throw on stop().
      }
    }
    sources.clear();
    void audioContext.close?.();
    resolveWhenEnded();
  };

  opts.signal?.addEventListener("abort", stop, { once: true });

  const scheduleChunk = (chunk: Uint8Array) => {
    const playable = appendTrailingBytes(trailingBytes, chunk);
    const playableLength =
      playable.byteLength - (playable.byteLength % BYTES_PER_SAMPLE);

    trailingBytes = playable.slice(playableLength);
    if (playableLength === 0) {
      return;
    }

    const pcm = playable.slice(0, playableLength);
    const samples = pcm.byteLength / BYTES_PER_SAMPLE;
    const buffer = audioContext.createBuffer(1, samples, opts.sampleRate);
    const channel = buffer.getChannelData(0);
    const view = new DataView(pcm.buffer, pcm.byteOffset, pcm.byteLength);

    for (let i = 0; i < samples; i += 1) {
      channel[i] = Math.max(-1, view.getInt16(i * BYTES_PER_SAMPLE, true) / 32768);
    }

    const source = audioContext.createBufferSource();
    source.buffer = buffer;
    source.connect(audioContext.destination);
    sources.add(source);

    source.onended = () => {
      sources.delete(source);
      finishIfDone();
    };

    // Web Audio can schedule decoded PCM chunks directly, so playback starts
    // once the first chunk arrives without buffering the whole response.
    const startAt = Math.max(audioContext.currentTime, nextStartTime);
    source.start(startAt);
    nextStartTime = startAt + buffer.duration;
    opts.onChunkPlayed?.(pcm.byteLength);
  };

  void (async () => {
    try {
      while (!stopped) {
        const { done, value } = await reader.read();
        if (done) {
          streamFinished = true;
          finishIfDone();
          return;
        }
        if (value) {
          scheduleChunk(value);
        }
      }
    } catch (err) {
      fail(err);
    }
  })();

  return { stop, whenEnded };
}

function appendTrailingBytes(
  trailingBytes: Uint8Array,
  chunk: Uint8Array,
): Uint8Array {
  if (trailingBytes.byteLength === 0) {
    return chunk;
  }

  const merged = new Uint8Array(trailingBytes.byteLength + chunk.byteLength);
  merged.set(trailingBytes);
  merged.set(chunk, trailingBytes.byteLength);
  return merged;
}
