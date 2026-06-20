"use client";

import { useEffect, useRef } from "react";
import { cn } from "@multica/ui/lib/utils";

export interface WaveformProps {
  /** Live microphone stream to visualise. When null, nothing is drawn. */
  stream: MediaStream | null;
  className?: string;
}

type BrowserAudioContext = AudioContext & { close?: () => Promise<void> };
type AudioContextConstructor = new () => BrowserAudioContext;
type WebkitAudioWindow = Window &
  typeof globalThis & { webkitAudioContext?: AudioContextConstructor };

const BAR_WIDTH = 2;
const BAR_GAP = 2;
const MIN_BAR = 2;

/**
 * Real-time microphone level meter drawn as ChatGPT-style vertical bars.
 *
 * It taps the live recording stream through a Web Audio AnalyserNode and
 * paints the smoothed amplitude on a canvas via requestAnimationFrame. The
 * AudioContext is created lazily per active stream and torn down the moment
 * recording stops, so an idle mic never holds an open context.
 *
 * Purely presentational: it neither owns the stream nor controls recording —
 * the MicButton drives both and hands the stream down only while recording.
 */
export function Waveform({ stream, className }: WaveformProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    if (!stream) return;
    const canvas = canvasRef.current;
    if (!canvas) return;

    const AudioContextCtor =
      globalThis.AudioContext ??
      (globalThis as WebkitAudioWindow).webkitAudioContext;
    if (!AudioContextCtor) return;

    const ctx = new AudioContextCtor();
    const source = ctx.createMediaStreamSource(stream);
    const analyser = ctx.createAnalyser();
    analyser.fftSize = 256;
    analyser.smoothingTimeConstant = 0.7;
    source.connect(analyser);
    const bins = new Uint8Array(analyser.frequencyBinCount);

    let raf = 0;
    let disposed = false;

    const draw = () => {
      if (disposed) return;
      raf = requestAnimationFrame(draw);

      const canvasEl = canvasRef.current;
      if (!canvasEl) return;
      const ctx2d = canvasEl.getContext("2d");
      if (!ctx2d) return;

      // Match the backing store to the displayed size so bars stay crisp on
      // high-DPI screens without blurring.
      const dpr = globalThis.devicePixelRatio || 1;
      const cssWidth = canvasEl.clientWidth || 1;
      const cssHeight = canvasEl.clientHeight || 1;
      const pxWidth = Math.round(cssWidth * dpr);
      const pxHeight = Math.round(cssHeight * dpr);
      if (canvasEl.width !== pxWidth) canvasEl.width = pxWidth;
      if (canvasEl.height !== pxHeight) canvasEl.height = pxHeight;

      analyser.getByteFrequencyData(bins);
      ctx2d.clearRect(0, 0, pxWidth, pxHeight);
      ctx2d.fillStyle = getComputedStyle(canvasEl).color || "currentColor";

      const step = BAR_WIDTH + BAR_GAP;
      const barCount = Math.max(1, Math.floor(cssWidth / step));
      const mid = pxHeight / 2;
      for (let i = 0; i < barCount; i++) {
        // Spread the lower (voice-dominant) half of the spectrum across the
        // bars so speech drives visible motion.
        const binIndex = Math.floor((i / barCount) * (bins.length / 2));
        const level = (bins[binIndex] ?? 0) / 255;
        const barHeight = Math.max(MIN_BAR * dpr, level * pxHeight);
        const x = i * step * dpr;
        ctx2d.fillRect(x, mid - barHeight / 2, BAR_WIDTH * dpr, barHeight);
      }
    };

    // Safari starts the context suspended until a user gesture; the mic tap is
    // that gesture, so resume() is a no-op elsewhere and the safety net here.
    void ctx.resume?.();
    draw();

    return () => {
      disposed = true;
      cancelAnimationFrame(raf);
      try {
        source.disconnect();
      } catch {
        // already disconnected — safe to ignore
      }
      void ctx.close?.();
    };
  }, [stream]);

  return (
    <canvas
      ref={canvasRef}
      aria-hidden
      className={cn("h-5 w-full text-brand", className)}
    />
  );
}
