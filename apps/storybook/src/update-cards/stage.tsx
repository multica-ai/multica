import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Bell,
  Bot,
  Circle,
  CircleDashed,
  CircleDot,
  CircleUser,
  Filter,
  Folder,
  FolderMinus,
  History,
  ListTodo,
  MessageSquare,
  RotateCcw,
  Search,
  Sparkles,
  SquarePen,
  Users,
  Zap,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import type { Lang } from "../voice-input/copy";
import { useStoryEnv } from "../voice-input/env";
import { UI, UPDATES, type Text, type Update } from "./copy";

const tx = (text: Text, lang: Lang) => text[lang];

/** packages/views/layout/bottom-bar-styles.ts */
export const BOTTOM_BAR_ITEM_CLASS =
  "inline-flex h-6 min-w-0 shrink-0 cursor-pointer items-center gap-1.5 rounded-md px-2 text-caption text-muted-foreground transition-colors outline-none hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground focus-visible:outline-2 focus-visible:outline-ring aria-pressed:bg-sidebar-accent aria-pressed:text-sidebar-accent-foreground [&_svg]:size-3.5 [&_svg]:shrink-0";

/** Where a bottom bar entry renders; null when the stage has no bottom bar. */
const BottomBarSlot = createContext<HTMLElement | null>(null);
export const useBottomBarSlot = () => useContext(BottomBarSlot);

function RailItem({ icon, label, active, badge }: { icon: ReactNode; label: Text; active?: boolean; badge?: number }) {
  const { lang } = useStoryEnv();
  return (
    <div
      className={cn(
        "relative flex w-12 flex-col items-center gap-0.5 rounded-lg py-1.5 text-micro text-muted-foreground [&_svg]:size-4.5",
        active && "bg-sidebar-accent text-foreground",
      )}
    >
      {icon}
      <span>{tx(label, lang)}</span>
      {badge ? (
        <span className="absolute top-0.5 right-1 flex size-4 items-center justify-center rounded-full bg-brand text-[10px] font-semibold text-brand-foreground">
          {badge}
        </span>
      ) : null}
    </div>
  );
}

function SideLabel({ children }: { children: ReactNode }) {
  return <div className="mt-4 mb-1 px-2 text-caption font-medium text-muted-foreground">{children}</div>;
}

function SideRow({ icon, label, count, active }: { icon: ReactNode; label: string; count?: number; active?: boolean }) {
  return (
    <div className={cn("flex h-7 items-center gap-2 rounded-md px-2 text-label", active && "bg-sidebar-accent font-medium")}>
      <span className="text-muted-foreground [&_svg]:size-4">{icon}</span>
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {count !== undefined && <span className="text-caption text-muted-foreground">{count}</span>}
    </div>
  );
}

const INBOX_ROWS = [
  { who: "LP", title: "Stripe webhook retries double-charge", what: { zh: "Leo Park 提及了你", en: "Leo Park mentioned you" }, unread: true },
  { who: "AK", title: "Sidebar inbox: archive on done", what: { zh: "Ana Kim 回复了你", en: "Ana Kim replied" }, unread: true },
  { who: "B", title: "Fix flaky realtime test", what: { zh: "Builder 运行失败", en: "Builder run failed" }, unread: false },
];

const ISSUES = [
  { id: "DEV-21", title: "Stripe webhook retries double-charge", status: "doing" },
  { id: "DEV-19", title: "Sidebar inbox: archive notifications when an issue is done", status: "doing" },
  { id: "DEV-17", title: "Onboarding: add an Inbox step to the product tour", status: "todo" },
  { id: "DEV-16", title: "Desktop: show the unread badge on the Work rail item", status: "todo" },
  { id: "DEV-14", title: "Notarization fails on the macOS release job", status: "todo" },
  { id: "DEV-12", title: "Fix flaky realtime test for inbox:batch-archived", status: "backlog" },
  { id: "DEV-11", title: "Billing: prorate seat changes mid-cycle", status: "backlog" },
  { id: "DEV-9", title: "Agent run logs: collapse repeated tool calls", status: "backlog" },
];

const STATUS_ICON: Record<string, ReactNode> = {
  doing: <CircleDot className="size-4 text-warning" />,
  todo: <Circle className="size-4 text-muted-foreground" />,
  backlog: <CircleDashed className="size-4 text-muted-foreground" />,
};

/** The page card's content column: the box the star prompt, and these cards, are pinned to. */
function Content({ children, toast }: { children?: ReactNode; toast?: string | null }) {
  const { lang } = useStoryEnv();
  return (
    <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4 text-body font-semibold">
        <ListTodo className="size-4.5" />
        {tx({ zh: "全部任务", en: "All issues" }, lang)}
        <Filter className="ml-1 size-4 text-muted-foreground" />
      </div>
      <div className="min-h-0 flex-1 overflow-hidden px-2 py-1">
        {ISSUES.map((issue) => (
          <div key={issue.id} className="flex h-10 items-center gap-3 rounded-md px-2 text-label">
            {STATUS_ICON[issue.status]}
            <span className="w-12 shrink-0 text-caption text-muted-foreground">{issue.id}</span>
            <span className="min-w-0 flex-1 truncate">{issue.title}</span>
          </div>
        ))}
      </div>
      {toast && (
        <div
          key={toast}
          className="absolute top-14 left-1/2 z-50 -translate-x-1/2 rounded-lg bg-foreground px-3 py-1.5 text-caption text-background shadow-[var(--menu-shadow)] animate-in fade-in-0 slide-in-from-top-1"
        >
          {toast}
        </div>
      )}
      {children}
    </div>
  );
}

function Sidebar() {
  const { lang } = useStoryEnv();
  return (
    <div className="w-64 shrink-0 overflow-hidden border-r border-border bg-sidebar px-2 pt-3 text-sidebar-foreground">
      <div className="px-2 text-title-sm font-semibold">{tx({ zh: "工作", en: "Work" }, lang)}</div>
      <div className="mt-4 mb-1 flex items-center justify-between px-2 text-caption font-medium text-muted-foreground">
        <span>{tx({ zh: "收件箱", en: "Inbox" }, lang)}</span>
        <span className="text-brand">2</span>
      </div>
      {INBOX_ROWS.map((row) => (
        <div key={row.who} className="flex items-start gap-2 rounded-md px-2 py-1.5">
          <span className="mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full bg-muted text-[9px] font-medium text-muted-foreground">
            {row.who}
          </span>
          <span className="min-w-0 flex-1">
            <span className={cn("block truncate text-label", row.unread ? "font-medium" : "text-muted-foreground")}>{row.title}</span>
            <span className="block truncate text-caption text-muted-foreground">{tx(row.what, lang)}</span>
          </span>
          {row.unread && <span className="mt-2 size-1.5 shrink-0 rounded-full bg-brand" />}
        </div>
      ))}
      <SideLabel>{tx({ zh: "视图", en: "Views" }, lang)}</SideLabel>
      <SideRow icon={<ListTodo />} label={tx({ zh: "全部任务", en: "All issues" }, lang)} active />
      <SideRow icon={<CircleUser />} label={tx({ zh: "我的工作", en: "My work" }, lang)} count={8} />
      <SideRow icon={<Bot />} label={tx({ zh: "执行中", en: "Working" }, lang)} count={2} />
      <SideRow icon={<Bell />} label={tx({ zh: "已订阅", en: "Subscribed" }, lang)} count={14} />
      <SideLabel>{tx({ zh: "项目", en: "Projects" }, lang)}</SideLabel>
      <SideRow icon={<FolderMinus />} label={tx({ zh: "无项目", en: "No project" }, lang)} count={6} />
      <SideRow icon={<Folder />} label="Desktop 1.4" count={12} />
      <SideRow icon={<Folder />} label="Billing" count={5} />
    </div>
  );
}

/** The desktop bottom bar: quick chat on the right, and a slot for an entry before it. */
function BottomBar({ onSlot }: { onSlot: (el: HTMLElement | null) => void }) {
  const { lang } = useStoryEnv();
  return (
    <div className="flex h-8 shrink-0 items-center justify-end gap-1 pr-[18px] pl-3">
      <span ref={onSlot} className="contents" />
      <span className={BOTTOM_BAR_ITEM_CLASS}>
        <Sparkles />
        {tx({ zh: "问 Multica", en: "Ask Multica" }, lang)}
      </span>
      <span className={cn(BOTTOM_BAR_ITEM_CLASS, "w-6 justify-center px-0")}>
        <History />
      </span>
    </div>
  );
}

/** A Multica desktop window: rail, page card (Work panel and content), bottom bar. */
export function AppWindow({ children, toast, sidebar = true }: { children?: ReactNode; toast?: string | null; sidebar?: boolean }) {
  const [slot, setSlot] = useState<HTMLElement | null>(null);
  return (
    <BottomBarSlot.Provider value={slot}>
      <div className="flex h-full min-h-0 flex-col bg-app-shell text-foreground">
        <div className="flex min-h-0 flex-1">
          {sidebar && (
            <div className="flex w-16 shrink-0 flex-col items-center gap-1.5 py-3">
              <div className="mb-2 flex size-9 items-center justify-center rounded-lg bg-background text-label font-semibold ring-1 ring-border">
                D
              </div>
              <RailItem icon={<SquarePen />} label={{ zh: "新建", en: "New" }} />
              <RailItem icon={<Search />} label={{ zh: "搜索", en: "Search" }} />
              <RailItem icon={<ListTodo />} label={{ zh: "工作", en: "Work" }} active badge={2} />
              <RailItem icon={<Users />} label={{ zh: "团队", en: "Squads" }} />
              <RailItem icon={<MessageSquare />} label={{ zh: "聊天", en: "Chat" }} />
              <RailItem icon={<Zap />} label={{ zh: "自动化", en: "Autopilot" }} />
            </div>
          )}
          <div className={cn("mt-2 mr-2 flex min-w-0 flex-1 overflow-hidden rounded-xl bg-background ring-1 ring-border", !sidebar && "ml-2")}>
            {sidebar && <Sidebar />}
            <Content toast={toast}>{children}</Content>
          </div>
        </div>
        <BottomBar onSlot={setSlot} />
      </div>
    </BottomBarSlot.Provider>
  );
}

export const COUNTS = [1, 2, 4] as const;
export type Count = (typeof COUNTS)[number];

export interface TryItRender {
  updates: Update[];
  onFinished: () => void;
  onCta: (update: Update) => void;
}

/**
 * A playable stage: a bar to pick how many updates are waiting and to start
 * over, and the app window the option plays in.
 */
export function TryItStage({
  title,
  pitch,
  render,
  initialCount = 4,
}: {
  title: string;
  pitch: string;
  render: (props: TryItRender) => ReactNode;
  initialCount?: Count;
}) {
  const { lang } = useStoryEnv();
  const [count, setCount] = useState<Count>(initialCount);
  const [run, setRun] = useState(0);
  const [finished, setFinished] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(null), 1800);
    return () => window.clearTimeout(timer);
  }, [toast]);

  const restart = (next = count) => {
    setCount(next);
    setFinished(false);
    setRun((r) => r + 1);
  };

  return (
    <div className="flex h-screen flex-col bg-app-shell">
      <div className="flex h-14 shrink-0 items-center gap-4 border-b border-border bg-background px-4">
        <div className="min-w-0 flex-1">
          <div className="truncate text-body font-semibold">{title}</div>
          <div className="truncate text-caption text-muted-foreground">{pitch}</div>
        </div>
        <div className="flex items-center gap-1 text-caption text-muted-foreground">
          <span className="mr-1">{UI.count[lang]}</span>
          {COUNTS.map((n) => (
            <Button key={n} variant={n === count ? "secondary" : "ghost"} size="xs" onClick={() => restart(n)}>
              {n}
            </Button>
          ))}
        </div>
        <Button variant="outline" size="sm" onClick={() => restart()}>
          <RotateCcw data-icon="inline-start" />
          {UI.restart[lang]}
        </Button>
      </div>
      <div className="min-h-0 flex-1">
        <AppWindow toast={finished ? UI.allRead[lang] : toast}>
          <div key={`${run}-${lang}`} className="contents">
            {render({
              updates: UPDATES.slice(0, count),
              onFinished: () => setFinished(true),
              onCta: (u) => setToast(`→ ${u.cta[lang]}`),
            })}
          </div>
        </AppWindow>
      </div>
    </div>
  );
}

/** A frozen state in the sheet: the content column and bottom bar, at a fixed size. */
export function Frame({ label, children, height = 600 }: { label: string; children: ReactNode; height?: number }) {
  return (
    <figure className="flex flex-col gap-2">
      <figcaption className="text-caption text-muted-foreground">{label}</figcaption>
      <div className="w-[27rem] overflow-hidden rounded-lg ring-1 ring-border" style={{ height }}>
        <AppWindow sidebar={false}>{children}</AppWindow>
      </div>
    </figure>
  );
}
