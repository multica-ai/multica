"use client";

import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  ArrowUp,
  Bot,
  Calendar,
  ChevronDown,
  ChevronRight,
  CircleUser,
  Columns3,
  CornerDownRight,
  Filter,
  Folder,
  ListTodo,
  MoreHorizontal,
  PanelRight,
  Paperclip,
  Plus,
  SquarePen,
  X,
  type IconComponent,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { ShimmerText } from "@multica/ui/components/common/shimmer-text";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import { PriorityIcon } from "@multica/views/issues/components/priority-icon";
import {
  APP_CARD,
  AppRail,
  Avatar,
  AvatarStack,
  HeaderButton,
  Working,
  WorkingChip,
  usePrefersReducedMotion,
  type Actor,
} from "./mock-kit";

/*
 * Hero product mockup: a project board run by a workflow, built from the
 * product's own tokens and glyphs at a fixed design width, then zoomed to fit
 * its frame. Every column is one of the workflow's custom statuses and names
 * the agent or member it hands off to. The story follows one issue through
 * the workflow: it moves column to column while the issue peek floats over
 * the board and fills with handoffs and comments, and the board pans so the
 * issue is never under the peek. Data is fictional.
 */

const WIDE_WIDTH = 1440;
const NARROW_WIDTH = 540;
const NARROW_BELOW = 600;
const RAIL_WIDTH = 64;
const PANEL_WIDTH = 240;
const CARD_MARGIN = 8;
const COLUMN_WIDTH = 280;
const COLUMN_GAP = 16;
const BOARD_PAD = 8;
const PEEK_WIDTH = 520;
const PEEK_INSET = 8;

export type Priority = "none" | "urgent" | "high" | "medium" | "low";
type Category = "unstarted" | "started" | "done";
type StatusGlyph = "dotted" | "circle" | "half" | "three_quarters" | "check";

export type Status = {
  key: string;
  name: string;
  category: Category;
  icon: StatusGlyph;
  color: string;
  handler?: Actor;
};

export type Card = {
  id: string;
  title: string;
  priority: Priority;
  assignee?: Actor;
  working?: boolean;
  updated: string;
};

export type TimelineItem =
  | { kind: "created"; by: string; icon: ReactNode; handoff?: Actor; when?: string }
  | { kind: "status"; actor: Actor; from: string; to: string; handoff?: Actor; when?: string }
  | { kind: "comment"; actor: Actor; body: ReactNode };

/** One beat of the story: where the issue is, how much of its timeline shows, and how long to hold. */
export type Stage = {
  status: string;
  panel: boolean;
  entries: number;
  working: boolean;
  priority?: Priority;
  hold: number;
};

export type Workflow = {
  id: string;
  label: string;
  description: string;
  project: string;
  statuses: Status[];
  cards: Record<string, { total: number; cards: Card[] }>;
  focus: { id: string; title: string; priority: Priority; description: string; label: string; due: string };
  timeline: TimelineItem[];
  stages: Stage[];
  /** Stage shown when motion is reduced. */
  still: number;
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
  // relative to the board, so panning it is not a move.
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
          duration: id === workflow.focus.id ? 700 : 450,
          easing: MOVE_EASE,
        });
      }
    }
    positions.current = { key, at: next };
  }, [step, fit, reducedMotion, workflow]);

  const narrow = fit?.narrow ?? false;
  const statuses = workflow.statuses;
  const statusIndex = statuses.findIndex((s) => s.key === stage.status);
  const assignee = assigneeAt(workflow, statusIndex);
  const focus: Card = {
    id: workflow.focus.id,
    title: workflow.focus.title,
    priority: stage.priority ?? workflow.focus.priority,
    assignee,
    working: stage.working && assignee?.kind === "agent",
    updated: "just now",
  };
  const peekOpen = stage.panel && !narrow;
  const working = [
    ...new Set(
      [...(focus.working ? [focus] : []), ...Object.values(workflow.cards).flatMap((column) => column.cards)]
        .filter((card) => card.working && card.assignee)
        .map((card) => card.assignee!),
    ),
  ];

  // Pan so the focus column is fully in view: right of the panel, left of
  // the peek when it is open.
  const canvasWidth = narrow ? NARROW_WIDTH : WIDE_WIDTH;
  const mainWidth = canvasWidth - (narrow ? 12 : RAIL_WIDTH + CARD_MARGIN + PANEL_WIDTH);
  const viewport = mainWidth - (peekOpen ? PEEK_WIDTH + PEEK_INSET * 2 : 0);
  const stride = COLUMN_WIDTH + COLUMN_GAP;
  const boardWidth = statuses.length * stride - COLUMN_GAP + BOARD_PAD * 2;
  const pan = Math.min(
    Math.max(0, (statusIndex + 1) * stride - COLUMN_GAP + BOARD_PAD - viewport),
    Math.max(0, boardWidth - viewport),
  );

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
        style={{ width: canvasWidth, height: fit?.height, zoom: fit?.zoom }}
      >
        {narrow ? null : <AppRail active="work" />}
        <div className={cn(APP_CARD, narrow ? "m-1.5" : "my-2 mr-2")}>
          {narrow ? null : <WorkPanel workflow={workflow} focus={focus} status={stage.status} peeked={peekOpen} />}
          <main className="relative flex min-w-0 flex-1 flex-col">
          <BoardHeader project={workflow.project} working={working} compact={narrow} />
          <div
            key={`board-${workflow.id}`}
            className="min-h-0 flex-1 overflow-hidden animate-in fade-in duration-500 motion-reduce:animate-none"
          >
            <div
              ref={boardRef}
              className="flex items-start gap-4 p-2 transition-transform duration-700 ease-[cubic-bezier(0.32,0.72,0,1)] motion-reduce:transition-none"
              style={{ width: boardWidth, transform: pan ? `translateX(${-pan}px)` : undefined }}
            >
              {statuses.map((status) => {
                const column = workflow.cards[status.key] ?? { total: 0, cards: [] };
                const holdsFocus = status.key === stage.status;
                return (
                  <BoardColumn
                    key={status.key}
                    status={status}
                    total={column.total + (holdsFocus ? 1 : 0)}
                    cards={holdsFocus ? [focus, ...column.cards] : column.cards}
                    peeked={peekOpen ? focus.id : undefined}
                  />
                );
              })}
            </div>
          </div>
          {narrow ? null : (
            <IssuePeek key={`peek-${workflow.id}`} workflow={workflow} focus={focus} stage={stage} open={peekOpen} />
          )}
          </main>
        </div>
      </div>
    </div>
  );
}

/** Who holds the issue at a status: that step's handler, else whoever held it before. */
function assigneeAt(workflow: Workflow, index: number): Actor | undefined {
  for (let i = index; i >= 0; i--) {
    const handler = workflow.statuses[i]?.handler;
    if (handler) return handler;
  }
  return undefined;
}

// --- The Work panel -----------------------------------------------------------

const PROJECTS = [
  { name: "API Platform", open: 12 },
  { name: "Autopilots launch", open: 9 },
  { name: "Customer feedback", open: 7 },
  { name: "Mobile app", open: 5 },
];

const PANEL_ROW = "flex min-h-8 items-center gap-2.5 rounded-lg px-3 py-1.5 text-body";
const PANEL_ROW_ACTIVE = "bg-surface-selected text-surface-selected-foreground";
/** A working agent's issue title in the panel: dimmer at rest, with a wider highlight. */
const PANEL_SHIMMER = "[--shimmer-text-base:var(--faint-foreground)] [--shimmer-text-spread:22%]";
const TREE_SIZE = 5;

function PanelSection({ label, children }: { label: string; children: ReactNode }) {
  return (
    <section className="mt-5 first:mt-0">
      <div className="flex h-7 items-center gap-1 pr-1 pl-3">
        <h3 className="min-w-0 flex-1 truncate text-caption font-medium text-muted-foreground">{label}</h3>
        <span className="flex size-6 items-center justify-center text-muted-foreground">
          <Plus className="size-3.5" />
        </span>
      </div>
      <ul className="space-y-px">{children}</ul>
    </section>
  );
}

function PanelRow({ icon: Icon, label, count, active }: { icon?: IconComponent; label: string; count?: number; active?: boolean }) {
  return (
    <li className={cn(PANEL_ROW, active ? PANEL_ROW_ACTIVE : "text-muted-foreground")}>
      {Icon ? <Icon className="size-4 shrink-0" /> : <span className="size-4 shrink-0" />}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {count ? <span className="shrink-0 text-caption tabular-nums text-faint-foreground">{count}</span> : null}
    </li>
  );
}

/**
 * The Work space's panel: views, then projects with the open one expanded into
 * its issues. Titles shimmer while an agent runs on them, and the issue in the
 * peek is the lit row.
 */
function WorkPanel({ workflow, focus, status, peeked }: { workflow: Workflow; focus: Card; status: string; peeked: boolean }) {
  const byStatus = workflow.statuses.flatMap((s) => (workflow.cards[s.key]?.cards ?? []).map((card) => ({ card, status: s })));
  const others = [...byStatus.filter((row) => row.card.working), ...byStatus.filter((row) => !row.card.working)];
  const tree = [{ card: focus, status: workflow.statuses.find((s) => s.key === status)! }, ...others].slice(0, TREE_SIZE);
  return (
    <aside className="flex shrink-0 flex-col border-r border-border" style={{ width: PANEL_WIDTH }}>
      <div className="flex h-12 shrink-0 items-center pr-2 pl-5">
        <h2 className="text-title-sm font-semibold">Work</h2>
      </div>
      <div className="min-h-0 flex-1 overflow-hidden px-2 pb-4">
        <PanelSection label="Views">
          <PanelRow icon={CircleUser} label="My Issues" />
          <PanelRow icon={Bot} label="My agents’ work" />
          <PanelRow icon={ListTodo} label="All issues" />
          <PanelRow label="All views" count={3} />
        </PanelSection>
        <PanelSection label="Projects">
          {PROJECTS.map((project) => {
            const open = project.name === workflow.project;
            return (
              <li key={project.name}>
                <div className={cn(PANEL_ROW, open ? (peeked ? "text-foreground" : PANEL_ROW_ACTIVE) : "text-muted-foreground")}>
                  <Folder className="size-4 shrink-0" />
                  <span className="min-w-0 flex-1 truncate">{project.name}</span>
                  <span className="shrink-0 text-caption tabular-nums text-faint-foreground">{project.open}</span>
                </div>
                {open ? (
                  <ul key={workflow.id} className="mb-2 ml-5 border-l border-surface-border pl-1.5">
                    {tree.map((row) => (
                      <li
                        key={row.card.id}
                        className={cn(
                          "flex min-h-[26px] items-center gap-2 rounded-lg px-2 py-[3px] text-label",
                          peeked && row.card.id === focus.id ? PANEL_ROW_ACTIVE : "text-muted-foreground",
                        )}
                      >
                        <CustomStatusIcon key={row.status.key} status={row.status} className="size-3 shrink-0" />
                        <ShimmerText active={row.card.working} className={cn("min-w-0 flex-1 truncate", PANEL_SHIMMER)}>
                          {row.card.title}
                        </ShimmerText>
                        {row.card.working && row.card.assignee ? <AvatarStack actors={[row.card.assignee]} size={16} ring="ring-page-canvas" /> : null}
                      </li>
                    ))}
                  </ul>
                ) : null}
              </li>
            );
          })}
          <PanelRow label="All 8 projects" />
        </PanelSection>
      </div>
    </aside>
  );
}

// --- The board ----------------------------------------------------------------

/** The project page's header: the project as a menu and the filter, then the page's actions. */
function BoardHeader({ project, working, compact }: { project: string; working: Actor[]; compact: boolean }) {
  return (
    <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4">
      <div className="flex min-w-0 flex-1 items-center gap-1">
        <span className="flex h-7 min-w-0 items-center gap-2 px-2 text-body font-medium">
          <Folder className="size-4 shrink-0 text-muted-foreground" />
          <span key={project} className="min-w-0 truncate animate-in fade-in duration-300 motion-reduce:animate-none">
            {project}
          </span>
          <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
        </span>
        <span className="flex size-7 shrink-0 items-center justify-center text-muted-foreground">
          <Filter className="size-4" />
        </span>
      </div>
      <div className="flex shrink-0 items-center gap-1">
        {working.length > 0 ? <WorkingChip agents={working} compact={compact} /> : null}
        {compact ? null : <HeaderButton icon={Columns3} />}
        <HeaderButton icon={SquarePen} />
        {compact ? null : (
          <>
            <span className="mx-1 h-4 w-px bg-border" />
            <HeaderButton icon={PanelRight} />
          </>
        )}
      </div>
    </div>
  );
}

function CustomStatusIcon({ status, className }: { status: Status; className: string }) {
  return (
    <StatusIcon status={status.key} category={status.category} color={status.color} icon={status.icon} className={className} />
  );
}

function BoardColumn({
  status,
  total,
  cards,
  peeked,
}: {
  status: Status;
  total: number;
  cards: Card[];
  peeked?: string;
}) {
  return (
    <section className="flex shrink-0 flex-col rounded-xl bg-muted/40 p-2" style={{ width: COLUMN_WIDTH }}>
      <div className="mb-2 flex items-center justify-between gap-2 px-1.5">
        <div className="flex min-w-0 items-center gap-2">
          <span className="inline-flex items-center gap-1.5 truncate text-caption font-semibold">
            <CustomStatusIcon status={status} className="h-3 w-3 shrink-0" />
            {status.name}
          </span>
          <span className="text-caption text-muted-foreground tabular-nums">{total}</span>
        </div>
        <div className="flex min-w-0 items-center gap-1">
          {status.handler ? (
            <span className="inline-flex min-w-0 max-w-32 items-center gap-1 rounded-full border border-surface-border bg-background/70 py-0.5 pr-2 pl-0.5 text-caption">
              <Avatar actor={status.handler} size={16} />
              <span className="truncate">{status.handler.name}</span>
            </span>
          ) : null}
          <span className="flex size-6 shrink-0 items-center justify-center text-muted-foreground">
            <Plus className="size-3.5" />
          </span>
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
        {card.working && card.assignee ? (
          <span key={card.assignee.name} className="animate-in fade-in duration-300 motion-reduce:animate-none">
            <Working agents={[card.assignee]} />
          </span>
        ) : null}
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

const PRIORITY_LABEL: Record<Priority, string> = {
  none: "No priority",
  urgent: "Urgent",
  high: "High",
  medium: "Medium",
  low: "Low",
};

/** The product's issue peek: a floating panel over the board's right edge. */
function IssuePeek({ workflow, focus, stage, open }: { workflow: Workflow; focus: Card; stage: Stage; open: boolean }) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const status = workflow.statuses.find((s) => s.key === stage.status)!;
  const items = workflow.timeline.slice(0, stage.entries);

  // The newest entry stays in view, the way a reader following along would scroll.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTo({ top: el.scrollHeight, behavior: open ? "smooth" : "auto" });
  }, [stage.entries, open]);

  return (
    <aside
      data-open={open}
      className={cn(
        "absolute top-2 right-2 bottom-2 z-30 flex flex-col overflow-hidden rounded-xl bg-background shadow-[var(--floating-shadow)] ring-1 ring-surface-border",
        "transition-[opacity,translate] duration-300 ease-out motion-reduce:transition-none",
        "data-[open=false]:pointer-events-none data-[open=false]:translate-x-2 data-[open=false]:opacity-0 data-[open=false]:duration-200",
      )}
      style={{ width: PEEK_WIDTH }}
    >
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border pr-2 pl-4 text-body">
        <span className="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden">
          <span className="flex min-w-0 items-center gap-1 text-muted-foreground">
            <Folder className="size-3.5 shrink-0" />
            <span className="truncate">{workflow.project}</span>
          </span>
          <ChevronRight className="size-3 shrink-0 text-faint-foreground" />
          <span className="shrink-0 font-medium">{focus.id}</span>
        </span>
        <span className="flex shrink-0 items-center gap-1">
          {focus.working && focus.assignee ? (
            <span
              key={focus.assignee.name}
              className="flex h-7 items-center gap-1.5 rounded-md bg-brand/5 px-1.5 animate-in fade-in duration-300 motion-reduce:animate-none"
            >
              <AvatarStack actors={[focus.assignee]} />
              <span className="text-caption whitespace-nowrap text-info">{focus.assignee.name} is working</span>
            </span>
          ) : null}
          <HeaderButton icon={MoreHorizontal} />
          <HeaderButton icon={X} />
        </span>
      </div>
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-hidden">
        <div className="px-6 py-5">
          <p className="text-title-lg leading-snug font-semibold tracking-tight">{focus.title}</p>
          <div className="mt-3 flex flex-wrap items-center gap-1.5">
            <Pill key={status.key}>
              <CustomStatusIcon status={status} className="h-3.5 w-3.5" />
              {status.name}
            </Pill>
            <Pill key={focus.priority}>
              <PriorityIcon priority={focus.priority} />
              {PRIORITY_LABEL[focus.priority]}
            </Pill>
            {focus.assignee ? (
              <Pill key={focus.assignee.name}>
                <Avatar actor={focus.assignee} size={16} />
                {focus.assignee.name}
              </Pill>
            ) : null}
            <Pill>
              <Folder className="size-3.5 text-muted-foreground" />
              {workflow.project}
            </Pill>
            <Pill>
              <Calendar className="size-3.5 text-muted-foreground" />
              {workflow.focus.due}
            </Pill>
            <Pill>
              <span className="size-2 rounded-full bg-brand" />
              {workflow.focus.label}
            </Pill>
          </div>
          <p className="mt-5 text-body leading-relaxed">{workflow.focus.description}</p>
          <div className="mt-8 border-t border-border pt-6">
            <h2 className="text-title-sm font-semibold">Activity</h2>
            <div className="mt-4 flex flex-col gap-3">
              {items.map((item, index) => (
                <Entry key={index}>
                  <TimelineRow item={item} statuses={workflow.statuses} />
                </Entry>
              ))}
            </div>
          </div>
        </div>
      </div>
      <div className="shrink-0 px-6 pt-2 pb-4">
        <div className="flex items-center justify-between rounded-lg border border-border px-3 py-2.5 text-body text-muted-foreground">
          Leave a comment…
          <span className="flex items-center gap-2">
            <Paperclip className="size-3.5" />
            <span className="flex size-6 items-center justify-center rounded-md bg-muted">
              <ArrowUp className="size-3.5" />
            </span>
          </span>
        </div>
      </div>
    </aside>
  );
}

function Pill({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1.5 h-[var(--button-height-xs)] rounded-full border px-2.5 text-caption animate-in fade-in duration-300 motion-reduce:animate-none">
      {children}
    </span>
  );
}

function Entry({ children }: { children: ReactNode }) {
  return <div className="animate-in fade-in slide-in-from-bottom-1 duration-300 motion-reduce:animate-none">{children}</div>;
}

/** One entry of an issue's activity: a comment, or a status change and the handoff it made. */
export function TimelineRow({ item, statuses }: { item: TimelineItem; statuses: Status[] }) {
  if (item.kind === "comment") {
    return (
      <div className="overflow-clip rounded-xl border border-border bg-card">
        <div className="flex items-center gap-2.5 px-4 pt-3 pb-2">
          <Avatar actor={item.actor} size={24} />
          <span className="text-body font-medium">{item.actor.name}</span>
          <span className="text-caption text-muted-foreground">just now</span>
        </div>
        <div className="px-4 pb-3 text-body leading-relaxed">{item.body}</div>
      </div>
    );
  }
  const byName = item.kind === "created" ? item.by : item.actor.name;
  const lead =
    item.kind === "created" ? (
      item.icon
    ) : (
      <CustomStatusIcon status={statuses.find((s) => s.key === item.to)!} className="h-4 w-4 shrink-0" />
    );
  const text =
    item.kind === "created"
      ? "created this issue"
      : `changed status from ${statuses.find((s) => s.key === item.from)?.name} to ${statuses.find((s) => s.key === item.to)?.name}`;
  return (
    <div className="text-label text-muted-foreground">
      <div className="flex items-center">
        <div className="mr-2 flex w-4 shrink-0 justify-center">{lead}</div>
        <div className="flex min-w-0 flex-1 items-center gap-1">
          <span className="shrink-0 font-medium">{byName}</span>
          <span className="truncate">{text}</span>
          <span className="ml-auto shrink-0">{item.when ?? "just now"}</span>
        </div>
      </div>
      {item.handoff ? (
        <div className="mt-1 ml-6 flex flex-wrap items-center gap-1.5 text-caption">
          <CornerDownRight className="size-3 shrink-0" />
          <span>Handed to</span>
          <Avatar actor={item.handoff} size={16} />
          <span className="font-medium text-foreground">{item.handoff.name}</span>
          <span>{item.handoff.kind === "member" ? "(member, no run)" : "and started a run"}</span>
        </div>
      ) : null}
    </div>
  );
}