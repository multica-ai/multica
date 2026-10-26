"use client";

import { useEffect, useRef, useState, useSyncExternalStore, type ComponentType, type ReactNode } from "react";
import {
  BookOpenText,
  Bot,
  CircleUser,
  FolderKanban,
  Inbox,
  ListTodo,
  MessageSquare,
  Monitor,
  Zap,
  type LucideIcon,
} from "lucide-react";
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
export const RESEARCH: Actor = { kind: "agent", name: "Research Agent", provider: "Kimi CLI" };
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
      if (!el.clientWidth) return;
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

/** Elevation steps from the product: a card raised off a muted column, and a panel floating over the page. */
export const RAISED = "rounded-lg border-[0.5px] border-surface-border bg-surface shadow-[var(--surface-shadow)]";
export const FLOATING = "rounded-xl bg-background shadow-[var(--floating-shadow)] ring-1 ring-surface-border";

const APP_NAV: { key: string; icon: LucideIcon; badge?: boolean }[] = [
  { key: "inbox", icon: Inbox, badge: true },
  { key: "mine", icon: CircleUser },
  { key: "chat", icon: MessageSquare },
  { key: "issues", icon: ListTodo },
  { key: "projects", icon: FolderKanban },
  { key: "autopilots", icon: Zap },
  { key: "agents", icon: Bot },
  { key: "skills", icon: BookOpenText },
  { key: "runtimes", icon: Monitor },
];

/**
 * The Multica app as a window, in the same two layers as the hero: the grey
 * app shell with a collapsed sidebar, and the page on an inset panel. A
 * window that runs off the right and bottom of its frame passes `bleed` (in
 * px), and the page keeps its content out of the cropped strip so nothing is
 * cut mid-word.
 */
export function AppWindow({
  className,
  active,
  bleed = 0,
  children,
}: {
  className: string;
  active: string;
  bleed?: number;
  children: ReactNode;
}) {
  return (
    <div
      className={cn("absolute flex overflow-hidden rounded-xl border border-site-line-strong bg-app-shell shadow-site-float", className)}
    >
      <div className="flex w-11 shrink-0 flex-col items-center gap-1 py-2.5">
        <span className="mb-1.5 flex size-6 items-center justify-center rounded-md bg-surface text-micro font-semibold shadow-[var(--surface-shadow)]">
          A
        </span>
        {APP_NAV.map((item) => (
          <span
            key={item.key}
            className={cn(
              "relative flex size-7 shrink-0 items-center justify-center rounded-md",
              item.key === active ? "bg-surface-selected text-foreground" : "text-muted-foreground",
            )}
          >
            <item.icon className="size-3.5" />
            {item.badge ? <span className="absolute top-1 right-1 size-1.5 rounded-full bg-brand" /> : null}
          </span>
        ))}
      </div>
      <div className="relative my-1.5 mr-1.5 flex min-w-0 flex-1 overflow-hidden rounded-lg border border-surface-border bg-background">
        <div className="relative flex min-w-0 flex-1 flex-col" style={{ marginRight: bleed, marginBottom: bleed }}>
          {children}
        </div>
      </div>
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
