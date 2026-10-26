"use client";

import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  BookOpenText,
  Bot,
  Check,
  ChevronDown,
  ChevronRight,
  CircleUser,
  Columns3,
  FolderKanban,
  Inbox,
  Layers,
  ListFilter,
  ListTodo,
  Maximize2,
  MessageSquare,
  Monitor,
  MoreHorizontal,
  Plus,
  Search,
  SlidersHorizontal,
  SquarePen,
  Users,
  X,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { ShimmerText } from "@multica/ui/components/common/shimmer-text";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import { PriorityIcon } from "@multica/views/issues/components/priority-icon";
import { Avatar, usePrefersReducedMotion, type Actor } from "./mock-kit";

/*
 * Hero product mockup: the issue board, built from the product's own tokens
 * and glyphs at a fixed design width, then zoomed to fit its frame; the
 * height follows the frame so the app's inset panel always closes. Data is
 * fictional. It loops one workflow's story — a focus issue moving across the
 * board while its side panel fills with activity — and calls `onLoop` each
 * time the story comes back to the start.
 */

const WIDE_WIDTH = 1360;
const NARROW_WIDTH = 540;
const NARROW_BELOW = 600;
const COLUMN_STRIDE = 232 + 12;

export type Priority = "none" | "urgent" | "high" | "medium" | "low";
export type StatusId = "backlog" | "todo" | "in_progress" | "in_review" | "done";
export type Card = {
  id: string;
  title: string;
  priority: Priority;
  assignee?: Actor;
  working?: boolean;
  updated: string;
};
export type Column = { status: StatusId; label: string; total: number; cards: Card[] };

/** One beat of the story: where the focus issue is, what the panel shows, and how long to hold. */
export type Stage = {
  column: StatusId;
  assignee?: Actor;
  priority?: Priority;
  working: boolean;
  panel: boolean;
  entries: number;
  hold: number;
};

export type Workflow = {
  id: string;
  label: string;
  description: string;
  columns: Column[];
  focus: Card;
  stages: Stage[];
  /** Stage shown when motion is reduced. */
  still: number;
  Activity: (props: { stage: Stage }) => ReactNode;
};

const MOVE_EASE = "cubic-bezier(0.32, 0.72, 0, 1)";

export function HeroBoard({ workflow, onLoop }: { workflow: Workflow; onLoop?: () => void }) {
  const windowRef = useRef<HTMLDivElement>(null);
  const boardRef = useRef<HTMLDivElement>(null);
  const positions = useRef<{ key: string; at: Map<string, { x: number; y: number }> } | null>(null);
  const [fit, setFit] = useState<{ zoom: number; narrow: boolean; height: number } | null>(null);
  const [visible, setVisible] = useState(false);
  const [shown, setShown] = useState({ id: workflow.id, step: 0 });
  const reducedMotion = usePrefersReducedMotion();

  // A new workflow restarts its story from the first beat.
  const step = shown.id === workflow.id ? shown.step : 0;
  if (shown.id !== workflow.id) setShown({ id: workflow.id, step: 0 });
  const stage = workflow.stages[reducedMotion ? workflow.still : step]!;

  const onLoopRef = useRef(onLoop);
  useLayoutEffect(() => {
    onLoopRef.current = onLoop;
  });

  useEffect(() => {
    const el = windowRef.current;
    if (!el) return;
    const resize = new ResizeObserver(() => {
      const narrow = el.clientWidth < NARROW_BELOW;
      const zoom = el.clientWidth / (narrow ? NARROW_WIDTH : WIDE_WIDTH);
      setFit({ zoom, narrow, height: el.clientHeight / zoom });
    });
    const intersection = new IntersectionObserver(([entry]) => setVisible(entry?.isIntersecting ?? false));
    resize.observe(el);
    intersection.observe(el);
    return () => {
      resize.disconnect();
      intersection.disconnect();
    };
  }, []);

  useEffect(() => {
    if (!visible || reducedMotion) return;
    const timer = setTimeout(() => {
      const next = (step + 1) % workflow.stages.length;
      if (next === 0) onLoopRef.current?.();
      setShown({ id: workflow.id, step: next });
    }, stage.hold);
    return () => clearTimeout(timer);
  }, [step, stage.hold, visible, reducedMotion, workflow]);

  // FLIP: cards that changed place glide from where they were. Positions are
  // relative to the board, so panning it on narrow screens is not a move.
  useLayoutEffect(() => {
    const board = boardRef.current;
    if (!board || !fit) return;
    const key = `${workflow.id}@${fit.zoom}`;
    const origin = board.getBoundingClientRect();
    const next = new Map<string, { x: number; y: number }>();
    const cards = board.querySelectorAll<HTMLElement>("[data-card]");
    for (const el of cards) {
      const rect = el.getBoundingClientRect();
      next.set(el.dataset.card!, { x: (rect.left - origin.left) / fit.zoom, y: (rect.top - origin.top) / fit.zoom });
    }
    const previous = positions.current;
    if (previous?.key === key && !reducedMotion) {
      for (const el of cards) {
        const id = el.dataset.card!;
        const from = previous.at.get(id);
        const to = next.get(id);
        if (!from || !to) continue;
        if (id === workflow.focus.id && step === 0) {
          el.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 400, easing: "ease-out" });
          continue;
        }
        const dx = from.x - to.x;
        const dy = from.y - to.y;
        if (Math.abs(dx) < 0.5 && Math.abs(dy) < 0.5) continue;
        el.animate([{ transform: `translate(${dx}px, ${dy}px)` }, { transform: "none" }], {
          duration: id === workflow.focus.id ? 650 : 450,
          easing: MOVE_EASE,
        });
      }
    }
    positions.current = { key, at: next };
  }, [step, fit, reducedMotion, workflow]);

  const narrow = fit?.narrow ?? false;
  const columns = workflow.columns;
  const focus: Card = {
    ...workflow.focus,
    priority: stage.priority ?? workflow.focus.priority,
    assignee: stage.assignee,
    working: stage.working,
  };
  const baseWorking = columns.flatMap((c) => c.cards).filter((card) => card.working).length;
  const columnIndex = columns.findIndex((c) => c.status === stage.column);
  const pan = narrow ? Math.min(Math.max(columnIndex - 1, 0), columns.length - 2) * COLUMN_STRIDE : 0;

  return (
    <div
      ref={windowRef}
      role="img"
      aria-label={workflow.description}
      className="absolute inset-0 overflow-hidden rounded-[12px] border border-site-line-strong bg-app-shell shadow-site-float"
    >
      <div
        aria-hidden
        className={cn("flex text-foreground", !fit && "opacity-0")}
        style={{ width: narrow ? NARROW_WIDTH : WIDE_WIDTH, height: fit?.height, zoom: fit?.zoom }}
      >
        {narrow ? null : <Sidebar />}
        <main
          className={cn(
            "relative flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-surface-border bg-background",
            narrow ? "m-1.5" : "my-1.5 mr-1.5",
          )}
        >
          <div className="flex h-11 shrink-0 items-center gap-2 border-b border-border px-4">
            <ListTodo className="size-4 text-muted-foreground" />
            <span className="text-label font-medium">Issues</span>
          </div>
          <Toolbar working={baseWorking + (stage.working ? 1 : 0)} compact={narrow} />
          <div
            key={`board-${workflow.id}`}
            className="min-h-0 flex-1 overflow-hidden animate-in fade-in duration-500 motion-reduce:animate-none"
          >
            <div
              ref={boardRef}
              className="flex items-start gap-3 px-3 pb-3 transition-transform duration-700 ease-in-out motion-reduce:transition-none"
              style={{ transform: pan ? `translateX(${-pan}px)` : undefined }}
            >
              {columns.map((column) => {
                const holdsFocus = column.status === stage.column;
                return (
                  <BoardColumn
                    key={column.status}
                    column={column}
                    total={column.total + (holdsFocus ? 1 : 0)}
                    cards={holdsFocus ? [focus, ...column.cards] : column.cards}
                    peeked={stage.panel && !narrow ? focus.id : undefined}
                  />
                );
              })}
            </div>
          </div>
          {narrow ? null : <IssuePanel key={`panel-${workflow.id}`} workflow={workflow} focus={focus} stage={stage} />}
        </main>
      </div>
    </div>
  );
}

function Sidebar() {
  return (
    <aside className="flex w-[200px] shrink-0 flex-col px-2 py-2.5">
      <div className="flex h-8 items-center gap-2 px-2">
        <span className="flex size-5 items-center justify-center rounded-md bg-surface text-micro font-semibold shadow-[var(--surface-shadow)]">
          A
        </span>
        <span className="flex-1 text-label font-semibold">Acme Labs</span>
        <ChevronDown className="size-3.5 text-muted-foreground" />
      </div>
      <div className="mt-2 flex flex-col">
        <NavRow icon={Search} label="Search…" hint="⌘ K" />
        <NavRow icon={SquarePen} label="New Issue" hint="C" />
      </div>
      <div className="mt-4 flex flex-col">
        <NavRow icon={Inbox} label="Inbox" badge="2" />
        <NavRow icon={CircleUser} label="My Issues" />
        <NavRow icon={MessageSquare} label="Chat" />
      </div>
      <NavGroup label="Work">
        <NavRow icon={ListTodo} label="Issues" active />
        <NavRow icon={FolderKanban} label="Projects" />
        <NavRow icon={Zap} label="Autopilot" />
      </NavGroup>
      <NavGroup label="AI Team">
        <NavRow icon={Bot} label="Agents" />
        <NavRow icon={Users} label="Squads" />
        <NavRow icon={BookOpenText} label="Skills" />
        <NavRow icon={Monitor} label="Runtimes" />
      </NavGroup>
    </aside>
  );
}

function NavGroup({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="mt-4 flex flex-col">
      <p className="px-2 pb-1 text-caption text-muted-foreground">{label}</p>
      {children}
    </div>
  );
}

function NavRow({
  icon: Icon,
  label,
  hint,
  badge,
  active,
}: {
  icon: LucideIcon;
  label: string;
  hint?: string;
  badge?: string;
  active?: boolean;
}) {
  return (
    <div className={cn("flex h-7 items-center gap-2 rounded-md px-2 text-label", active && "bg-surface-selected font-medium")}>
      <Icon className="size-4 text-muted-foreground" />
      <span className="flex-1">{label}</span>
      {hint ? <span className="text-micro text-muted-foreground">{hint}</span> : null}
      {badge ? (
        <span className="rounded-full bg-surface px-1.5 text-micro font-medium tabular-nums text-muted-foreground shadow-[var(--surface-shadow)]">
          {badge}
        </span>
      ) : null}
    </div>
  );
}

function Toolbar({ working, compact }: { working: number; compact: boolean }) {
  return (
    <div className="flex h-12 shrink-0 items-center justify-between px-3">
      <div className="flex items-center gap-1">
        <span className="flex h-7 items-center rounded-md border border-border bg-muted px-2.5 text-caption font-medium">All</span>
        <span className="flex h-7 items-center rounded-md px-2.5 text-caption text-muted-foreground">Members</span>
        <span className="flex h-7 items-center rounded-md px-2.5 text-caption text-muted-foreground">Agents</span>
        {compact ? null : <Layers className="ml-1 size-3.5 text-muted-foreground" />}
      </div>
      <div className="flex items-center gap-1.5">
        <ToolbarButton>
          <span className="size-1.5 rounded-full bg-site-agent" />
          <span className="tabular-nums">{working}</span> agents working
        </ToolbarButton>
        {compact ? null : (
          <>
            <ToolbarButton icon={ListFilter}>Filter</ToolbarButton>
            <ToolbarButton icon={SlidersHorizontal}>Display</ToolbarButton>
            <ToolbarButton icon={Columns3}>Board</ToolbarButton>
          </>
        )}
      </div>
    </div>
  );
}

function ToolbarButton({ icon: Icon, children }: { icon?: LucideIcon; children: ReactNode }) {
  return (
    <span className="flex h-7 items-center gap-1.5 rounded-md border border-border bg-background px-2.5 text-caption">
      {Icon ? <Icon className="size-3.5 text-muted-foreground" /> : null}
      {children}
    </span>
  );
}

function BoardColumn({
  column,
  total,
  cards,
  peeked,
}: {
  column: Column;
  total: number;
  cards: Card[];
  peeked?: string;
}) {
  return (
    <section className="flex w-[232px] shrink-0 flex-col rounded-xl bg-muted/40 p-2">
      <div className="mb-2 flex items-center justify-between gap-2 px-1.5">
        <div className="flex items-center gap-2">
          <span className="inline-flex items-center gap-1.5 text-caption font-semibold">
            <StatusIcon status={column.status} className="h-3 w-3" />
            {column.label}
          </span>
          <span className="text-caption text-muted-foreground tabular-nums">{total}</span>
        </div>
        <div className="flex items-center gap-2 text-muted-foreground">
          <MoreHorizontal className="size-3.5" />
          <Plus className="size-3.5" />
        </div>
      </div>
      <div className="flex flex-col gap-2 p-1">
        {cards.map((card) => (
          <BoardCard key={card.id} card={card} peeked={card.id === peeked} />
        ))}
      </div>
    </section>
  );
}

function BoardCard({ card, peeked }: { card: Card; peeked: boolean }) {
  return (
    <div
      data-card={card.id}
      className={cn(
        "rounded-lg border-[0.5px] border-surface-border bg-surface px-2.5 py-3 shadow-[var(--surface-shadow)] transition-shadow duration-300",
        peeked && "ring-2 ring-brand/50",
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-1.5">
          <span key={card.priority} className="inline-flex size-5 shrink-0 items-center justify-center animate-in fade-in duration-300 motion-reduce:animate-none">
            <PriorityIcon priority={card.priority} />
          </span>
          <p className="truncate text-caption text-muted-foreground">{card.id}</p>
        </div>
        {card.working && card.assignee ? <WorkingBadge actor={card.assignee} /> : null}
      </div>
      <p className="mt-1 line-clamp-2 text-body leading-snug font-medium">{card.title}</p>
      <div className="mt-2 flex items-center justify-between gap-2">
        {card.assignee ? (
          <span key={card.assignee.name} className="flex min-w-0 items-center gap-1.5 animate-in fade-in duration-300 motion-reduce:animate-none">
            <Avatar actor={card.assignee} size={20} />
            <span className="truncate text-caption">{card.assignee.name}</span>
          </span>
        ) : (
          <span className="text-caption text-muted-foreground">Unassigned</span>
        )}
        <span className="shrink-0 text-caption text-muted-foreground">{card.updated}</span>
      </div>
    </div>
  );
}

function WorkingBadge({ actor }: { actor: Actor }) {
  return (
    <span className="inline-flex shrink-0 items-center gap-1 animate-in fade-in duration-300 motion-reduce:animate-none">
      <Avatar actor={actor} size={16} />
      <span className="size-1.5 rounded-full bg-site-agent" />
      <ShimmerText className="text-micro text-muted-foreground">Working</ShimmerText>
    </span>
  );
}

const STATUS_LABEL: Record<StatusId, string> = {
  backlog: "Backlog",
  todo: "Todo",
  in_progress: "In Progress",
  in_review: "In Review",
  done: "Done",
};

const PRIORITY_LABEL: Record<Priority, string> = {
  none: "No priority",
  urgent: "Urgent",
  high: "High",
  medium: "Medium",
  low: "Low",
};

function IssuePanel({ workflow, focus, stage }: { workflow: Workflow; focus: Card; stage: Stage }) {
  const { Activity } = workflow;
  return (
    <aside
      data-open={stage.panel}
      className={cn(
        "absolute inset-y-0 right-0 z-10 flex w-[380px] flex-col border-l border-border bg-background shadow-[var(--floating-shadow)]",
        "transition-[translate,box-shadow] duration-500 ease-[cubic-bezier(0.32,0.72,0,1)] motion-reduce:transition-none",
        "data-[open=false]:translate-x-full data-[open=false]:shadow-none data-[open=false]:duration-300 data-[open=false]:ease-in",
      )}
    >
      <div className="flex h-11 shrink-0 items-center justify-between border-b border-border px-4">
        <span className="flex items-center gap-1 text-caption text-muted-foreground">
          Acme Labs <ChevronRight className="size-3" /> <span className="text-foreground">{focus.id}</span>
        </span>
        <span className="flex items-center gap-3 text-muted-foreground">
          <Maximize2 className="size-3.5" />
          <X className="size-3.5" />
        </span>
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-5 overflow-hidden px-5 py-4">
        <p className="text-title font-semibold leading-snug">{focus.title}</p>
        <dl className="grid grid-cols-[88px_1fr] items-center gap-y-2.5 text-caption">
          <dt className="text-muted-foreground">Status</dt>
          <dd key={stage.column} className="flex items-center gap-1.5 animate-in fade-in duration-300 motion-reduce:animate-none">
            <StatusIcon status={stage.column} className="h-3.5 w-3.5" />
            {STATUS_LABEL[stage.column]}
          </dd>
          <dt className="text-muted-foreground">Priority</dt>
          <dd key={focus.priority} className="flex items-center gap-1.5 animate-in fade-in duration-300 motion-reduce:animate-none">
            <PriorityIcon priority={focus.priority} />
            {PRIORITY_LABEL[focus.priority]}
          </dd>
          <dt className="text-muted-foreground">Assignee</dt>
          {focus.assignee ? (
            <dd key={focus.assignee.name} className="flex items-center gap-1.5 animate-in fade-in duration-300 motion-reduce:animate-none">
              <Avatar actor={focus.assignee} size={16} />
              {focus.assignee.name}
            </dd>
          ) : (
            <dd className="text-muted-foreground">Unassigned</dd>
          )}
        </dl>
        <div className="flex flex-col gap-3 border-t border-border pt-4">
          <p className="text-caption font-medium text-muted-foreground">Activity</p>
          <Activity stage={stage} />
        </div>
      </div>
    </aside>
  );
}

export function Entry({ children }: { children: ReactNode }) {
  return <div className="animate-in fade-in slide-in-from-bottom-1 duration-300 motion-reduce:animate-none">{children}</div>;
}

export function ActivityRow({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  return (
    <p className="flex items-center gap-2 text-caption text-muted-foreground">
      <span className="flex size-4 items-center justify-center">{icon}</span>
      <span>{children}</span>
      <span className="ml-auto">just now</span>
    </p>
  );
}

export type RunStep = { icon: LucideIcon; text: ReactNode };

/** An agent run whose steps appear one per entry from `at`; it finishes once all are shown. */
export function RunCard({
  actor,
  runtime,
  steps,
  at,
  entries,
  duration,
}: {
  actor: Actor;
  runtime: string;
  steps: RunStep[];
  at: number;
  entries: number;
  duration: string;
}) {
  const shown = Math.min(entries - at + 1, steps.length);
  const running = entries < at + steps.length;
  return (
    <div className="rounded-lg border border-border bg-muted/40 p-3">
      <div className="flex items-center justify-between text-caption">
        <span className="flex items-center gap-1.5 font-medium">
          <Avatar actor={actor} size={16} />
          Run · {runtime}
        </span>
        {running ? (
          <span className="flex items-center gap-1.5">
            <span className="size-1.5 rounded-full bg-site-agent" />
            <ShimmerText className="text-micro text-muted-foreground">Working</ShimmerText>
          </span>
        ) : (
          <span className="flex items-center gap-1 text-micro text-muted-foreground">
            <Check className="size-3" /> Finished in {duration}
          </span>
        )}
      </div>
      <ul className="mt-2.5 flex flex-col gap-1.5 font-mono text-micro">
        {steps.slice(0, shown).map((runStep, index) => (
          <li key={index} className="flex items-center gap-2 animate-in fade-in duration-300 motion-reduce:animate-none">
            <runStep.icon className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{runStep.text}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function Comment({ actor, children }: { actor: Actor; children: ReactNode }) {
  return (
    <div className="rounded-lg border border-surface-border bg-surface p-3 text-caption shadow-[var(--surface-shadow)]">
      <div className="mb-1.5 flex items-center gap-1.5">
        <Avatar actor={actor} size={16} />
        <span className="font-medium">{actor.name}</span>
        <span className="text-muted-foreground">just now</span>
      </div>
      {children}
    </div>
  );
}
