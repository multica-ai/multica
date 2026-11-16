import { createContext, useContext, useEffect, useMemo, useSyncExternalStore, type ReactNode } from "react";
import { useStoryEnv } from "../env";

/** Same cadence as the desktop recorder (packages/views/voice/recorder.ts). */
export const SAMPLE_MS = 50;
const MAX_SAMPLES = 6000;
export const BANDS = 16;

export type Scenario = "speech" | "silence" | "loud";

/**
 * One shared input every waveform variant reads, so they can be compared on
 * identical audio. Levels use the recorder's formula: RMS of the time-domain
 * signal × 5, clamped to 1.
 */
export interface LevelStream {
  /** Every sample since the start, oldest first. */
  samples: number[];
  /** Latest spectrum, low to high frequency, 0..1. */
  bands: number[];
  startedAt: number;
  /** When the newest sample arrived. */
  lastAt: number;
  /** Animation clock; stops moving while frozen. */
  now: () => number;
  version: number;
}

interface StreamStore {
  stream: LevelStream;
  subscribe: (listener: () => void) => () => void;
  getVersion: () => number;
  setPaused: (paused: boolean) => void;
}

const StreamContext = createContext<StreamStore | null>(null);

function mulberry32(seed: number) {
  let a = seed;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** Phrases with word-level gaps and a longer breath between sentences. */
function speechEnvelope(t: number): number {
  const cycle = t % 6400;
  const speaking = (cycle > 300 && cycle < 2500) || (cycle > 2900 && cycle < 4700);
  if (!speaking) return 0;
  const word = (t % 330) / 330;
  const wordShape = Math.pow(Math.sin(Math.PI * word), 0.6);
  const syllable = 0.55 + 0.45 * Math.sin(t / 61) * Math.sin(t / 173 + 1.3);
  const phrase = 0.75 + 0.25 * Math.sin(t / 900);
  return wordShape * syllable * phrase;
}

function simulatedLevel(scenario: Scenario, t: number, rand: () => number): number {
  const noise = 0.015 + rand() * 0.03;
  if (scenario === "silence") return noise;
  const gain = scenario === "loud" ? 1.25 : 0.72;
  const env = speechEnvelope(t);
  return Math.min(1, noise + env * gain + (env > 0 ? rand() * 0.12 : 0));
}

function simulatedBands(level: number, rand: () => number): number[] {
  return Array.from({ length: BANDS }, (_, i) => {
    // Speech energy sits in the low-mid bands and rolls off above.
    const weight = Math.exp(-Math.pow((i - 3.5) / 4.2, 2)) * 0.85 + 0.15;
    return Math.min(1, level * weight * (0.55 + rand() * 0.7));
  });
}

interface Options {
  scenario: Scenario;
  paused?: boolean;
  frozen?: boolean;
  /** Fill this many ms of history up front, so a frozen frame looks mid-recording. */
  prefillMs?: number;
  seed?: number;
}

function createStore({ scenario, frozen = false, prefillMs = 0, seed = 7 }: Options, useMic: boolean) {
  const rand = mulberry32(seed);
  const listeners = new Set<() => void>();
  const startedAt = performance.now() - prefillMs;
  const samples: number[] = [];
  let bands = Array.from({ length: BANDS }, () => 0);
  for (let t = 0; t < prefillMs; t += SAMPLE_MS) {
    const level = simulatedLevel(scenario, t, rand);
    samples.push(level);
    bands = simulatedBands(level, rand);
  }
  const frozenAt = performance.now();
  let pausedAt: number | null = null;
  const stream: LevelStream = {
    samples,
    bands,
    startedAt,
    lastAt: frozenAt,
    now: () => (frozen ? frozenAt : (pausedAt ?? performance.now())),
    version: 0,
  };
  const emit = () => {
    stream.version += 1;
    listeners.forEach((listener) => listener());
  };
  const push = (level: number, nextBands: number[]) => {
    if (pausedAt !== null) return;
    stream.samples.push(level);
    if (stream.samples.length > MAX_SAMPLES) stream.samples.shift();
    stream.bands = nextBands;
    stream.lastAt = performance.now();
    emit();
  };

  const start = (): (() => void) => {
    if (frozen) return () => {};
    if (useMic) return startMicrophone(push);
    const timer = window.setInterval(() => {
      const level = simulatedLevel(scenario, performance.now() - stream.startedAt, rand);
      push(level, simulatedBands(level, rand));
    }, SAMPLE_MS);
    return () => window.clearInterval(timer);
  };

  const store: StreamStore = {
    stream,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    getVersion: () => stream.version,
    setPaused: (paused) => {
      if (paused === (pausedAt !== null)) return;
      if (paused) {
        pausedAt = performance.now();
        return;
      }
      const resumedAt = performance.now();
      const pause = resumedAt - (pausedAt ?? resumedAt);
      stream.startedAt += pause;
      stream.lastAt += pause;
      pausedAt = null;
    },
  };
  return { store, start };
}

function startMicrophone(push: (level: number, bands: number[]) => void): () => void {
  let cancelled = false;
  let cleanup = () => {};
  void navigator.mediaDevices
    .getUserMedia({ audio: true })
    .then((media) => {
      if (cancelled) {
        media.getTracks().forEach((track) => track.stop());
        return;
      }
      const context = new AudioContext();
      const resume = () => void context.resume();
      window.addEventListener("pointerdown", resume);
      const analyser = context.createAnalyser();
      analyser.fftSize = 512;
      analyser.smoothingTimeConstant = 0.5;
      context.createMediaStreamSource(media).connect(analyser);
      const time = new Uint8Array(analyser.fftSize);
      const freq = new Uint8Array(analyser.frequencyBinCount);
      const timer = window.setInterval(() => {
        analyser.getByteTimeDomainData(time);
        let sum = 0;
        for (const sample of time) {
          const v = (sample - 128) / 128;
          sum += v * v;
        }
        const level = Math.min(1, Math.sqrt(sum / time.length) * 5);
        analyser.getByteFrequencyData(freq);
        const bands = Array.from({ length: BANDS }, (_, i) => {
          // Log-spaced bins from ~170 Hz to ~8 kHz.
          const lo = Math.floor(2 * Math.pow(48, i / BANDS));
          const hi = Math.max(lo + 1, Math.floor(2 * Math.pow(48, (i + 1) / BANDS)));
          let peak = 0;
          for (let b = lo; b < hi; b++) peak = Math.max(peak, freq[b] ?? 0);
          return Math.pow(peak / 255, 1.6);
        });
        push(level, bands);
      }, SAMPLE_MS);
      cleanup = () => {
        window.clearInterval(timer);
        window.removeEventListener("pointerdown", resume);
        media.getTracks().forEach((track) => track.stop());
        void context.close();
      };
    })
    .catch(() => {});
  return () => {
    cancelled = true;
    cleanup();
  };
}

export function LevelStreamProvider({ children, paused = false, ...options }: Options & { children: ReactNode }) {
  const { source } = useStoryEnv();
  const useMic = source === "microphone" && !options.frozen;
  const { scenario, frozen, prefillMs, seed } = options;
  const created = useMemo(
    () => createStore({ scenario, frozen, prefillMs, seed }, useMic),
    [scenario, frozen, prefillMs, seed, useMic],
  );
  useEffect(() => created.start(), [created]);
  useEffect(() => created.store.setPaused(paused), [created, paused]);
  return <StreamContext.Provider value={created.store}>{children}</StreamContext.Provider>;
}

function useStore(): StreamStore {
  const store = useContext(StreamContext);
  if (!store) throw new Error("LevelStreamProvider is missing");
  return store;
}

/** For canvas renderers: read on every animation frame, no re-render. */
export function useLevelStream(): LevelStream {
  return useStore().stream;
}

/** For DOM renderers: re-renders on every new sample, like the shipping meter. */
export function useLevelSamples(): { samples: number[]; elapsedMs: number } {
  const store = useStore();
  useSyncExternalStore(store.subscribe, store.getVersion);
  const { stream } = store;
  return { samples: stream.samples, elapsedMs: stream.lastAt - stream.startedAt };
}
