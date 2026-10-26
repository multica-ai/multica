"use client";

import { useEffect, useRef, useState, useSyncExternalStore, type ComponentType, type ReactNode } from "react";
import { Bot, type LucideIcon } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { ShimmerText } from "@multica/ui/components/common/shimmer-text";
import { HERO_PROVIDERS } from "../../components/provider-marks";

/*
 * Shared pieces for the landing's product mockups: the fictional Acme Labs
 * team and its avatars, window chrome, a canvas that lays a mockup out at a
 * fixed design size and zooms it to whatever frame it sits in, and the loop
 * that steps a mockup's story while it is on screen.
 */

export type Actor =
  | { kind: "member"; name: string; initials: string }
  | { kind: "agent"; name: string; provider: string };

export const MIA: Actor = { kind: "member", name: "Mia Chen", initials: "MC" };
export const LEO: Actor = { kind: "member", name: "Leo Park", initials: "LP" };
export const SAM: Actor = { kind: "member", name: "Sam Rivera", initials: "SR" };
export const BACKEND: Actor = { kind: "agent", name: "Backend Agent", provider: "Claude Code" };
export const QA: Actor = { kind: "agent", name: "QA Agent", provider: "Codex" };
export const FRONTEND: Actor = { kind: "agent", name: "Frontend Agent", provider: "Cursor" };
export const LEAD: Actor = { kind: "agent", name: "Lead Agent", provider: "Claude Code" };
export const CONTENT: Actor = { kind: "agent", name: "Content Agent", provider: "Hermes" };
export const TRIAGE: Actor = { kind: "agent", name: "Triage Agent", provider: "OpenCode" };

export const PROVIDER_MARKS = new Map<string, ComponentType<{ className?: string }>>(
  HERO_PROVIDERS.map((provider) => [provider.name, provider.Mark]),
);

const AVATAR_MARK = { 16: "size-2.5", 20: "size-3", 24: "size-3.5" } as const;

export function Avatar({ actor, size }: { actor: Actor; size: 16 | 20 | 24 }) {
  if (actor.kind === "member") {
    return <ActorAvatar name={actor.name} initials={actor.initials} size={size === 16 ? "xs" : "sm"} />;
  }
  const Mark = PROVIDER_MARKS.get(actor.provider);
  return (
    <span
      className="inline-flex shrink-0 items-center justify-center rounded-full border border-border bg-background text-foreground"
      style={{ width: size, height: size }}
    >
      {Mark ? <Mark className={AVATAR_MARK[size]} /> : <Bot className="size-3" />}
    </span>
  );
}

/** Largest zoom a mockup is shown at, so its UI never reads bigger than the product's. */
const MAX_ZOOM = 0.88;

/**
 * Fills its positioned parent and renders `children` on a design canvas
 * zoomed to the parent's width. Below `width` the canvas stays `width` ×
 * `height` and shrinks to fit; past `MAX_ZOOM` it grows to fill the frame
 * instead, so lay content out against the canvas edges, not at fixed spots.
 * The parent sets the aspect ratio; keep it equal to the canvas's.
 */
export function ZoomCanvas({ width, height, children }: { width: number; height: number; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const [fit, setFit] = useState<{ zoom: number; width: number; height: number } | null>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const resize = new ResizeObserver(() => {
      const zoom = Math.min(el.clientWidth / width, MAX_ZOOM);
      setFit({ zoom, width: Math.max(width, el.clientWidth / zoom), height: Math.max(height, el.clientHeight / zoom) });
    });
    resize.observe(el);
    return () => resize.disconnect();
  }, [width, height]);

  return (
    <div ref={ref} className="absolute inset-0 overflow-hidden">
      <div
        aria-hidden
        className={cn("relative text-foreground", fit === null && "opacity-0")}
        style={{ width: fit?.width ?? width, height: fit?.height ?? height, zoom: fit?.zoom }}
      >
        {children}
      </div>
    </div>
  );
}

export function usePrefersReducedMotion() {
  return useSyncExternalStore(
    (onChange) => {
      const query = window.matchMedia("(prefers-reduced-motion: reduce)");
      query.addEventListener("change", onChange);
      return () => query.removeEventListener("change", onChange);
    },
    () => window.matchMedia("(prefers-reduced-motion: reduce)").matches,
    () => true,
  );
}

/**
 * Steps through a looping story, holding step `i` for `holds[i]` ms. The loop
 * runs only while the element behind `ref` is on screen; under reduced motion
 * it rests on `still`, the one frame that tells the story on its own.
 */
export function useStoryLoop(holds: readonly number[], still: number) {
  const ref = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(false);
  const [step, setStep] = useState(0);
  const reducedMotion = usePrefersReducedMotion();

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const intersection = new IntersectionObserver(([entry]) => setVisible(entry?.isIntersecting ?? false));
    intersection.observe(el);
    return () => intersection.disconnect();
  }, []);

  useEffect(() => {
    if (!visible || reducedMotion) return;
    const timer = setTimeout(() => setStep((current) => (current + 1) % holds.length), holds[step]);
    return () => clearTimeout(timer);
  }, [holds, step, visible, reducedMotion]);

  return { ref, step: reducedMotion ? still : step };
}

// Window chrome and small product atoms shared by the mockups.

export const ENTER = "animate-in fade-in slide-in-from-bottom-1 duration-300 motion-reduce:animate-none";

export function Window({ className, children }: { className: string; children: ReactNode }) {
  return (
    <div
      className={cn(
        "absolute flex flex-col overflow-hidden rounded-xl border border-site-line-strong bg-background shadow-site-float",
        className,
      )}
    >
      {children}
    </div>
  );
}

export function WindowHeader({ icon: Icon, title, children }: { icon: LucideIcon; title: string; children?: ReactNode }) {
  return (
    <div className="flex h-11 shrink-0 items-center gap-2 border-b border-border px-4">
      <Icon className="size-4 text-muted-foreground" />
      <span className="flex-1 text-label font-medium">{title}</span>
      {children}
    </div>
  );
}

export function Working({ label = "Working" }: { label?: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="size-1.5 shrink-0 rounded-full bg-site-agent" />
      <ShimmerText className="text-micro text-muted-foreground">{label}</ShimmerText>
    </span>
  );
}
