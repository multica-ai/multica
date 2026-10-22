"use client";

import { useEffect, useRef, useState, useSyncExternalStore, type ComponentType, type ReactNode } from "react";
import { Bot } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { HERO_PROVIDERS } from "../../components/provider-marks";

/*
 * Shared pieces for the landing's product mockups: the fictional Acme Labs
 * team and its avatars, and a canvas that lays a mockup out at a fixed design
 * size and zooms it to whatever frame it sits in.
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

/**
 * Fills its positioned parent and renders `children` on a `width` × `height`
 * design canvas scaled to the parent's width. The parent sets the aspect
 * ratio; keep it equal to the canvas's so nothing is cropped unintentionally.
 */
export function ZoomCanvas({ width, height, children }: { width: number; height: number; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const [zoom, setZoom] = useState<number | null>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const resize = new ResizeObserver(() => setZoom(el.clientWidth / width));
    resize.observe(el);
    return () => resize.disconnect();
  }, [width]);

  return (
    <div ref={ref} className="absolute inset-0 overflow-hidden">
      <div
        aria-hidden
        className={cn("relative text-foreground", zoom === null && "opacity-0")}
        style={{ width, height, zoom: zoom ?? undefined }}
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
