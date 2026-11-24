"use client";

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useTheme } from "next-themes";
import { Pause, Play, RotateCcw } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useDocsLocale } from "@/components/locale-link";
import type { Lang } from "@/lib/i18n";
import { demoChromeCopy } from "@/lib/translations";
import { CLIP_MANIFEST } from "./clip-manifest";

// Shared primitives for the three native demo mediums in Multica Docs:
//   1. EditorialClip    — real English product recordings + localized chapter scrubber
//   2. EditorialFigure  — architectural SVG schematics aligned with the warm-paper palette
//   3. EditorialUtility — borderless prose-embedded decision tools & matrices

const VIDEO_BASE = "/docs/videos/demos";

/** Picks a demo's scenario copy for the page language, falling back to English. */
export function useDemoCopy<T>(copy: { en: T } & Partial<Record<Lang, T>>): T {
  const lang = useDocsLocale();
  return copy[lang] ?? copy.en;
}

export interface ClipChapter {
  /** Short localized chapter label shown below its progress segment. */
  label: string;
  /** Timestamp in seconds where this chapter begins. */
  at: number;
}

function formatClock(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const mins = Math.floor(s / 60);
  const secs = s % 60;
  return `${mins}:${secs.toString().padStart(2, "0")}`;
}

/**
 * Medium 1: Real recorded English product clip with a quiet editorial frame
 * and clickable chapter progress bar. Auto-plays muted when visible (unless
 * reduced motion is requested) and pauses when scrolled offscreen.
 */
export function EditorialClip({
  slug,
  title,
  chapters,
  duration,
  caption,
}: {
  slug: string;
  title: string;
  chapters: ClipChapter[];
  /** Nominal duration in seconds, used before video metadata arrives. */
  duration: number;
  caption?: string;
}) {
  const lang = useDocsLocale();
  const chrome = demoChromeCopy[lang];
  const { resolvedTheme } = useTheme();
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  const theme = mounted && resolvedTheme === "dark" ? "dark" : "light";
  const videoSrc = `${VIDEO_BASE}/${slug}-${theme}.mp4`;
  const posterSrc = `${VIDEO_BASE}/${slug}-${theme}.jpg`;

  const darkVariant = theme === "dark" ? CLIP_MANIFEST[slug]?.dark : undefined;
  const activeChapters = darkVariant
    ? chapters.map((ch, i) => ({
        label: ch.label,
        at: darkVariant.chapters[i]?.at ?? ch.at,
      }))
    : chapters;
  const nominalDuration = darkVariant?.duration ?? duration;

  const figureRef = useRef<HTMLElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);

  const [playing, setPlaying] = useState(false);
  const [userPaused, setUserPaused] = useState(false);
  const [inView, setInView] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [mediaDuration, setMediaDuration] = useState(nominalDuration);

  const totalDuration = mediaDuration > 0 ? mediaDuration : nominalDuration;

  // Pause when scrolled offscreen; autoplay when visible unless user paused or reduced motion.
  useEffect(() => {
    const el = figureRef.current;
    if (!el) return;
    const observer = new IntersectionObserver(
      ([entry]) => setInView(Boolean(entry?.isIntersecting)),
      { threshold: 0.25 },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (inView && !userPaused && !reducedMotion) {
      video.play().catch(() => {});
    } else if (!inView && !video.paused) {
      video.pause();
    }
  }, [inView, userPaused, videoSrc]);

  // Keep current playback position when toggling between light and dark video sources.
  const prevTimeRef = useRef(0);
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    const resumeAt = prevTimeRef.current;
    const onLoaded = () => {
      if (Number.isFinite(video.duration) && video.duration > 0) {
        setMediaDuration(video.duration);
      }
      if (resumeAt > 0 && resumeAt < (video.duration || duration)) {
        video.currentTime = resumeAt;
      }
    };
    video.addEventListener("loadedmetadata", onLoaded);
    return () => video.removeEventListener("loadedmetadata", onLoaded);
  }, [videoSrc, duration]);

  const togglePlay = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    if (video.paused) {
      setUserPaused(false);
      video.play().catch(() => {});
    } else {
      setUserPaused(true);
      video.pause();
    }
  }, []);

  const seekTo = useCallback((at: number) => {
    const video = videoRef.current;
    if (!video) return;
    video.currentTime = at;
    prevTimeRef.current = at;
    setCurrentTime(at);
    setUserPaused(false);
    video.play().catch(() => {});
  }, []);

  const activeChapter = (() => {
    let idx = 0;
    for (let i = 0; i < activeChapters.length; i++) {
      if (currentTime + 0.08 >= activeChapters[i]!.at) idx = i;
    }
    return idx;
  })();

  const chapterProgress = (idx: number) => {
    const start = activeChapters[idx]!.at;
    const end = activeChapters[idx + 1]?.at ?? totalDuration;
    if (currentTime <= start) return 0;
    if (currentTime >= end) return 100;
    const span = Math.max(0.1, end - start);
    return Math.min(100, Math.max(0, ((currentTime - start) / span) * 100));
  };

  return (
    <figure
      ref={figureRef}
      className="not-prose my-7 border-y border-[var(--docs-rule)] py-4"
    >
      <div className="mb-3 flex items-baseline justify-between gap-4">
        <div className="min-w-0 font-[family-name:var(--font-serif)] text-[1.0625rem] leading-[1.35] tracking-[-0.01em] text-foreground">
          {title}
        </div>
        <div className="flex shrink-0 items-center gap-2 font-mono text-[0.6875rem] tabular-nums text-muted-foreground">
          <span>
            {formatClock(currentTime)} / {formatClock(totalDuration)}
          </span>
          <button
            type="button"
            onClick={togglePlay}
            aria-label={playing ? chrome.pause : chrome.play}
            className="inline-flex size-6 items-center justify-center rounded-sm border border-border bg-card text-foreground transition-colors hover:border-[var(--primary)] hover:text-[var(--primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            {playing ? <Pause className="size-3" /> : <Play className="size-3 translate-x-[0.5px]" />}
          </button>
        </div>
      </div>

      <div
        className="overflow-hidden rounded-[5px] border border-border bg-muted/40 cursor-pointer"
        onClick={togglePlay}
      >
        <video
          key={videoSrc}
          ref={videoRef}
          src={videoSrc}
          poster={posterSrc}
          muted
          loop
          playsInline
          preload="metadata"
          aria-label={title}
          onPlay={() => setPlaying(true)}
          onPause={() => setPlaying(false)}
          onTimeUpdate={(e) => {
            const t = e.currentTarget.currentTime;
            prevTimeRef.current = t;
            setCurrentTime(t);
          }}
          className="block aspect-[16/10] w-full bg-background object-cover"
        />
      </div>

      {activeChapters.length > 0 && (
        <div
          className="mt-3 grid gap-2.5"
          style={{ gridTemplateColumns: `repeat(${activeChapters.length}, minmax(0, 1fr))` }}
        >
          {activeChapters.map((chapter, idx) => {
            const pct = chapterProgress(idx);
            const active = idx === activeChapter;
            const done = idx < activeChapter;
            return (
              <button
                key={`${chapter.at}-${chapter.label}`}
                type="button"
                onClick={() => seekTo(chapter.at)}
                className="group flex min-w-0 flex-col gap-1.5 rounded-sm text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
              >
                <span className="block h-[2.5px] w-full overflow-hidden rounded-full bg-border">
                  <span
                    className={cn(
                      "block h-full rounded-full transition-[width] duration-150 ease-linear motion-reduce:transition-none",
                      active ? "bg-[var(--primary)]" : done ? "bg-foreground/35" : "bg-transparent",
                    )}
                    style={{ width: `${pct}%` }}
                  />
                </span>
                <span
                  className={cn(
                    "truncate text-[0.75rem] leading-[1.35] transition-colors",
                    active
                      ? "font-medium text-foreground"
                      : "text-muted-foreground group-hover:text-foreground",
                  )}
                >
                  <span className="mr-1 font-mono text-[0.65rem] opacity-65">
                    {String(idx + 1).padStart(2, "0")}
                  </span>
                  {chapter.label}
                </span>
              </button>
            );
          })}
        </div>
      )}

      {caption && (
        <figcaption className="mt-2.5 text-[0.8125rem] leading-[1.5] text-muted-foreground">
          {caption}
        </figcaption>
      )}
    </figure>
  );
}

/**
 * Medium 2: Publication-grade architectural schematic frame.
 * Hairline top/bottom rules, editorial serif title, warm-paper SVG surface,
 * and a fixed-height quiet caption strip below.
 */
export function EditorialFigure({
  kicker,
  title,
  actions,
  caption,
  children,
}: {
  kicker?: string;
  title: string;
  actions?: ReactNode;
  caption?: ReactNode;
  children: ReactNode;
}) {
  return (
    <figure className="not-prose my-7 border-y border-[var(--docs-rule)] py-4">
      <div className="mb-3 flex flex-wrap items-baseline justify-between gap-3">
        <div className="flex min-w-0 items-baseline gap-2.5">
          {kicker && (
            <span className="shrink-0 font-mono text-[0.6875rem] font-semibold uppercase tracking-[0.08em] text-[var(--primary)]">
              {kicker}
            </span>
          )}
          <span className="font-[family-name:var(--font-serif)] text-[1.0625rem] leading-[1.35] tracking-[-0.01em] text-foreground">
            {title}
          </span>
        </div>
        {actions}
      </div>

      <div className="rounded-[4px] border border-border bg-card/70 p-3.5 sm:p-5">
        {children}
      </div>

      {caption && (
        <figcaption className="mt-3 min-h-[1.35rem] text-[0.8125rem] leading-[1.55] text-muted-foreground">
          {caption}
        </figcaption>
      )}
    </figure>
  );
}

/**
 * Medium 3: Borderless prose-embedded utility (filters, decision matrices,
 * role tables). No card box, no blue "Try it" badge, no pulsing cues.
 */
export function EditorialUtility({
  title,
  meta,
  onReset,
  children,
}: {
  title: string;
  meta?: ReactNode;
  onReset?: () => void;
  children: ReactNode;
}) {
  const lang = useDocsLocale();
  const chrome = demoChromeCopy[lang];
  return (
    <section
      aria-label={title}
      className="not-prose my-7 border-y border-[var(--docs-rule)] py-4"
    >
      <div className="mb-3 flex flex-wrap items-baseline justify-between gap-2">
        <div className="font-[family-name:var(--font-serif)] text-[1.0625rem] leading-[1.35] tracking-[-0.01em] text-foreground">
          {title}
        </div>
        <div className="flex items-center gap-3 text-[0.75rem] text-muted-foreground">
          {meta}
          {onReset && (
            <button
              type="button"
              onClick={onReset}
              className="inline-flex items-center gap-1 text-muted-foreground transition-colors hover:text-foreground"
            >
              <RotateCcw className="size-3" />
              {chrome.reset}
            </button>
          )}
        </div>
      </div>
      {children}
    </section>
  );
}
