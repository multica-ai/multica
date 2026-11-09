"use client";

import { useEffect, useRef, useState, useSyncExternalStore, type ComponentType, type ReactNode } from "react";
import {
  Bot,
  ChevronDown,
  ChevronRight,
  CircleHelp,
  Filter,
  Inbox,
  ListTodo,
  MessageSquare,
  Search,
  Settings,
  SquarePen,
  Users,
  Wifi,
  Zap,
  type IconComponent,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { ShimmerText } from "@multica/ui/components/common/shimmer-text";
import { HERO_PROVIDERS } from "../../components/provider-marks";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";

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

/** The app's top-level spaces. Settings sits in the rail's footer, not its list. */
export type Space = "inbox" | "work" | "team" | "chat" | "autopilots" | "settings";

const RAIL_SPACES: { key: Space; label: string; icon: IconComponent }[] = [
  { key: "inbox", label: "Inbox", icon: Inbox },
  { key: "work", label: "Work", icon: ListTodo },
  { key: "team", label: "Team", icon: Users },
  { key: "chat", label: "Chat", icon: MessageSquare },
  { key: "autopilots", label: "Auto", icon: Zap },
];

const RAIL_ACTIVE = "bg-sidebar-accent font-medium text-sidebar-accent-foreground";

function RailButton({ icon: Icon, active }: { icon: IconComponent; active?: boolean }) {
  return (
    <span
      className={cn(
        "flex size-9 shrink-0 items-center justify-center rounded-lg",
        active ? RAIL_ACTIVE : "text-muted-foreground",
      )}
    >
      <Icon className="size-4.5" />
    </span>
  );
}

/**
 * The app rail on the grey shell: the workspace, New issue and search, the
 * five spaces with their labels, then help, settings and the account. Desktop
 * moves New issue and search to its title bar and leaves room for the window
 * controls at the top.
 */
export function AppRail({ active, inbox = 3, desktop }: { active: Space; inbox?: number; desktop?: boolean }) {
  return (
    <div className={cn("flex w-16 shrink-0 flex-col items-center pb-2.5", desktop ? "pt-10" : "pt-3")}>
      <span className="flex size-[34px] shrink-0 items-center justify-center rounded-lg border border-surface-border bg-surface-raised text-body font-semibold shadow-[var(--surface-shadow)]">
        A
      </span>
      {desktop ? (
        <span className="h-2 shrink-0" />
      ) : (
        <span className="my-2 flex flex-col items-center gap-1">
          <RailButton icon={SquarePen} />
          <RailButton icon={Search} />
        </span>
      )}
      <span className="flex flex-col items-center gap-0.5">
        {RAIL_SPACES.map((space) => {
          const current = space.key === active;
          return (
            <span
              key={space.key}
              className={cn(
                "relative flex size-[52px] shrink-0 flex-col items-center justify-center gap-1 rounded-xl",
                current ? RAIL_ACTIVE : "text-muted-foreground",
              )}
            >
              <space.icon className="size-5" />
              <span className="text-micro leading-[14px]">{space.label}</span>
              {space.key === "inbox" && inbox > 0 ? (
                <span
                  className={cn(
                    "absolute top-0.5 right-1.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-brand px-1 text-[10px] leading-none font-medium tabular-nums text-brand-foreground ring-2",
                    current ? "ring-sidebar-accent" : "ring-app-shell",
                  )}
                >
                  {inbox}
                </span>
              ) : null}
            </span>
          );
        })}
      </span>
      <span className="mt-auto flex flex-col items-center gap-1 pt-2">
        <RailButton icon={CircleHelp} />
        <RailButton icon={Settings} active={active === "settings"} />
        <span className="flex size-9 items-center justify-center">
          <Avatar actor={MIA} size={24} />
        </span>
      </span>
    </div>
  );
}

/** The content card beside the rail: the space panel and the page share one surface. */
export const APP_CARD =
  "relative flex min-w-0 flex-1 overflow-hidden rounded-xl bg-page-canvas shadow-[var(--surface-shadow)] ring-1 ring-surface-border";

/**
 * The Multica app as a window: the rail on the grey app shell, and the page
 * on the content card with the space panel collapsed. A window that runs off
 * the right and bottom of its frame passes `bleed` (in px), and the page keeps
 * its content out of the cropped strip so nothing is cut mid-word.
 */
export function AppWindow({
  className,
  active,
  bleed = 0,
  children,
}: {
  className: string;
  active: Space;
  bleed?: number;
  children: ReactNode;
}) {
  return (
    <div
      className={cn("absolute flex overflow-hidden rounded-xl border border-site-line-strong bg-app-shell shadow-site-float", className)}
    >
      <AppRail active={active} />
      <div className={cn(APP_CARD, "my-2 mr-2")}>
        <div className="relative flex min-w-0 flex-1 flex-col" style={{ marginRight: bleed, marginBottom: bleed }}>
          {children}
        </div>
      </div>
    </div>
  );
}

/** A ghost icon button from a page header. */
export function HeaderButton({ icon: Icon }: { icon: IconComponent }) {
  return (
    <span className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground">
      <Icon className="size-4.5" />
    </span>
  );
}

/**
 * A collection page's header: the current view as a menu (the header never
 * carries a count), the filter, and the page's actions on the right.
 */
export function PageHeader({
  icon: Icon,
  title,
  filter = true,
  children,
}: {
  icon: IconComponent;
  title: ReactNode;
  filter?: boolean;
  children?: ReactNode;
}) {
  return (
    <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4">
      <div className="flex min-w-0 flex-1 items-center gap-1">
        <span className="flex h-7 min-w-0 items-center gap-2 px-2 text-body font-medium">
          <Icon className="size-4 shrink-0 text-muted-foreground" />
          <span className="min-w-0 truncate">{title}</span>
          <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
        </span>
        {filter ? (
          <span className="flex size-7 shrink-0 items-center justify-center text-muted-foreground">
            <Filter className="size-4" />
          </span>
        ) : null}
      </div>
      {children ? <div className="flex shrink-0 items-center gap-1">{children}</div> : null}
    </div>
  );
}

/** An object page's header: where it lives, then the object itself. */
export function BreadcrumbHeader({ parent, leaf, children }: { parent: ReactNode; leaf: ReactNode; children?: ReactNode }) {
  return (
    <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4 text-body">
      <div className="flex min-w-0 flex-1 items-center gap-1.5">
        <span className="flex shrink-0 items-center gap-1 text-muted-foreground">{parent}</span>
        <ChevronRight className="size-3 shrink-0 text-faint-foreground" />
        <span className="min-w-0 truncate font-medium">{leaf}</span>
      </div>
      {children ? <div className="flex shrink-0 items-center gap-1">{children}</div> : null}
    </div>
  );
}

/*
 * A collection list from the product: optional column heads, group bars, and
 * borderless rows on one grid. Wrap the pieces in `LIST` and pass every row
 * the same `columns` (a grid-template-columns value).
 */
export const LIST = "flex shrink-0 flex-col px-2";

export function ListHead({ columns, children }: { columns: string; children: ReactNode }) {
  return (
    <div
      className="grid h-9 shrink-0 items-center gap-x-3 px-3 text-caption whitespace-nowrap text-muted-foreground"
      style={{ gridTemplateColumns: columns }}
    >
      {children}
    </div>
  );
}

export function ListGroup({ title, count }: { title: string; count: number }) {
  return (
    <div className="shrink-0 pt-2 pb-0.5">
      <div className="flex h-10 items-center gap-2 rounded-lg bg-muted px-3 text-caption font-semibold">
        {title}
        <span className="font-normal tabular-nums text-muted-foreground">{count}</span>
      </div>
    </div>
  );
}

export function ListRow({ columns, className, children }: { columns: string; className?: string; children: ReactNode }) {
  return (
    <div
      className={cn("grid h-9 shrink-0 items-center gap-x-3 rounded-md px-3 text-body whitespace-nowrap", className)}
      style={{ gridTemplateColumns: columns }}
    >
      {children}
    </div>
  );
}

/** A list's primary cell: icon, name, an inline tag, then the description in whatever width is left. */
export function NameCell({
  icon,
  title,
  tag,
  description,
  muted,
}: {
  icon?: ReactNode;
  title: ReactNode;
  tag?: string;
  description?: ReactNode;
  muted?: boolean;
}) {
  return (
    <span className="flex min-w-0 items-center gap-2">
      {icon}
      <span className={cn("min-w-0 truncate font-medium", muted && "text-muted-foreground")}>{title}</span>
      {tag ? (
        <span className="shrink-0 rounded-xs bg-muted px-1 text-micro leading-4 font-medium text-muted-foreground">{tag}</span>
      ) : null}
      {description ? <span className="min-w-0 flex-1 basis-0 truncate text-muted-foreground">{description}</span> : null}
    </span>
  );
}

/** Every cell after the name. */
export const LIST_CELL = "flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground";

/** The coding tools found on a machine, as the runtimes list stacks them. */
export function ProviderStack({ names }: { names: string[] }) {
  return (
    <span className="flex min-w-0 items-center -space-x-1">
      {names.map((name) => {
        const Mark = PROVIDER_MARKS.get(name);
        return (
          <span key={name} className="inline-flex size-5 shrink-0 items-center justify-center rounded-xs bg-background ring-1 ring-border">
            {Mark ? <Mark className="size-3.5" /> : null}
          </span>
        );
      })}
    </span>
  );
}

export function Online() {
  return (
    <>
      <Wifi className="size-3 shrink-0 text-success" />
      <span className="truncate text-foreground">Online</span>
    </>
  );
}

/** Overlapping avatars, each ringed in the surface it sits on. */
export function AvatarStack({ actors, size = 20, ring = "ring-background" }: { actors: Actor[]; size?: 16 | 20; ring?: string }) {
  return (
    <span className="flex shrink-0 items-center -space-x-1.5">
      {actors.map((actor) => (
        <span key={actor.name} className={cn("inline-flex rounded-full ring-2", ring)}>
          <Avatar actor={actor} size={size} />
        </span>
      ))}
    </span>
  );
}

/** An issue's live agent badge: who is on it, and a shimmering "Working". */
export function Working({ label = "Working", agents }: { label?: string; agents?: Actor[] }) {
  return (
    <span className="inline-flex shrink-0 items-center gap-1">
      {agents ? <AvatarStack actors={agents} size={16} /> : null}
      <MulticaIcon mode="think" size={12} />
      <ShimmerText className="text-micro text-muted-foreground">{label}</ShimmerText>
    </span>
  );
}

/** The page header's "agents working" chip; compact keeps the faces and the number. */
export function WorkingChip({ agents, compact }: { agents: Actor[]; compact?: boolean }) {
  return (
    <span className="flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-brand/28 bg-brand/7 px-2.5 text-caption font-medium dark:border-brand/45 dark:bg-brand/12">
      <AvatarStack actors={agents.slice(0, 3)} ring="ring-page-canvas" />
      <span className="tabular-nums">{compact ? agents.length : `${agents.length} agents working`}</span>
    </span>
  );
}
