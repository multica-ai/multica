import { useCallback, useEffect, useRef } from "react";
import type { PomodoroSettings } from "./use-pomodoro-settings";

// Shared AudioContext singleton — created on first user interaction.
let audioCtx: AudioContext | null = null;

function getAudioContext(): AudioContext {
  if (!audioCtx) {
    audioCtx = new (window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext)();
  }
  return audioCtx;
}

/** Generate a white noise buffer of the given length in seconds. */
function createNoiseBuffer(ctx: AudioContext, seconds = 10): AudioBuffer {
  const bufferSize = ctx.sampleRate * seconds;
  const buffer = ctx.createBuffer(1, bufferSize, ctx.sampleRate);
  const output = buffer.getChannelData(0);
  for (let i = 0; i < bufferSize; i++) {
    output[i] = Math.random() * 2 - 1;
  }
  return buffer;
}

/** Play a sine-wave tone at the given frequency and duration. */
function playTone(
  ctx: AudioContext,
  freq: number,
  startTime: number,
  durationMs: number,
  volume: number,
): void {
  const osc = ctx.createOscillator();
  const gain = ctx.createGain();
  osc.connect(gain);
  gain.connect(ctx.destination);
  osc.frequency.value = freq;
  osc.type = "sine";
  gain.gain.setValueAtTime(0, startTime);
  gain.gain.linearRampToValueAtTime(volume * 0.5, startTime + 0.01);
  gain.gain.linearRampToValueAtTime(0, startTime + durationMs / 1000 - 0.01);
  osc.start(startTime);
  osc.stop(startTime + durationMs / 1000);
}

export function useSoundSystem(settings: PomodoroSettings) {
  const whiteNoiseNodeRef = useRef<AudioBufferSourceNode | null>(null);
  const whiteNoiseGainRef = useRef<GainNode | null>(null);

  /** Resume the AudioContext (required after a user gesture on some browsers). */
  const ensureResumed = useCallback(async (): Promise<AudioContext | null> => {
    try {
      const ctx = getAudioContext();
      if (ctx.state === "suspended") await ctx.resume();
      return ctx;
    } catch {
      return null;
    }
  }, []);

  /** Three ascending tones (C5-E5-G5) to signal work phase complete. */
  const playWorkComplete = useCallback(async () => {
    if (!settings.sound_enabled) return;
    const ctx = await ensureResumed();
    if (!ctx) return;
    const now = ctx.currentTime;
    playTone(ctx, 523.25, now, 200, settings.volume);         // C5
    playTone(ctx, 659.25, now + 0.22, 200, settings.volume);  // E5
    playTone(ctx, 783.99, now + 0.44, 350, settings.volume);  // G5
  }, [settings.sound_enabled, settings.volume, ensureResumed]);

  /** Two descending tones (A5-E5) to signal break complete. */
  const playBreakComplete = useCallback(async () => {
    if (!settings.sound_enabled) return;
    const ctx = await ensureResumed();
    if (!ctx) return;
    const now = ctx.currentTime;
    playTone(ctx, 880, now, 250, settings.volume * 0.7);           // A5
    playTone(ctx, 659.25, now + 0.27, 350, settings.volume * 0.7); // E5
  }, [settings.sound_enabled, settings.volume, ensureResumed]);

  /** Short click tone to confirm timer start. */
  const playStartTick = useCallback(async () => {
    if (!settings.sound_enabled) return;
    const ctx = await ensureResumed();
    if (!ctx) return;
    playTone(ctx, 2000, ctx.currentTime, 60, settings.volume * 0.4);
  }, [settings.sound_enabled, settings.volume, ensureResumed]);

  /** Start looping white noise. Stops any previously playing noise first. */
  const startWhiteNoise = useCallback(
    async (type: PomodoroSettings["white_noise"]) => {
      if (type === "none") return;
      const ctx = await ensureResumed();
      if (!ctx) return;

      // Stop existing noise before starting a new one.
      if (whiteNoiseNodeRef.current) {
        try {
          whiteNoiseNodeRef.current.stop();
        } catch {
          // ignore errors from already-stopped nodes
        }
        whiteNoiseNodeRef.current = null;
      }

      const buffer = createNoiseBuffer(ctx, 10);
      const source = ctx.createBufferSource();
      source.buffer = buffer;
      source.loop = true;

      const gainNode = ctx.createGain();
      gainNode.gain.value = settings.volume * 0.15;

      // Each noise type applies a different filter to shape the spectrum.
      const filter = ctx.createBiquadFilter();
      if (type === "brown") {
        // Heavy low-pass for deep warmth.
        filter.type = "lowpass";
        filter.frequency.value = 200;
      } else if (type === "rain") {
        // Gentle low-pass for rain texture.
        filter.type = "lowpass";
        filter.frequency.value = 400;
      } else if (type === "cafe") {
        // Band-pass centred around speech frequencies for café ambience.
        filter.type = "bandpass";
        filter.frequency.value = 600;
        filter.Q.value = 0.5;
      } else {
        // Pink noise: slight high-shelf cut to soften the highs.
        filter.type = "highshelf";
        filter.frequency.value = 1000;
        filter.gain.value = -6;
      }

      source.connect(filter);
      filter.connect(gainNode);
      gainNode.connect(ctx.destination);
      source.start();

      whiteNoiseNodeRef.current = source;
      whiteNoiseGainRef.current = gainNode;
    },
    [settings.volume, ensureResumed],
  );

  const stopWhiteNoise = useCallback(() => {
    if (whiteNoiseNodeRef.current) {
      try {
        whiteNoiseNodeRef.current.stop();
      } catch {
        // ignore errors from already-stopped nodes
      }
      whiteNoiseNodeRef.current = null;
      whiteNoiseGainRef.current = null;
    }
  }, []);

  // Clean up noise on unmount.
  useEffect(() => {
    return () => {
      stopWhiteNoise();
    };
  }, [stopWhiteNoise]);

  const isWhiteNoisePlaying = whiteNoiseNodeRef.current !== null;

  return {
    playWorkComplete,
    playBreakComplete,
    playStartTick,
    startWhiteNoise,
    stopWhiteNoise,
    isWhiteNoisePlaying,
  };
}
