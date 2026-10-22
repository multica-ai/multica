"use client";

import { useEffect, useRef, useState, type ComponentType } from "react";
import {
  BookOpenText,
  Bot,
  ChevronDown,
  CircleUser,
  Columns3,
  FolderKanban,
  Inbox,
  Layers,
  ListFilter,
  ListTodo,
  MessageSquare,
  Monitor,
  MoreHorizontal,
  Plus,
  Search,
  SlidersHorizontal,
  SquarePen,
  Users,
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
 * fictional. This is the resting frame the hero animation starts from.
 */

const DESIGN_WIDTH = 1200;
const DESIGN_HEIGHT = 720;

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
type Card = {
  id: string;
  title: string;
  priority: Priority;
  assignee?: Actor;
  working?: boolean;
  updated: string;
};
type Column = { status: "todo" | "in_progress" | "in_review" | "done"; label: string; total: number; cards: Card[] };

const COLUMNS: Column[] = [
  {
    status: "todo",
    label: "Todo",
    total: 12,
    cards: [
      { id: "ACM-142", title: "Add rate limiting to the public API", priority: "high", updated: "just now" },
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

const WORKING_AGENTS = COLUMNS.flatMap((c) => c.cards).filter((card) => card.working).length;

export function HeroBoard() {
  const windowRef = useRef<HTMLDivElement>(null);
  const [zoom, setZoom] = useState<number | null>(null);

  useEffect(() => {
    const el = windowRef.current;
    if (!el) return;
    const observer = new ResizeObserver(() => setZoom(el.clientWidth / DESIGN_WIDTH));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  return (
    <div
      ref={windowRef}
      role="img"
      aria-label="A Multica issue board where teammates and coding agents work side by side"
      className="absolute inset-x-[4%] top-[6%] bottom-0 overflow-hidden rounded-t-[12px] border border-b-0 border-site-line-strong bg-app-shell shadow-site-float"
    >
      <div
        aria-hidden
        className={cn("flex text-foreground", zoom == null && "opacity-0")}
        style={{ width: DESIGN_WIDTH, height: DESIGN_HEIGHT, zoom: zoom ?? undefined }}
      >
        <Sidebar />
        <main className="my-1.5 mr-1.5 flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-surface-border bg-background shadow-[var(--surface-shadow)]">
          <div className="flex h-11 shrink-0 items-center gap-2 border-b border-border px-4">
            <ListTodo className="size-4 text-muted-foreground" />
            <span className="text-label font-medium">Issues</span>
          </div>
          <Toolbar />
          <div className="flex min-h-0 flex-1 items-start gap-3 px-3 pb-3">
            {COLUMNS.map((column) => (
              <BoardColumn key={column.status} column={column} />
            ))}
          </div>
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

function NavGroup({ label, children }: { label: string; children: React.ReactNode }) {
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

function Toolbar() {
  return (
    <div className="flex h-12 shrink-0 items-center justify-between px-3">
      <div className="flex items-center gap-1">
        <span className="flex h-7 items-center rounded-md border border-border bg-muted px-2.5 text-caption font-medium">All</span>
        <span className="flex h-7 items-center rounded-md px-2.5 text-caption text-muted-foreground">Members</span>
        <span className="flex h-7 items-center rounded-md px-2.5 text-caption text-muted-foreground">Agents</span>
        <Layers className="ml-1 size-3.5 text-muted-foreground" />
      </div>
      <div className="flex items-center gap-1.5">
        <ToolbarButton>
          <span className="size-1.5 rounded-full bg-site-agent" />
          {WORKING_AGENTS} agents working
        </ToolbarButton>
        <ToolbarButton icon={ListFilter}>Filter</ToolbarButton>
        <ToolbarButton icon={SlidersHorizontal}>Display</ToolbarButton>
        <ToolbarButton icon={Columns3}>Board</ToolbarButton>
      </div>
    </div>
  );
}

function ToolbarButton({ icon: Icon, children }: { icon?: LucideIcon; children: React.ReactNode }) {
  return (
    <span className="flex h-7 items-center gap-1.5 rounded-md border border-border bg-background px-2.5 text-caption">
      {Icon ? <Icon className="size-3.5 text-muted-foreground" /> : null}
      {children}
    </span>
  );
}

function BoardColumn({ column }: { column: Column }) {
  return (
    <section className="flex w-[232px] shrink-0 flex-col rounded-xl bg-muted/40 p-2">
      <div className="mb-2 flex items-center justify-between gap-2 px-1.5">
        <div className="flex items-center gap-2">
          <span className="inline-flex items-center gap-1.5 text-caption font-semibold">
            <StatusIcon status={column.status} className="h-3 w-3" />
            {column.label}
          </span>
          <span className="text-caption text-muted-foreground">{column.total}</span>
        </div>
        <div className="flex items-center gap-2 text-muted-foreground">
          <MoreHorizontal className="size-3.5" />
          <Plus className="size-3.5" />
        </div>
      </div>
      <div className="flex flex-col gap-2 p-1">
        {column.cards.map((card) => (
          <BoardCard key={card.id} card={card} />
        ))}
      </div>
    </section>
  );
}

function BoardCard({ card }: { card: Card }) {
  return (
    <div className="rounded-lg border-[0.5px] border-surface-border bg-surface px-2.5 py-3 shadow-[var(--surface-shadow)]">
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-1.5">
          <span className="inline-flex size-5 shrink-0 items-center justify-center">
            <PriorityIcon priority={card.priority} />
          </span>
          <p className="truncate text-caption text-muted-foreground">{card.id}</p>
        </div>
        {card.working && card.assignee ? (
          <span className="inline-flex shrink-0 items-center gap-1">
            <Avatar actor={card.assignee} size={16} />
            <span className="size-1.5 rounded-full bg-site-agent" />
            <ShimmerText className="text-micro text-muted-foreground">Working</ShimmerText>
          </span>
        ) : null}
      </div>
      <p className="mt-1 line-clamp-2 text-body leading-snug font-medium">{card.title}</p>
      <div className="mt-2 flex items-center justify-between gap-2">
        {card.assignee ? (
          <span className="flex min-w-0 items-center gap-1.5">
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
