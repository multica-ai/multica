"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from "react";
import {
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  Bot,
  Check,
  CornerDownRight,
  Loader2,
  RotateCcw,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useDocsLocale } from "@/components/locale-link";
import type { Lang } from "@/lib/i18n";
import { demoChromeCopy } from "@/lib/translations";

// Building blocks for the scripted product demos embedded in docs pages. The
// demos imitate the product with docs-local markup instead of importing
// @multica/views, which needs live stores and queries.

/** Picks a demo's scenario copy for the page language, falling back to English. */
export function useDemoCopy<T>(copy: { en: T } & Partial<Record<Lang, T>>): T {
  const lang = useDocsLocale();
  return copy[lang] ?? copy.en;
}

/** Timers owned by one demo run: cleared on replay and on unmount. */
export function useDemoTimers() {
  const ids = useRef<number[]>([]);
  const clear = useCallback(() => {
    for (const id of ids.current) window.clearTimeout(id);
    ids.current = [];
  }, []);
  const after = useCallback((ms: number, fn: () => void) => {
    ids.current.push(window.setTimeout(fn, ms));
  }, []);
  useEffect(() => clear, [clear]);
  return { after, clear };
}

/**
 * True once the element has been mostly on screen, so a demo starts when the
 * reader reaches it rather than while they read the top of the page.
 */
export function useStartWhenVisible<T extends Element>(): [RefObject<T | null>, boolean] {
  const ref = useRef<T>(null);
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    const el = ref.current;
    if (!el || visible) return;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry?.isIntersecting) {
          setVisible(true);
          observer.disconnect();
        }
      },
      { threshold: 0.35 },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [visible]);
  return [ref, visible];
}

export function DemoFrame({
  title,
  caption,
  onReplay,
  frameRef,
  children,
}: {
  title: string;
  /** What the reader should do or notice now; announced to screen readers. */
  caption: ReactNode;
  onReplay: () => void;
  frameRef?: RefObject<HTMLElement | null>;
  children: ReactNode;
}) {
  const lang = useDocsLocale();
  const chrome = demoChromeCopy[lang];
  // `isolate` keeps the demos' own z-indexes below the sticky page header.
  return (
    <figure
      ref={frameRef}
      className="not-prose isolate my-6 overflow-hidden rounded-xl border bg-card text-card-foreground shadow-sm"
    >
      <div className="flex items-center gap-2 border-b bg-muted/40 py-1.5 pr-1.5 pl-3">
        <span className="shrink-0 rounded-sm bg-brand/10 px-1.5 py-0.5 text-micro font-medium text-brand">
          {chrome.label}
        </span>
        <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground">{title}</span>
        <Button type="button" size="xs" variant="ghost" className="text-muted-foreground" onClick={onReplay}>
          <RotateCcw />
          {chrome.replay}
        </Button>
      </div>
      <div className="relative">{children}</div>
      <figcaption
        role="status"
        aria-live="polite"
        className="border-t bg-muted/40 px-3 py-2 text-label text-muted-foreground"
      >
        {caption}
      </figcaption>
    </figure>
  );
}

/**
 * Draws the eye to the control the reader should use next: a pulsing ring
 * around it and a plain "Click" with an arrow pointing at it. The control's
 * own label says what it does, so the hint never repeats it. It is
 * decorative; the frame caption carries the instruction for assistive
 * technology.
 */
export function CoachMark({
  active,
  quiet = false,
  side = "top",
  align = "center",
  ringClassName = "rounded-lg",
  className,
  children,
}: {
  active: boolean;
  /** Show only the ring, e.g. where a label would cover neighbouring content. */
  quiet?: boolean;
  /** Where the label sits relative to the control; the arrow points back at it. */
  side?: "top" | "bottom" | "left" | "right";
  align?: "start" | "center" | "end";
  ringClassName?: string;
  className?: string;
  children: ReactNode;
}) {
  const lang = useDocsLocale();
  const Arrow = { top: ArrowDown, bottom: ArrowUp, left: ArrowRight, right: ArrowLeft }[side];
  const vertical = side === "top" || side === "bottom";
  return (
    <span className={cn("relative inline-flex", className)}>
      {children}
      {active && (
        <>
          <span
            aria-hidden
            className={cn("demo-halo pointer-events-none absolute -inset-1 ring-2 ring-brand", ringClassName)}
          />
          {!quiet && (
            <span
              aria-hidden
              className={cn(
                // Phones have no room for labels beside controls; the ring and the
                // caption remain.
                "pointer-events-none absolute z-20 flex w-max max-w-56 gap-0.5 text-caption font-medium text-brand motion-safe:animate-in motion-safe:fade-in max-sm:hidden",
                side === "top" && "bottom-full mb-1.5 flex-col",
                side === "bottom" && "top-full mt-1.5 flex-col-reverse",
                side === "left" && "top-1/2 right-full mr-2 -translate-y-1/2 items-center",
                side === "right" && "top-1/2 left-full ml-2 -translate-y-1/2 flex-row-reverse items-center",
                vertical && align === "start" && "left-0 items-start",
                vertical && align === "center" && "left-1/2 -translate-x-1/2 items-center",
                vertical && align === "end" && "right-0 items-end",
              )}
            >
              {/* A halo in the card color keeps lines underneath from crossing the text. */}
              <span className="[text-shadow:0_0_2px_var(--card),0_0_4px_var(--card),0_0_6px_var(--card)]">
                {demoChromeCopy[lang].click}
              </span>
              <Arrow className={cn("size-3.5 shrink-0", `demo-nudge-${side}`)} />
            </span>
          )}
        </>
      )}
    </span>
  );
}

export function DemoAvatar({ name, agent = false }: { name: string; agent?: boolean }) {
  return (
    <span
      aria-hidden
      className={cn(
        "inline-flex size-5 shrink-0 items-center justify-center rounded-full text-micro font-medium",
        agent ? "bg-brand/12 text-brand" : "bg-muted text-foreground",
      )}
    >
      {agent ? <Bot className="size-3" /> : name.slice(0, 1)}
    </span>
  );
}

export type StepState = "running" | "done";

export function DemoStep({ state, children }: { state: StepState; children: ReactNode }) {
  return (
    <li className="m-0 flex min-w-0 items-start gap-2 py-0.5 ps-0 text-caption text-muted-foreground motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-top-1">
      {state === "running" ? (
        <Loader2 aria-hidden className="mt-px size-3.5 shrink-0 text-brand motion-safe:animate-spin" />
      ) : (
        <Check aria-hidden className="mt-px size-3.5 shrink-0 text-success" />
      )}
      <span className={cn("min-w-0", state === "running" && "text-foreground")}>{children}</span>
    </li>
  );
}

/** The step-list marker for a message a running turn has read (mirrors the product). */
export function DemoSteerMarker({ label, detail }: { label: string; detail: string }) {
  return (
    <li className="m-0 flex min-w-0 items-center gap-2 rounded-xs bg-brand/7 px-1 py-1.5 text-caption motion-safe:animate-in motion-safe:fade-in dark:bg-brand/12">
      <CornerDownRight aria-hidden className="size-3.5 shrink-0 text-brand" />
      <span className="min-w-0 truncate">
        <span className="font-medium">{label}</span>
        <span className="text-muted-foreground"> · {detail}</span>
      </span>
    </li>
  );
}

/** One agent run as it appears in the issue timeline. */
export function DemoRun({
  agent,
  status,
  active,
  children,
}: {
  agent: string;
  status: string;
  active: boolean;
  children?: ReactNode;
}) {
  return (
    <div className="min-w-0 motion-safe:animate-in motion-safe:fade-in">
      <div className="flex min-h-7 items-center gap-2">
        <DemoAvatar name={agent} agent />
        <span className="truncate text-body font-medium">{agent}</span>
        <span className="inline-flex items-center gap-1 text-caption text-muted-foreground">
          {active ? (
            <Loader2 aria-hidden className="size-3 text-brand motion-safe:animate-spin" />
          ) : (
            <Check aria-hidden className="size-3 text-success" />
          )}
          {status}
        </span>
      </div>
      {children && <ul className="mt-1 flex flex-col gap-0.5 pl-7">{children}</ul>}
    </div>
  );
}

/** A posted comment in the issue timeline. */
export function DemoComment({
  author,
  agent = false,
  children,
  footer,
}: {
  author: string;
  agent?: boolean;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <div className="min-w-0 rounded-lg border bg-background px-3 py-2 motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-bottom-1">
      <div className="flex items-center gap-2">
        <DemoAvatar name={author} agent={agent} />
        <span className="text-label font-medium">{author}</span>
      </div>
      <div className="mt-1 pl-7 text-body break-words">{children}</div>
      {footer && <div className="mt-1.5 pl-7">{footer}</div>}
    </div>
  );
}

export function DemoReceipt({ read, children }: { read: boolean; children: ReactNode }) {
  return (
    <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
      {read ? (
        <Check aria-hidden className="size-3.5 shrink-0 text-success" />
      ) : (
        <Loader2 aria-hidden className="size-3.5 shrink-0 motion-safe:animate-spin" />
      )}
      {children}
    </span>
  );
}

/** The round send button of the comment composer. */
export function DemoSendButton({
  label,
  disabled,
  onClick,
}: {
  label: string;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
      className="inline-flex size-7 items-center justify-center rounded-full bg-foreground text-background transition-opacity outline-none hover:opacity-85 focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-40"
    >
      <ArrowUp className="size-4" />
    </button>
  );
}
