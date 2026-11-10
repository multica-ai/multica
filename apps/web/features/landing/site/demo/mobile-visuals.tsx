"use client";

import type { ReactNode } from "react";
import {
  ArrowUp,
  ChevronLeft,
  ChevronRight,
  CircleUser,
  FolderKanban,
  Inbox,
  ListTodo,
  MessageSquare,
  MoreHorizontal,
  Pin,
  Plus,
  Search,
  Settings,
  SlidersHorizontal,
  type IconComponent,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import { PriorityIcon } from "@multica/views/issues/components/priority-icon";
import { Avatar, BACKEND, LEO, MIA, QA, SAM, Working, ZoomCanvas, type Actor } from "./mock-kit";

/*
 * Mockups for the Mobile page: the Multica iOS app on an iPhone, one screen
 * per tab plus an issue's agent runs. Phones are drawn at device size
 * (264 × 560) and are meant to run off the bottom of their frame. Strings
 * follow the app's own copy; data is fictional.
 */

type TabKey = "inbox" | "my-issues" | "chat" | "more";

const TABS: { key: TabKey; label: string; icon: IconComponent }[] = [
  { key: "inbox", label: "Inbox", icon: Inbox },
  { key: "my-issues", label: "My Issues", icon: CircleUser },
  { key: "chat", label: "Chat", icon: MessageSquare },
  { key: "more", label: "More", icon: MoreHorizontal },
];

function StatusBar() {
  return (
    <div className="flex h-11 shrink-0 items-end justify-between px-7 pb-1.5 text-[13px] font-semibold tabular-nums">
      <span>9:41</span>
      <span className="flex items-center gap-1">
        <span className="flex items-end gap-px">
          {[4, 6, 8, 10].map((h) => (
            <span key={h} className="w-[3px] rounded-[1px] bg-foreground" style={{ height: h }} />
          ))}
        </span>
        <span className="ml-1 h-[11px] w-[22px] rounded-xs border border-foreground/40 p-px">
          <span className="block h-full w-3/4 rounded-[1.5px] bg-foreground" />
        </span>
      </span>
    </div>
  );
}

function TabBar({ active }: { active: TabKey }) {
  return (
    <div className="flex h-[74px] shrink-0 items-start justify-around border-t border-border bg-background/95 pt-2">
      {TABS.map((tab) => (
        <span
          key={tab.key}
          className={cn("flex w-14 flex-col items-center gap-1 text-[10px] font-medium", tab.key === active ? "text-foreground" : "text-muted-foreground")}
        >
          <tab.icon className="size-[22px]" strokeWidth={tab.key === active ? 2.2 : 1.8} />
          {tab.label}
        </span>
      ))}
    </div>
  );
}

/** An iPhone at device size. `tab` adds the app's tab bar; leave it off for pushed screens. */
export function Phone({ className, tab, children }: { className?: string; tab?: TabKey; children: ReactNode }) {
  return (
    <div className={cn("h-[560px] w-[264px] rounded-[46px] bg-[#1c1c1e] p-[7px] shadow-site-float ring-1 ring-black/10", className)}>
      <div className="relative flex size-full flex-col overflow-hidden rounded-[39px] bg-background text-foreground">
        <span className="absolute top-[9px] left-1/2 h-[26px] w-[86px] -translate-x-1/2 rounded-full bg-[#1c1c1e]" />
        <StatusBar />
        <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">{children}</div>
        {tab ? <TabBar active={tab} /> : null}
        <span className="absolute bottom-2 left-1/2 h-[5px] w-[100px] -translate-x-1/2 rounded-full bg-foreground/80" />
      </div>
    </div>
  );
}

function LargeTitle({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="flex shrink-0 items-end justify-between px-4 pt-2 pb-2">
      <span className="text-[26px] leading-tight font-bold tracking-tight">{title}</span>
      <span className="flex items-center gap-3 pb-1.5 text-muted-foreground">{children}</span>
    </div>
  );
}

function NavBar({ back, title, children }: { back: string; title?: string; children?: ReactNode }) {
  return (
    <div className="flex h-10 shrink-0 items-center gap-1 px-2.5">
      <span className="flex items-center text-[14px] text-brand">
        <ChevronLeft className="size-5" />
        {back}
      </span>
      <span className="flex-1 text-center text-[14px] font-semibold">{title}</span>
      <span className="flex min-w-14 justify-end text-muted-foreground">{children}</span>
    </div>
  );
}

// --- Tab screens ---------------------------------------------------------------

const INBOX: { actor: Actor; issue: string; type: string; body: string; when: string; unread?: boolean }[] = [
  { actor: BACKEND, issue: "ACM-151", type: "Comment", body: "Apply this migration to the shared database?", when: "2m", unread: true },
  { actor: MIA, issue: "ACM-152", type: "Mention", body: "@Leo can you review the new tabs?", when: "18m", unread: true },
  { actor: QA, issue: "ACM-147", type: "Status changed", body: "Add an audit log → In Review", when: "1h" },
  { actor: SAM, issue: "ACM-149", type: "Assigned", body: "Rate-limit the public API", when: "3h" },
  { actor: BACKEND, issue: "ACM-144", type: "Status changed", body: "Document the export API → Done", when: "5h" },
];

export function InboxScreen() {
  return (
    <>
      <LargeTitle title="Inbox">
        <SlidersHorizontal className="size-[18px]" />
      </LargeTitle>
      <div className="flex flex-col">
        {INBOX.map((item) => (
          <div key={item.issue} className="flex gap-2.5 border-b border-border px-4 py-2.5">
            <span className="pt-0.5">
              <Avatar actor={item.actor} size={24} />
            </span>
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-1.5 text-[12px]">
                <span className="font-semibold">{item.actor.name}</span>
                <span className="text-muted-foreground">{item.type}</span>
                <span className="ml-auto text-muted-foreground tabular-nums">{item.when}</span>
              </span>
              <span className="mt-0.5 flex items-center gap-1.5 text-[12px]">
                <span className="min-w-0 flex-1 truncate text-muted-foreground">
                  <span className="text-foreground tabular-nums">{item.issue}</span> · {item.body}
                </span>
                {item.unread ? <span className="size-2 shrink-0 rounded-full bg-brand" /> : null}
              </span>
            </span>
          </div>
        ))}
      </div>
    </>
  );
}

const MY_ISSUES: { group: string; status: "in_progress" | "in_review" | "todo"; items: { id: string; title: string; priority: "urgent" | "high" | "medium" | "low" }[] }[] = [
  {
    group: "In Progress",
    status: "in_progress",
    items: [
      { id: "ACM-152", title: "Redesign settings", priority: "high" },
      { id: "ACM-148", title: "Fix flaky auth tests", priority: "urgent" },
    ],
  },
  { group: "In Review", status: "in_review", items: [{ id: "ACM-147", title: "Add an audit log", priority: "medium" }] },
  {
    group: "Todo",
    status: "todo",
    items: [
      { id: "ACM-153", title: "Rate-limit the API", priority: "high" },
      { id: "ACM-154", title: "Triage bug reports", priority: "low" },
    ],
  },
];

export function MyIssuesScreen() {
  return (
    <>
      <LargeTitle title="My Issues">
        <Search className="size-[18px]" />
        <Plus className="size-5" />
      </LargeTitle>
      <div className="flex shrink-0 gap-1.5 px-4 pb-2.5 text-[12px]">
        {["Assigned", "Created", "Agents"].map((tab, index) => (
          <span key={tab} className={cn("rounded-full px-2.5 py-1", index === 0 ? "bg-foreground text-background" : "bg-muted text-muted-foreground")}>
            {tab}
          </span>
        ))}
      </div>
      {MY_ISSUES.map((group) => (
        <div key={group.group}>
          <p className="flex items-center gap-1.5 bg-muted/50 px-4 py-1.5 text-[11px] font-medium text-muted-foreground">
            <StatusIcon status={group.status} className="size-3" />
            {group.group}
            <span className="tabular-nums">{group.items.length}</span>
          </p>
          {group.items.map((item) => (
            <div key={item.id} className="flex h-11 items-center gap-2 border-b border-border px-4 text-[13px]">
              <PriorityIcon priority={item.priority} />
              <span className="w-12 shrink-0 text-[11px] text-muted-foreground tabular-nums">{item.id}</span>
              <span className="min-w-0 flex-1 truncate">{item.title}</span>
            </div>
          ))}
        </div>
      ))}
    </>
  );
}

export function ChatScreen() {
  return (
    <>
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4">
        <Avatar actor={BACKEND} size={24} />
        <span className="min-w-0 flex-1">
          <span className="block text-[13px] font-semibold">Backend Agent</span>
          <span className="block text-[11px] text-muted-foreground">Claude Code · Mia’s MacBook Pro</span>
        </span>
      </div>
      <div className="flex min-h-0 flex-1 flex-col justify-end gap-3 overflow-hidden px-3 py-3 text-[13px] leading-snug">
        <span className="ml-auto max-w-[80%] rounded-2xl rounded-br-md bg-foreground px-3 py-2 text-background">
          Is the export fix safe to ship tonight?
        </span>
        <span className="flex max-w-[88%] gap-2">
          <Avatar actor={BACKEND} size={20} />
          <span className="rounded-2xl rounded-tl-md bg-muted px-3 py-2">
            Yes. The batched job passed on staging with 2.1M rows in 6 minutes, well under the timeout. PR #486 is
            approved.
          </span>
        </span>
        <span className="ml-auto max-w-[80%] rounded-2xl rounded-br-md bg-foreground px-3 py-2 text-background">
          Great, merge it after 6pm.
        </span>
        <span className="flex items-center gap-2 pl-7">
          <Working />
        </span>
      </div>
      <div className="flex shrink-0 items-center gap-2 px-3 pb-2.5">
        <span className="flex h-9 flex-1 items-center rounded-full border border-border px-3.5 text-[13px] text-muted-foreground">
          Message Backend Agent
        </span>
        <span className="flex size-8 items-center justify-center rounded-full bg-foreground text-background">
          <ArrowUp className="size-4" />
        </span>
      </div>
    </>
  );
}

const MORE: { icon: IconComponent; label: string; detail?: string }[] = [
  { icon: Pin, label: "Pinned", detail: "3" },
  { icon: ListTodo, label: "Issues" },
  { icon: FolderKanban, label: "Projects", detail: "4" },
  { icon: Settings, label: "Account settings" },
];

export function MoreScreen() {
  return (
    <>
      <LargeTitle title="More" />
      <div className="mx-4 flex items-center gap-3 rounded-xl bg-muted/60 px-3 py-2.5">
        <span className="flex size-8 items-center justify-center rounded-lg bg-surface text-[13px] font-semibold shadow-[var(--surface-shadow)]">A</span>
        <span className="min-w-0 flex-1">
          <span className="block text-[13px] font-semibold">Acme Labs</span>
          <span className="block text-[11px] text-muted-foreground">Switch workspace</span>
        </span>
        <ChevronRight className="size-4 text-muted-foreground" />
      </div>
      <div className="mx-4 mt-4 overflow-hidden rounded-xl bg-muted/60">
        {MORE.map((item, index) => (
          <div key={item.label} className={cn("flex h-11 items-center gap-3 px-3 text-[13px]", index > 0 && "border-t border-border")}>
            <item.icon className="size-4 text-muted-foreground" />
            <span className="flex-1">{item.label}</span>
            {item.detail ? <span className="text-muted-foreground tabular-nums">{item.detail}</span> : null}
            <ChevronRight className="size-4 text-faint-foreground" />
          </div>
        ))}
      </div>
    </>
  );
}

// --- Issue and its runs ----------------------------------------------------------

function IssueScreen() {
  return (
    <>
      <NavBar back="Inbox">
        <MoreHorizontal className="size-5" />
      </NavBar>
      <div className="px-4 pt-1">
        <p className="text-[11px] text-muted-foreground tabular-nums">ACM-151</p>
        <p className="mt-0.5 text-[18px] leading-snug font-semibold tracking-tight">Apply the export migration</p>
        <div className="mt-2.5 flex flex-wrap gap-1.5 text-[11px]">
          <span className="flex items-center gap-1 rounded-full bg-muted px-2 py-0.5">
            <StatusIcon status="in_progress" className="size-3" />
            In Progress
          </span>
          <span className="flex items-center gap-1 rounded-full bg-muted px-2 py-0.5">
            <PriorityIcon priority="high" />
            High
          </span>
          <span className="flex items-center gap-1 rounded-full bg-muted py-0.5 pr-2 pl-0.5">
            <Avatar actor={BACKEND} size={16} />
            Backend Agent
          </span>
        </div>
      </div>
      <div className="mt-3 flex min-h-0 flex-1 flex-col gap-3 border-t border-border px-4 pt-3">
        <span className="flex items-center gap-2 rounded-lg bg-muted/60 px-3 py-2 text-[12px]">
          <Working label="Agent working" />
          <span className="ml-auto flex items-center text-muted-foreground">
            Runs
            <ChevronRight className="size-3.5" />
          </span>
        </span>
        <div className="flex gap-2">
          <Avatar actor={BACKEND} size={20} />
          <div className="min-w-0 text-[12px] leading-snug">
            <p>
              <span className="font-semibold">Backend Agent</span> <span className="text-muted-foreground">2m</span>
            </p>
            <p className="mt-0.5">
              The migration adds a batch_id column to export_jobs. Apply it to the shared database?
            </p>
          </div>
        </div>
        <div className="flex gap-2">
          <Avatar actor={LEO} size={20} />
          <div className="min-w-0 text-[12px] leading-snug">
            <p>
              <span className="font-semibold">Leo Park</span> <span className="text-muted-foreground">now</span>
            </p>
            <p className="mt-0.5">Yes, go ahead. It’s backward compatible.</p>
          </div>
        </div>
      </div>
      <div className="shrink-0 px-3 pb-6">
        <span className="flex h-9 items-center rounded-full border border-border px-3.5 text-[12px] text-muted-foreground">
          Add a comment, type @ to mention...
        </span>
      </div>
    </>
  );
}

type Run = { actor: Actor; status: string; summary: string; tone?: "success" | "failed" };

const ACTIVE_RUNS: Run[] = [
  { actor: BACKEND, status: "Running", summary: "Comment run" },
  { actor: QA, status: "Queued", summary: "Run" },
];

const PAST_RUNS: Run[] = [
  { actor: BACKEND, status: "Run succeeded", summary: "Comment run", tone: "success" },
  { actor: QA, status: "Run failed", summary: "Run", tone: "failed" },
  { actor: BACKEND, status: "Run succeeded", summary: "Run", tone: "success" },
];

function RunRow({ run, active }: { run: Run; active?: boolean }) {
  return (
    <div className="flex items-center gap-2.5 px-3 py-3">
      <Avatar actor={run.actor} size={24} />
      <span className="min-w-0 flex-1">
        <span className="block text-[12px] font-semibold">{run.actor.name}</span>
        <span className="flex items-center gap-1 text-[11px]">
          {run.status === "Running" ? (
            <Working label="Running" />
          ) : (
            <span className={cn(run.tone === "failed" ? "text-destructive" : run.tone === "success" ? "text-success" : "text-muted-foreground")}>
              {run.status}
            </span>
          )}
          <span className="text-muted-foreground">· {run.summary}</span>
        </span>
      </span>
      {active ? (
        <span className="rounded-full px-2.5 py-1 text-[11px] font-medium text-destructive ring-1 ring-border">Cancel</span>
      ) : null}
    </div>
  );
}

function RunGroup({ title, runs, active }: { title: string; runs: Run[]; active?: boolean }) {
  return (
    <>
      <p className="px-4 pt-4 pb-1.5 text-[11px] font-medium text-muted-foreground">{title}</p>
      <div className="mx-4 overflow-hidden rounded-xl border border-border">
        {runs.map((run, index) => (
          <div key={`${run.actor.name}-${index}`} className={cn(index > 0 && "border-t border-border")}>
            <RunRow run={run} active={active} />
          </div>
        ))}
      </div>
    </>
  );
}

/** Agent Runs for an issue: active runs, each with Cancel, above past ones. */
export function RunsScreen() {
  return (
    <>
      <NavBar back="ACM-151" title="Agent Runs" />
      <RunGroup title="Active" runs={ACTIVE_RUNS} active />
      <RunGroup title="Past" runs={PAST_RUNS} />
    </>
  );
}

// --- Compositions --------------------------------------------------------------

const SCENE = { width: 1100, height: 600 } as const;
const SCENE_NARROW = { width: 440, height: 440 } as const;

/** Hero: the inbox, an agent asking a question on an issue, and a chat with that agent. */
export function MobileScene() {
  return (
    <>
      <div className="relative aspect-square sm:hidden">
        <ZoomCanvas {...SCENE_NARROW}>
          <Phone className="absolute top-1/2 left-1/2 -translate-1/2 scale-[0.76]">
            <IssueScreen />
          </Phone>
        </ZoomCanvas>
      </div>
      <div className="relative hidden aspect-[1100/600] sm:block">
        <ZoomCanvas {...SCENE}>
          <Phone tab="inbox" className="absolute -bottom-3 left-[calc(50%-132px-290px)] origin-bottom scale-[0.9]">
            <InboxScreen />
          </Phone>
          <Phone tab="chat" className="absolute -bottom-3 left-[calc(50%-132px+290px)] origin-bottom scale-[0.9]">
            <ChatScreen />
          </Phone>
          <Phone className="absolute -bottom-3 left-[calc(50%-132px)]">
            <IssueScreen />
          </Phone>
        </ZoomCanvas>
      </div>
    </>
  );
}

/** A whole phone, scaled to sit inside the walkthrough frame with its tab bar in view. */
function TabPhone({ tab, children }: { tab: TabKey; children: ReactNode }) {
  return (
    <Phone
      tab={tab}
      className="absolute top-1/2 left-1/2 -translate-1/2 scale-[0.6] sm:scale-[0.68] lg:scale-[0.78]"
    >
      {children}
    </Phone>
  );
}

export function InboxTabVisual() {
  return (
    <TabPhone tab="inbox">
      <InboxScreen />
    </TabPhone>
  );
}

export function MyIssuesTabVisual() {
  return (
    <TabPhone tab="my-issues">
      <MyIssuesScreen />
    </TabPhone>
  );
}

export function ChatTabVisual() {
  return (
    <TabPhone tab="chat">
      <ChatScreen />
    </TabPhone>
  );
}

export function MoreTabVisual() {
  return (
    <TabPhone tab="more">
      <MoreScreen />
    </TabPhone>
  );
}

export function RunsVisual() {
  return (
    <Phone className="absolute top-8 left-1/2 origin-top -translate-x-1/2 scale-[0.68] sm:top-10 sm:scale-[0.86]">
      <RunsScreen />
    </Phone>
  );
}
