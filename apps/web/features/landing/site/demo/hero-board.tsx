"use client";

import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ComponentType,
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
  FilePen,
  FileSearch,
  FolderKanban,
  GitMerge,
  GitPullRequest,
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
  SquareTerminal,
  Users,
  X,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { ShimmerText } from "@multica/ui/components/common/shimmer-text";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import { PriorityIcon } from "@multica/views/issues/components/priority-icon";
import { HERO_PROVIDERS } from "../../components/provider-marks";

/*
 * Hero product mockup: the issue board, built from the product's own tokens
 * and glyphs at a fixed design size, then zoomed to fit its frame. Data is
 * fictional. It loops one story: an unassigned issue is handed to an agent,
 * which works it, opens a PR, gets it approved, and lands it in Done.
 */

const WIDE_WIDTH = 1200;
const NARROW_WIDTH = 540;
const NARROW_BELOW = 600;
const DESIGN_HEIGHT = 720;
const COLUMN_STRIDE = 232 + 12;

type Actor =
  | { kind: "member"; name: string; initials: string }
  | { kind: "agent"; name: string; provider: string };

const MIA: Actor = { kind: "member", name: "Mia Chen", initials: "MC" };
const LEO: Actor = { kind: "member", name: "Leo Park", initials: "LP" };
const SAM: Actor = { kind: "member", name: "Sam Rivera", initials: "SR" };
const BACKEND: Actor = { kind: "agent", name: "Backend Agent", provider: "Claude Code" };
const QA: Actor = { kind: "agent", name: "QA Agent", provider: "Codex" };
const FRONTEND: Actor = { kind: "agent", name: "Frontend Agent", provider: "Cursor" };

type Priority = "urgent" | "high" | "medium" | "low";
type ColumnId = "todo" | "in_progress" | "in_review" | "done";
type Card = {
  id: string;
  title: string;
  priority: Priority;
  assignee?: Actor;
  working?: boolean;
  updated: string;
};
type Column = { status: ColumnId; label: string; total: number; cards: Card[] };

const HERO_CARD: Card = { id: "ACM-142", title: "Add rate limiting to the public API", priority: "high", updated: "just now" };

const COLUMNS: Column[] = [
  {
    status: "todo",
    label: "Todo",
    total: 11,
    cards: [
      { id: "ACM-140", title: "Dark mode for billing settings", priority: "medium", assignee: MIA, updated: "1h ago" },
      { id: "ACM-138", title: "Upgrade the Postgres driver to v5", priority: "low", assignee: LEO, updated: "3h ago" },
      { id: "ACM-135", title: "Add SAML login for enterprise workspaces", priority: "medium", updated: "5h ago" },
      { id: "ACM-134", title: "Search results ignore archived projects", priority: "low", assignee: SAM, updated: "1d ago" },
    ],
  },
  {
    status: "in_progress",
    label: "In Progress",
    total: 4,
    cards: [
      { id: "ACM-136", title: "Move auth webhooks onto the job queue", priority: "high", assignee: BACKEND, working: true, updated: "2m ago" },
      { id: "ACM-133", title: "Fix the flaky checkout test on CI", priority: "urgent", assignee: QA, working: true, updated: "just now" },
      { id: "ACM-131", title: "Rewrite the onboarding emails", priority: "medium", assignee: SAM, updated: "40m ago" },
      { id: "ACM-130", title: "Usage chart for the admin dashboard", priority: "medium", assignee: FRONTEND, updated: "1h ago" },
    ],
  },
  {
    status: "in_review",
    label: "In Review",
    total: 3,
    cards: [
      { id: "ACM-129", title: "Paginate the audit log endpoint", priority: "medium", assignee: BACKEND, updated: "12m ago" },
      { id: "ACM-127", title: "Refresh the pricing page copy", priority: "low", assignee: MIA, updated: "2h ago" },
      { id: "ACM-126", title: "Cache the plan lookup on checkout", priority: "medium", assignee: QA, updated: "4h ago" },
    ],
  },
  {
    status: "done",
    label: "Done",
    total: 28,
    cards: [
      { id: "ACM-125", title: "CSV export for reports", priority: "medium", assignee: FRONTEND, updated: "1h ago" },
      { id: "ACM-122", title: "Retry failed Stripe webhooks", priority: "high", assignee: BACKEND, updated: "3h ago" },
      { id: "ACM-120", title: "Update the SSO setup guide", priority: "low", assignee: LEO, updated: "5h ago" },
      { id: "ACM-118", title: "Remove the legacy v1 export endpoint", priority: "medium", assignee: BACKEND, updated: "1d ago" },
      { id: "ACM-117", title: "Fix timezone drift in weekly digests", priority: "high", assignee: QA, updated: "1d ago" },
    ],
  },
];

const COLUMN_ORDER: ColumnId[] = ["todo", "in_progress", "in_review", "done"];
const BASE_WORKING = COLUMNS.flatMap((c) => c.cards).filter((card) => card.working).length;

/** One beat of the story: where the issue is, what the panel shows, and how long to hold. */
type Stage = {
  column: ColumnId;
  assigned: boolean;
  working: boolean;
  panel: boolean;
  entries: number;
  hold: number;
};

const STAGES: Stage[] = [
  { column: "todo", assigned: false, working: false, panel: false, entries: 0, hold: 1800 },
  { column: "todo", assigned: true, working: false, panel: false, entries: 0, hold: 900 },
  { column: "in_progress", assigned: true, working: true, panel: true, entries: 1, hold: 1100 },
  { column: "in_progress", assigned: true, working: true, panel: true, entries: 2, hold: 1000 },
  { column: "in_progress", assigned: true, working: true, panel: true, entries: 3, hold: 1000 },
  { column: "in_progress", assigned: true, working: true, panel: true, entries: 4, hold: 1300 },
  { column: "in_review", assigned: true, working: false, panel: true, entries: 5, hold: 1700 },
  { column: "in_review", assigned: true, working: false, panel: true, entries: 6, hold: 1300 },
  { column: "done", assigned: true, working: false, panel: true, entries: 7, hold: 1500 },
  { column: "done", assigned: true, working: false, panel: false, entries: 7, hold: 2200 },
];

const MOVE_EASE = "cubic-bezier(0.32, 0.72, 0, 1)";

export function HeroBoard() {
  const windowRef = useRef<HTMLDivElement>(null);
  const boardRef = useRef<HTMLDivElement>(null);
  const positions = useRef<{ zoom: number; at: Map<string, { x: number; y: number }> } | null>(null);
  const [fit, setFit] = useState<{ zoom: number; narrow: boolean } | null>(null);
  const [visible, setVisible] = useState(false);
  const [step, setStep] = useState(0);
  const reducedMotion = usePrefersReducedMotion();
  const stage = STAGES[step]!;

  useEffect(() => {
    const el = windowRef.current;
    if (!el) return;
    const resize = new ResizeObserver(() => {
      const narrow = el.clientWidth < NARROW_BELOW;
      setFit({ zoom: el.clientWidth / (narrow ? NARROW_WIDTH : WIDE_WIDTH), narrow });
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
    const timer = setTimeout(() => setStep((current) => (current + 1) % STAGES.length), stage.hold);
    return () => clearTimeout(timer);
  }, [step, stage.hold, visible, reducedMotion]);

  // FLIP: cards that changed place glide from where they were. Positions are
  // relative to the board, so panning it on narrow screens is not a move.
  useLayoutEffect(() => {
    const board = boardRef.current;
    if (!board || !fit) return;
    const origin = board.getBoundingClientRect();
    const next = new Map<string, { x: number; y: number }>();
    const cards = board.querySelectorAll<HTMLElement>("[data-card]");
    for (const el of cards) {
      const rect = el.getBoundingClientRect();
      next.set(el.dataset.card!, { x: (rect.left - origin.left) / fit.zoom, y: (rect.top - origin.top) / fit.zoom });
    }
    const previous = positions.current;
    if (previous && previous.zoom === fit.zoom && !reducedMotion) {
      for (const el of cards) {
        const id = el.dataset.card!;
        const from = previous.at.get(id);
        const to = next.get(id);
        if (!from || !to) continue;
        if (id === HERO_CARD.id && step === 0) {
          el.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 400, easing: "ease-out" });
          continue;
        }
        const dx = from.x - to.x;
        const dy = from.y - to.y;
        if (Math.abs(dx) < 0.5 && Math.abs(dy) < 0.5) continue;
        el.animate([{ transform: `translate(${dx}px, ${dy}px)` }, { transform: "none" }], {
          duration: id === HERO_CARD.id ? 650 : 450,
          easing: MOVE_EASE,
        });
      }
    }
    positions.current = { zoom: fit.zoom, at: next };
  }, [step, fit, reducedMotion]);

  const narrow = fit?.narrow ?? false;
  const heroCard: Card = {
    ...HERO_CARD,
    assignee: stage.assigned ? BACKEND : undefined,
    working: stage.working,
  };
  const pan = narrow ? Math.min(Math.max(COLUMN_ORDER.indexOf(stage.column) - 1, 0), 2) * COLUMN_STRIDE : 0;

  return (
    <div
      ref={windowRef}
      role="img"
      aria-label="A Multica issue board: an issue is assigned to a coding agent, which works on it, opens a pull request, and moves it to done after review"
      className="absolute inset-0 overflow-hidden rounded-[12px] border border-site-line-strong bg-app-shell shadow-site-float"
    >
      <div
        aria-hidden
        className={cn("flex text-foreground", !fit && "opacity-0")}
        style={{ width: narrow ? NARROW_WIDTH : WIDE_WIDTH, height: DESIGN_HEIGHT, zoom: fit?.zoom }}
      >
        {narrow ? null : <Sidebar />}
        <main
          className={cn(
            "relative flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-surface-border bg-background shadow-[var(--surface-shadow)]",
            narrow ? "m-1.5" : "my-1.5 mr-1.5",
          )}
        >
          <div className="flex h-11 shrink-0 items-center gap-2 border-b border-border px-4">
            <ListTodo className="size-4 text-muted-foreground" />
            <span className="text-label font-medium">Issues</span>
          </div>
          <Toolbar working={BASE_WORKING + (stage.working ? 1 : 0)} compact={narrow} />
          <div className="min-h-0 flex-1 overflow-hidden">
            <div
              ref={boardRef}
              className="flex items-start gap-3 px-3 pb-3 transition-transform duration-700 ease-in-out motion-reduce:transition-none"
              style={{ transform: pan ? `translateX(${-pan}px)` : undefined }}
            >
              {COLUMNS.map((column) => {
                const holdsHero = column.status === stage.column;
                return (
                  <BoardColumn
                    key={column.status}
                    column={column}
                    total={column.total + (holdsHero ? 1 : 0)}
                    cards={holdsHero ? [heroCard, ...column.cards] : column.cards}
                    peeked={stage.panel && !narrow ? HERO_CARD.id : undefined}
                  />
                );
              })}
            </div>
          </div>
          {narrow ? null : <IssuePanel stage={stage} />}
        </main>
      </div>
    </div>
  );
}

function usePrefersReducedMotion() {
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
          <span className="inline-flex size-5 shrink-0 items-center justify-center">
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

const STATUS_LABEL: Record<ColumnId, string> = {
  todo: "Todo",
  in_progress: "In Progress",
  in_review: "In Review",
  done: "Done",
};

function IssuePanel({ stage }: { stage: Stage }) {
  return (
    <aside
      data-open={stage.panel}
      className={cn(
        "absolute inset-y-0 right-0 z-10 flex w-[380px] flex-col border-l border-border bg-background shadow-[var(--floating-shadow)]",
        "transition-transform duration-500 ease-[cubic-bezier(0.32,0.72,0,1)] motion-reduce:transition-none",
        "data-[open=false]:translate-x-full data-[open=false]:duration-300 data-[open=false]:ease-in",
      )}
    >
      <div className="flex h-11 shrink-0 items-center justify-between border-b border-border px-4">
        <span className="flex items-center gap-1 text-caption text-muted-foreground">
          Acme Labs <ChevronRight className="size-3" /> <span className="text-foreground">{HERO_CARD.id}</span>
        </span>
        <span className="flex items-center gap-3 text-muted-foreground">
          <Maximize2 className="size-3.5" />
          <X className="size-3.5" />
        </span>
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-5 overflow-hidden px-5 py-4">
        <p className="text-title font-semibold leading-snug">{HERO_CARD.title}</p>
        <dl className="grid grid-cols-[88px_1fr] items-center gap-y-2.5 text-caption">
          <dt className="text-muted-foreground">Status</dt>
          <dd key={stage.column} className="flex items-center gap-1.5 animate-in fade-in duration-300 motion-reduce:animate-none">
            <StatusIcon status={stage.column} className="h-3.5 w-3.5" />
            {STATUS_LABEL[stage.column]}
          </dd>
          <dt className="text-muted-foreground">Priority</dt>
          <dd className="flex items-center gap-1.5">
            <PriorityIcon priority="high" />
            High
          </dd>
          <dt className="text-muted-foreground">Assignee</dt>
          <dd className="flex items-center gap-1.5">
            <Avatar actor={BACKEND} size={16} />
            {BACKEND.name}
          </dd>
        </dl>
        <div className="flex flex-col gap-3 border-t border-border pt-4">
          <p className="text-caption font-medium text-muted-foreground">Activity</p>
          {stage.entries >= 1 ? (
            <Entry>
              <ActivityRow icon={<Avatar actor={BACKEND} size={16} />}>
                <span className="font-medium text-foreground">Backend Agent</span> picked this up
              </ActivityRow>
            </Entry>
          ) : null}
          {stage.entries >= 2 ? (
            <Entry>
              <RunCard stage={stage} />
            </Entry>
          ) : null}
          {stage.entries >= 5 ? (
            <Entry>
              <Comment actor={BACKEND}>
                <p>Added a token bucket per API key: 100 requests a minute by default, 429 with Retry-After past that.</p>
                <span className="mt-2 flex items-center gap-2 rounded-md border border-border px-2.5 py-1.5">
                  <GitPullRequest className="size-3.5 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate font-medium">#318 Rate limit the public API</span>
                  <span className="text-success tabular-nums">+184</span>
                  <span className="text-destructive tabular-nums">−12</span>
                </span>
              </Comment>
            </Entry>
          ) : null}
          {stage.entries >= 6 ? (
            <Entry>
              <Comment actor={MIA}>
                <p>Reviewed, nice tests. Approved.</p>
              </Comment>
            </Entry>
          ) : null}
          {stage.entries >= 7 ? (
            <Entry>
              <ActivityRow icon={<GitMerge className="size-3.5 text-muted-foreground" />}>
                <span className="font-medium text-foreground">Mia Chen</span> merged #318
              </ActivityRow>
            </Entry>
          ) : null}
        </div>
      </div>
    </aside>
  );
}

function Entry({ children }: { children: ReactNode }) {
  return <div className="animate-in fade-in slide-in-from-bottom-1 duration-300 motion-reduce:animate-none">{children}</div>;
}

function ActivityRow({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  return (
    <p className="flex items-center gap-2 text-caption text-muted-foreground">
      <span className="flex size-4 items-center justify-center">{icon}</span>
      <span>{children}</span>
      <span className="ml-auto">just now</span>
    </p>
  );
}

const RUN_STEPS: { icon: LucideIcon; text: ReactNode }[] = [
  { icon: FileSearch, text: "Read 14 files in api/middleware" },
  {
    icon: FilePen,
    text: (
      <>
        Edited 3 files <span className="text-success">+184</span> <span className="text-destructive">−12</span>
      </>
    ),
  },
  {
    icon: SquareTerminal,
    text: (
      <>
        go test ./... <span className="text-muted-foreground">212 passed</span>
      </>
    ),
  },
];

function RunCard({ stage }: { stage: Stage }) {
  const shown = Math.min(stage.entries - 1, RUN_STEPS.length);
  return (
    <div className="rounded-lg border border-border bg-muted/40 p-3">
      <div className="flex items-center justify-between text-caption">
        <span className="flex items-center gap-1.5 font-medium">
          <Avatar actor={BACKEND} size={16} />
          Run · Claude Code
        </span>
        {stage.working ? (
          <span className="flex items-center gap-1.5">
            <span className="size-1.5 rounded-full bg-site-agent" />
            <ShimmerText className="text-micro text-muted-foreground">Working</ShimmerText>
          </span>
        ) : (
          <span className="flex items-center gap-1 text-micro text-muted-foreground">
            <Check className="size-3" /> Finished in 3m 48s
          </span>
        )}
      </div>
      <ul className="mt-2.5 flex flex-col gap-1.5 font-mono text-micro">
        {RUN_STEPS.slice(0, shown).map((runStep, index) => (
          <li key={index} className="flex items-center gap-2 animate-in fade-in duration-300 motion-reduce:animate-none">
            <runStep.icon className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{runStep.text}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function Comment({ actor, children }: { actor: Actor; children: ReactNode }) {
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

const PROVIDER_MARKS = new Map<string, ComponentType<{ className?: string }>>(
  HERO_PROVIDERS.map((provider) => [provider.name, provider.Mark]),
);

function Avatar({ actor, size }: { actor: Actor; size: 16 | 20 }) {
  if (actor.kind === "member") {
    return <ActorAvatar name={actor.name} initials={actor.initials} size={size === 16 ? "xs" : "sm"} />;
  }
  const Mark = PROVIDER_MARKS.get(actor.provider);
  return (
    <span
      className="inline-flex shrink-0 items-center justify-center rounded-full border border-border bg-background text-foreground"
      style={{ width: size, height: size }}
    >
      {Mark ? <Mark className={size === 16 ? "size-2.5" : "size-3"} /> : <Bot className="size-3" />}
    </span>
  );
}
