import { useEffect, useRef, type ComponentType } from "react";
import { cn } from "@multica/ui/lib/utils";
import type { Lang } from "../copy";
import { BANDS, SAMPLE_MS, useLevelSamples, useLevelStream, type LevelStream } from "./stream";

export type Density = "strip" | "hud";

export interface WaveProps {
  density: Density;
  className?: string;
}

const clamp01 = (v: number) => Math.min(1, Math.max(0, v));

/** Recorder level (RMS × 5) to bar height: a noise gate, then a gentle curve so quiet speech still moves. */
function perceptual(v: number): number {
  return Math.pow(clamp01((v - 0.035) / 0.6), 0.8);
}

/** Frame-rate independent easing toward `target`, faster up than down. */
function follow(current: number, target: number, dt: number, attackMs: number, releaseMs: number): number {
  const tau = target > current ? attackMs : releaseMs;
  return current + (target - current) * (1 - Math.exp(-dt / tau));
}

const easeOut = (t: number) => 1 - Math.pow(1 - t, 3);

/** Each bar's slide progress toward the next sample slot, 0..1. */
function slideProgress(stream: LevelStream, now: number): number {
  return clamp01((now - stream.lastAt) / SAMPLE_MS);
}

function capsule(ctx: CanvasRenderingContext2D, x: number, cy: number, width: number, height: number) {
  const h = Math.max(width, height);
  ctx.beginPath();
  ctx.roundRect(x, cy - h / 2, width, h, width / 2);
  ctx.fill();
}

type Draw = (ctx: CanvasRenderingContext2D, w: number, h: number, stream: LevelStream, now: number, dt: number) => void;

/** A crisp canvas the size of its box, redrawn every animation frame in the current text color. */
function useCanvas(draw: Draw) {
  const ref = useRef<HTMLCanvasElement>(null);
  const stream = useLevelStream();
  const drawRef = useRef(draw);
  drawRef.current = draw;
  useEffect(() => {
    const canvas = ref.current;
    const ctx = canvas?.getContext("2d");
    if (!canvas || !ctx) return;
    let w = 0;
    let h = 0;
    const resize = () => {
      const rect = canvas.getBoundingClientRect();
      const dpr = window.devicePixelRatio || 1;
      w = rect.width;
      h = rect.height;
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    };
    const observer = new ResizeObserver(resize);
    observer.observe(canvas);
    resize();
    let last = performance.now();
    let raf = 0;
    const frame = () => {
      const real = performance.now();
      const dt = Math.min(64, real - last);
      last = real;
      ctx.clearRect(0, 0, w, h);
      ctx.fillStyle = ctx.strokeStyle = getComputedStyle(canvas).color;
      ctx.globalAlpha = 1;
      drawRef.current(ctx, w, h, stream, stream.now(), dt);
      raf = requestAnimationFrame(frame);
    };
    raf = requestAnimationFrame(frame);
    return () => {
      cancelAnimationFrame(raf);
      observer.disconnect();
    };
  }, [stream]);
  return ref;
}

function WaveCanvas({ draw, className }: { draw: Draw; className?: string }) {
  const ref = useCanvas(draw);
  return <canvas ref={ref} aria-hidden className={cn("block h-5 w-full", className)} />;
}

/** A: what ships today (packages/views/voice/voice-ui.tsx), DOM bars re-rendered per sample. */
function CurrentWave({ density, className }: WaveProps) {
  const { samples } = useLevelSamples();
  const bars = density === "hud" ? 28 : 64;
  return (
    <div
      aria-hidden
      className={cn("flex h-5 items-center justify-end gap-[2px] overflow-hidden", className)}
      style={{ maskImage: "linear-gradient(90deg, transparent 0, #000 24px)" }}
    >
      {samples.slice(-Math.min(bars, 64)).map((level, index) => (
        <span
          key={index}
          className="w-[2px] shrink-0 rounded-full bg-current"
          style={{ height: `${Math.max(10, Math.min(100, level * 110))}%` }}
        />
      ))}
    </div>
  );
}

/** B: the same history, sliding continuously and smoothed, with a dot line for silence. */
function SmoothWave({ density, className }: WaveProps) {
  const pitch = density === "hud" ? 3.5 : 4;
  const bar = 2;
  const fade = density === "hud" ? 16 : 28;
  return (
    <WaveCanvas
      className={className}
      draw={(ctx, w, h, stream, now) => {
        const s = stream.samples;
        const progress = slideProgress(stream, now);
        const count = Math.ceil(w / pitch) + 2;
        for (let k = 0; k < count; k++) {
          const i = s.length - 1 - k;
          if (i < 0) break;
          const v = 0.25 * (s[i - 1] ?? s[i]!) + 0.5 * s[i]! + 0.25 * (s[i + 1] ?? s[i]!);
          const grow = k === 0 ? easeOut(progress) : 1;
          const x = w - bar - (k + progress) * pitch;
          if (x < -bar) break;
          ctx.globalAlpha = clamp01(x / fade);
          capsule(ctx, x, h / 2, bar, Math.max(2, perceptual(v) * h * grow));
        }
      }}
    />
  );
}

/** C: silence is a row of dots; speech stretches dots into thicker capsules. */
function DotsWave({ density, className }: WaveProps) {
  const pitch = density === "hud" ? 4.5 : 5;
  const bar = density === "hud" ? 2.5 : 3;
  const fade = density === "hud" ? 14 : 24;
  return (
    <WaveCanvas
      className={className}
      draw={(ctx, w, h, stream, now) => {
        const s = stream.samples;
        const progress = slideProgress(stream, now);
        const count = Math.ceil(w / pitch) + 2;
        for (let k = 0; k < count; k++) {
          const i = s.length - 1 - k;
          if (i < 0) break;
          const p = perceptual(s[i]!);
          const x = w - bar - (k + progress) * pitch;
          if (x < -bar) break;
          const edge = clamp01(x / fade);
          if (p < 0.08) {
            ctx.globalAlpha = 0.4 * edge;
            capsule(ctx, x, h / 2, bar, bar);
            continue;
          }
          const grow = k === 0 ? easeOut(progress) : 1;
          ctx.globalAlpha = edge;
          capsule(ctx, x, h / 2, bar, bar + (h - bar) * p * grow);
        }
      }}
    />
  );
}

/** D: bars stay put and dance with the spectrum, low frequencies in the middle (Handy-style overlay). */
function SpectrumWave({ density, className }: WaveProps) {
  const pitch = density === "hud" ? 4.5 : 5;
  const bar = density === "hud" ? 2.5 : 3;
  const heights = useRef<number[]>([]);
  return (
    <WaveCanvas
      className={className}
      draw={(ctx, w, h, stream, _now, dt) => {
        // A compact cluster reads as one voice; a full-width row is mostly idle highs.
        const count = Math.max(3, Math.min(density === "hud" ? 19 : 33, Math.floor((w + pitch - bar) / pitch)));
        const offset = (w - (count * pitch - (pitch - bar))) / 2;
        const center = (count - 1) / 2;
        const bands = stream.bands;
        for (let i = 0; i < count; i++) {
          const d = center === 0 ? 0 : Math.abs(i - center) / center;
          // Speech lives below ~3 kHz: spread the lower 60% of bands across the cluster.
          const f = d * (BANDS - 1) * 0.6;
          const lo = Math.floor(f);
          const mix = f - lo;
          const band = (bands[lo] ?? 0) * (1 - mix) + (bands[lo + 1] ?? bands[lo] ?? 0) * mix;
          const target = Math.max(2, clamp01(band * (1.25 - 0.45 * d)) * h);
          const cur = heights.current[i] ?? 2;
          heights.current[i] = follow(cur, target, dt, 40, 170);
          capsule(ctx, offset + i * pitch, h / 2, bar, heights.current[i]!);
        }
      }}
    />
  );
}

/** E: three layered sine curves whose amplitude follows the voice, tapered at both ends. */
function LineWave({ density, className }: WaveProps) {
  const amp = useRef(0);
  const layers = [
    { k: 1, freq: 0.85, speed: 1.1, alpha: 1, width: 1.5 },
    { k: 0.65, freq: 1.3, speed: -1.6, alpha: 0.45, width: 1.25 },
    { k: 0.4, freq: 1.8, speed: 2.2, alpha: 0.25, width: 1 },
  ];
  const step = density === "hud" ? 1.5 : 2;
  return (
    <WaveCanvas
      className={className}
      draw={(ctx, w, h, stream, now, dt) => {
        const latest = stream.samples[stream.samples.length - 1] ?? 0;
        amp.current = follow(amp.current, Math.min(1, perceptual(latest) * 1.5), dt, 70, 260);
        const a = 0.07 + amp.current * 0.93;
        const t = now / 1000;
        // Keep the wavelength constant, so a wide strip shows more cycles instead of a stretched curve.
        const cycles = Math.max(2, w / 150);
        ctx.lineCap = "round";
        ctx.lineJoin = "round";
        for (const layer of layers) {
          ctx.globalAlpha = layer.alpha;
          ctx.lineWidth = layer.width;
          ctx.beginPath();
          for (let x = 0; x <= w; x += step) {
            const u = x / w;
            const env = Math.pow(Math.sin(Math.PI * u), 2);
            const phase = t * layer.speed * (1 + amp.current * 1.4);
            const y = h / 2 + Math.sin(2 * Math.PI * layer.freq * cycles * u + phase) * env * a * layer.k * (h / 2 - 1.5);
            if (x === 0) ctx.moveTo(x, y);
            else ctx.lineTo(x, y);
          }
          ctx.stroke();
        }
      }}
    />
  );
}

/** F: the whole recording fills the track left to right, halving its resolution when full. */
function TimelineWave({ density, className }: WaveProps) {
  const pitch = density === "hud" ? 3 : 4;
  const bar = density === "hud" ? 1.5 : 2;
  return (
    <WaveCanvas
      className={className}
      draw={(ctx, w, h, stream, now) => {
        const s = stream.samples;
        const slots = Math.max(1, Math.floor((w + pitch - bar) / pitch));
        let bucket = 1;
        while (Math.ceil(s.length / bucket) > slots) bucket *= 2;
        const used = Math.ceil(s.length / bucket);
        const progress = slideProgress(stream, now);
        for (let i = 0; i < slots; i++) {
          const x = i * pitch;
          if (i >= used) {
            ctx.globalAlpha = 0.18;
            capsule(ctx, x, h / 2, bar, bar);
            continue;
          }
          let peak = 0;
          for (let j = i * bucket; j < Math.min(s.length, (i + 1) * bucket); j++) peak = Math.max(peak, s[j]!);
          const grow = bucket === 1 && i === used - 1 ? easeOut(progress) : 1;
          ctx.globalAlpha = 1;
          capsule(ctx, x, h / 2, bar, Math.max(bar, perceptual(peak) * h * grow));
        }
      }}
    />
  );
}

export type VariantId = "current" | "smooth" | "dots" | "spectrum" | "line" | "timeline";

export interface WaveVariant {
  id: VariantId;
  letter: string;
  name: Record<Lang, string>;
  pitch: Record<Lang, string>;
  notes: Record<Lang, string[]>;
  Wave: ComponentType<WaveProps>;
}

export const VARIANTS: WaveVariant[] = [
  {
    id: "current",
    letter: "A",
    name: { zh: "现状", en: "Current" },
    pitch: {
      zh: "线上实现：每 50ms 来一个采样，整排柱子向左跳一格。",
      en: "What ships: a sample every 50 ms, and the whole row jumps one slot left.",
    },
    notes: {
      zh: ["20 fps 的跳格能看出顿挫", "安静时柱子仍有 10% 高，看起来一直在“响”", "线性映射：小声几乎不动，大声很快顶格"],
      en: ["The 20 fps step is visible as stutter", "Bars keep 10% height in silence, so it always looks loud", "Linear mapping: soft speech barely moves, loud speech clips fast"],
    },
    Wave: CurrentWave,
  },
  {
    id: "smooth",
    letter: "B",
    name: { zh: "平滑滚动", en: "Smooth scroll" },
    pitch: {
      zh: "同样的历史柱状图，改成 60fps 连续滑动，新柱从右边“长”出来。",
      en: "The same history bars, gliding at 60 fps, with new bars growing in at the right.",
    },
    notes: {
      zh: ["和现状最接近，改动最小", "相邻采样轻微平滑，高度用感知曲线映射", "安静时收成一条 2px 的点线"],
      en: ["Closest to today, the smallest change", "Neighbouring samples are smoothed; heights use a perceptual curve", "Silence collapses to a 2 px dotted line"],
    },
    Wave: SmoothWave,
  },
  {
    id: "dots",
    letter: "C",
    name: { zh: "静音成点", en: "Dots to bars" },
    pitch: {
      zh: "安静时是一排浅色小圆点，说话时圆点被撑开成胶囊。",
      en: "Silence is a row of faint dots; speech stretches them into capsules.",
    },
    notes: {
      zh: ["一眼区分“在听但没声音”和“正在说话”", "柱子更粗(3px)、更稀，读起来更轻松", "观感接近 ChatGPT 的听写"],
      en: ["Tells “listening, but quiet” from “speaking” at a glance", "Thicker (3 px), sparser bars that read more calmly", "Feels close to ChatGPT dictation"],
    },
    Wave: DotsWave,
  },
  {
    id: "spectrum",
    letter: "D",
    name: { zh: "实时频谱", en: "Live spectrum" },
    pitch: {
      zh: "柱子不滚动，原地随频谱起伏；低频在中间，高频在两侧。",
      en: "Bars stay put and move with the spectrum, lows in the middle and highs at the sides.",
    },
    notes: {
      zh: ["参考 Handy 的录音浮层", "快起慢落，跳动有弹性", "需要录音器多输出一份频谱（AnalyserNode 现成就有），目前只输出音量"],
      en: ["After Handy’s recording overlay", "Fast attack, slow release, so it bounces", "The recorder would also need to emit a spectrum (AnalyserNode has it); today it only emits level"],
    },
    Wave: SpectrumWave,
  },
  {
    id: "line",
    letter: "E",
    name: { zh: "流动曲线", en: "Flowing line" },
    pitch: {
      zh: "三条叠加的正弦曲线，振幅跟着音量走，两端收尖。",
      en: "Three layered sine curves whose amplitude follows the voice, tapered at both ends.",
    },
    notes: {
      zh: ["最安静、最不抢眼", "只反映当下，看不出前几秒的起伏", "安静时是一条缓慢呼吸的细线"],
      en: ["The calmest and least distracting", "Shows only the present, not the last few seconds", "Silence is a thin, slowly breathing line"],
    },
    Wave: LineWave,
  },
  {
    id: "timeline",
    letter: "F",
    name: { zh: "全程概览", en: "Whole recording" },
    pitch: {
      zh: "柱子从左往右写满整条，写满后自动压缩，始终显示整段录音。",
      en: "Bars fill the track left to right, then compress, so the whole recording stays visible.",
    },
    notes: {
      zh: ["能感知“已经说了多久”", "空余位置用浅点占位", "录得越久，细节被压得越狠"],
      en: ["Gives a sense of how long you have been talking", "Empty slots show as faint dots", "Long recordings lose detail as they compress"],
    },
    Wave: TimelineWave,
  },
];

export const VARIANT_BY_ID = Object.fromEntries(VARIANTS.map((v) => [v.id, v])) as Record<VariantId, WaveVariant>;
