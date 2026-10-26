"use client";

import { Fragment, type CSSProperties, type ReactNode } from "react";
import {
  Bot,
  Check,
  ChevronRight,
  CircleUser,
  Hash,
  Inbox,
  ListTodo,
  Server,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import { GroupBand } from "./feature-visuals";
import {
  Avatar,
  BACKEND,
  CONTENT,
  FRONTEND,
  LEAD,
  LEO,
  MIA,
  QA,
  RESEARCH,
  SAM,
  TRIAGE,
  Working,
  type Actor,
} from "./mock-kit";

/*
 * Scenes for the home page before-and-after stage. Each story has two scenes
 * on the same canvas: "before" is a desktop cluttered with the tools a team
 * already has, drawn plain and grey so it never reads as any one vendor's
 * product; "after" is the Multica app doing the same work. The stage opens
 * split down the middle, so each scene keeps its main window in its own half
 * (before on the left, after on the right) and puts supporting details in the
 * other half for whoever drags the divider. The canvas grows on wide screens
 * (see ZoomCanvas), so windows are placed in percentages. Data is fictional.
 */

export const STAGE_CANVAS = { width: 600, height: 400 } as const;

// --- Before: plain desktop windows -------------------------------------------

function Desk({ className, style, title, children }: { className?: string; style: CSSProperties; title?: ReactNode; children: ReactNode }) {
  return (
    <div
      className={cn("absolute flex flex-col overflow-hidden rounded-lg border border-site-line-strong bg-site-surface shadow-site-float", className)}
      style={style}
    >
      <div className="flex h-7 shrink-0 items-center gap-1.5 border-b border-site-line bg-site-sunken px-2.5">
        {[0, 1, 2].map((dot) => (
          <span key={dot} className="size-2 rounded-full bg-site-line-strong" />
        ))}
        {title ? <span className="ml-1.5 min-w-0 truncate text-caption text-muted-foreground">{title}</span> : null}
      </div>
      {children}
    </div>
  );
}

function Toast({ style, actor, app, children }: { style: CSSProperties; actor: Actor; app: string; children: ReactNode }) {
  return (
    <div className="absolute flex gap-2 rounded-xl border border-site-line-strong bg-site-surface p-2.5 shadow-site-float" style={style}>
      <Avatar actor={actor} size={24} />
      <div className="min-w-0 flex-1 text-caption">
        <p className="flex items-center gap-1.5">
          <span className="font-medium">{actor.name}</span>
          <span className="text-muted-foreground">{app}</span>
          <span className="ml-auto text-muted-foreground">now</span>
        </p>
        <p className="mt-0.5 text-muted-foreground">{children}</p>
      </div>
    </div>
  );
}

function Mono({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("flex flex-col gap-1 p-3 font-mono text-caption text-muted-foreground *:truncate", className)}>{children}</div>;
}

const NOTE = "absolute rounded-sm bg-[#fdf3c4] px-3 py-2.5 font-mono text-caption text-[#6b5a1e] shadow-site-float";

// --- After: the Multica app ---------------------------------------------------

const NAV: { key: string; icon: LucideIcon; badge?: boolean }[] = [
  { key: "inbox", icon: Inbox, badge: true },
  { key: "mine", icon: CircleUser },
  { key: "issues", icon: ListTodo },
  { key: "agents", icon: Bot },
  { key: "autopilots", icon: Zap },
  { key: "runtimes", icon: Server },
];

function App({ active, children }: { active: string; children: ReactNode }) {
  return (
    <div className="absolute top-[12%] right-[4%] -bottom-6 left-[52%] flex overflow-hidden rounded-xl border border-site-line-strong bg-background shadow-site-float">
      <div className="flex w-11 shrink-0 flex-col items-center gap-1 border-r border-border bg-muted/40 py-2.5">
        <span className="mb-1.5 flex size-6 items-center justify-center rounded-md bg-foreground text-micro font-semibold text-background">A</span>
        {NAV.map((item) => (
          <span
            key={item.key}
            className={cn(
              "relative flex size-7 items-center justify-center rounded-md",
              item.key === active ? "bg-accent text-foreground" : "text-muted-foreground",
            )}
          >
            <item.icon className="size-3.5" />
            {item.badge ? <span className="absolute top-1 right-1 size-1.5 rounded-full bg-brand" /> : null}
          </span>
        ))}
      </div>
      <div className="flex min-w-0 flex-1 flex-col">{children}</div>
    </div>
  );
}

/** A floating Multica card, for the details beside the app window. */
function Card({ style, children }: { style: CSSProperties; children: ReactNode }) {
  return (
    <div className="absolute rounded-xl border border-border bg-background p-3 shadow-site-float" style={style}>
      {children}
    </div>
  );
}

function AppHeader({ children }: { children: ReactNode }) {
  return <div className="flex h-11 shrink-0 items-center gap-2 border-b border-border px-4">{children}</div>;
}

// --- 1. Thirty agents, thirty tabs ---------------------------------------------

const TABS = ["web · settings", "api · auth", "infra · ci"];

export function TabsBefore() {
  return (
    <>
      <Desk style={{ left: "4%", top: "8%", width: "42%", height: "54%" }} title="zsh — 30 sessions">
        <div className="flex h-7 shrink-0 items-stretch border-b border-site-line text-caption text-muted-foreground">
          {TABS.map((tab, index) => (
            <span
              key={tab}
              className={cn("flex w-[86px] shrink-0 items-center gap-1 border-r border-site-line px-2", index === 0 && "bg-site-sunken text-foreground")}
            >
              <span className="truncate">{tab}</span>
              {index === 2 ? <span className="ml-auto size-1.5 shrink-0 rounded-full bg-destructive" /> : null}
            </span>
          ))}
          <span className="flex items-center px-2 tabular-nums">+26</span>
        </div>
        <Mono>
          <span className="text-foreground">› Redesign the settings page</span>
          <span>Reading app/settings/layout.tsx</span>
          <span>Editing app/settings/profile.tsx</span>
          <span>Editing app/settings/billing.tsx</span>
          <span>Running tests… 86 passed</span>
          <span className="text-foreground">Done. 4 files changed.</span>
        </Mono>
      </Desk>
      <Desk style={{ right: "6%", top: "10%", width: "34%", height: "34%" }} title="infra · ci">
        <Mono>
          <span>pnpm test --filter api</span>
          <span className="text-destructive">✗ 3 tests failed</span>
          <span>auth/session.test.ts</span>
          <span>Waiting for next instruction…</span>
        </Mono>
      </Desk>
      <Desk style={{ left: "9%", top: "50%", width: "39%", height: "34%" }} title="api · export-fix — 2h idle">
        <Mono>
          <span>Wrote migrations/0042_export_jobs.sql</span>
          <span className="mt-1 rounded bg-[#fdf3c4] px-1.5 py-1 whitespace-normal! text-[#6b5a1e]">? Apply this migration to the shared database? (y/n) █</span>
        </Mono>
      </Desk>
      <Toast style={{ right: "5%", bottom: "9%", width: "36%" }} actor={LEO} app="#eng">
        Is anyone on the export bug? Customers keep asking.
      </Toast>
    </>
  );
}

type Row = { id: string; title: string; actor: Actor; working?: boolean; note?: string };

const BOARD: { title: string; status: "blocked" | "in_progress" | "in_review" | "todo"; rows: Row[] }[] = [
  { title: "Blocked", status: "blocked", rows: [{ id: "ACM-151", title: "Apply export migration", actor: BACKEND, note: "Waiting for you" }] },
  {
    title: "In progress",
    status: "in_progress",
    rows: [
      { id: "ACM-152", title: "Redesign the settings page", actor: FRONTEND, working: true },
      { id: "ACM-148", title: "Fix failing auth tests", actor: QA, working: true },
      { id: "ACM-146", title: "Speed up the CI cache", actor: LEO },
    ],
  },
  {
    title: "In review",
    status: "in_review",
    rows: [
      { id: "ACM-147", title: "Add an audit log", actor: BACKEND },
      { id: "ACM-144", title: "Document the export API", actor: CONTENT },
    ],
  },
  {
    title: "Todo",
    status: "todo",
    rows: [
      { id: "ACM-153", title: "Rate-limit the public API", actor: LEAD },
      { id: "ACM-154", title: "Triage new bug reports", actor: TRIAGE },
    ],
  },
];

export function TabsAfter() {
  return (
    <>
      <App active="issues">
        <AppHeader>
          <span className="text-label font-medium">Issues</span>
          <span className="ml-2 rounded-md bg-accent px-2 py-0.5 text-caption">All</span>
          <span className="px-2 py-0.5 text-caption text-muted-foreground">Agents</span>
          <span className="px-2 py-0.5 text-caption text-muted-foreground">Members</span>
        </AppHeader>
        {BOARD.map((group) => (
          <Fragment key={group.title}>
            <GroupBand title={group.title} count={group.rows.length} />
            {group.rows.map((row) => (
              <div key={row.id} className="flex h-9 shrink-0 items-center gap-2.5 border-b border-border px-4">
                <StatusIcon status={group.status} className="h-3.5 w-3.5 shrink-0" />
                <span className="w-[56px] shrink-0 whitespace-nowrap text-caption text-muted-foreground tabular-nums">{row.id}</span>
                <span className="min-w-0 flex-1 truncate text-label">{row.title}</span>
                {row.working ? <Working /> : null}
                {row.note ? <span className="shrink-0 whitespace-nowrap text-caption font-medium text-warning">{row.note}</span> : null}
                <Avatar actor={row.actor} size={20} />
              </div>
            ))}
          </Fragment>
        ))}
      </App>
      <Card style={{ left: "5%", top: "34%", width: "40%" }}>
        <p className="flex items-center gap-2 text-caption text-muted-foreground">
          <Inbox className="size-3.5" /> Inbox · ACM-151
        </p>
        <div className="mt-2 flex gap-2">
          <Avatar actor={BACKEND} size={20} />
          <p className="text-caption">
            <span className="font-medium">Backend Agent</span> asks: apply the export migration to the shared database?
          </p>
        </div>
        <div className="mt-2.5 flex gap-1.5 pl-7">
          <span className="rounded-md bg-foreground px-2 py-1 text-caption font-medium text-background">Approve</span>
          <span className="rounded-md border border-border px-2 py-1 text-caption">Reply</span>
        </div>
      </Card>
      <Card style={{ left: "9%", top: "68%", width: "34%" }}>
        <p className="flex items-center gap-2 text-caption">
          <Avatar actor={QA} size={16} />
          <span className="min-w-0 truncate">
            <span className="font-medium">QA Agent</span> <span className="text-muted-foreground">moved ACM-148 to</span> In review
          </span>
        </p>
      </Card>
    </>
  );
}

// --- 2. The work nobody has time to do -----------------------------------------

const CONVERSATIONS: { customer: string; topic: string; reviewed?: boolean }[] = [
  { customer: "Globex", topic: "Export stuck at 90%", reviewed: true },
  { customer: "Initech", topic: "Can't log in on Safari" },
  { customer: "Umbrella", topic: "Invoice shows wrong VAT" },
  { customer: "Hooli", topic: "Export times out", reviewed: true },
  { customer: "Stark Ind.", topic: "How do I add a seat?" },
  { customer: "Wayne Co.", topic: "Login loop after reset" },
  { customer: "Soylent", topic: "CSV is missing columns" },
  { customer: "Cyberdyne", topic: "Refund not received" },
  { customer: "Tyrell", topic: "SSO setup question" },
  { customer: "Massive", topic: "Export stuck again" },
  { customer: "Vandelay", topic: "Change billing email" },
  { customer: "Oscorp", topic: "Login loop on Safari" },
];

const SHEET = "grid grid-cols-[22px_72px_minmax(0,1fr)_56px]";

export function DigestBefore() {
  return (
    <>
      <Desk style={{ left: "4%", top: "8%", width: "44%", bottom: "6%" }} title="Support review · Oct 25.xlsx">
        <div className={cn(SHEET, "border-b border-site-line bg-site-sunken text-caption font-medium")}>
          {["", "Customer", "Topic", "Checked"].map((cell, index) => (
            <span key={index} className="border-r border-site-line px-2 py-1">
              {cell}
            </span>
          ))}
        </div>
        {CONVERSATIONS.map((row, index) => (
          <div key={row.customer} className={cn(SHEET, "border-b border-site-line text-caption")}>
            <span className="border-r border-site-line bg-site-sunken px-1 py-1 text-center text-muted-foreground tabular-nums">{index + 2}</span>
            <span className="truncate border-r border-site-line px-2 py-1">{row.customer}</span>
            <span className="truncate border-r border-site-line px-2 py-1 text-muted-foreground">{row.topic}</span>
            <span className="truncate border-r border-site-line px-2 py-1 text-muted-foreground">{row.reviewed ? "✓ Sam" : "—"}</span>
          </div>
        ))}
      </Desk>
      <Desk style={{ right: "6%", top: "10%", width: "34%" }} title="Support inbox">
        <div className="flex flex-col p-2.5">
          <p className="flex items-baseline gap-1.5">
            <span className="text-title-sm font-semibold tabular-nums">2,418</span>
            <span className="text-caption text-muted-foreground">unread</span>
          </p>
          {["Export stuck at 90%", "Refund not received", "SSO setup question"].map((subject) => (
            <p key={subject} className="mt-1.5 flex items-center gap-1.5 text-caption">
              <span className="size-1.5 shrink-0 rounded-full bg-site-accent" />
              <span className="truncate">{subject}</span>
            </p>
          ))}
        </div>
      </Desk>
      <div className={NOTE} style={{ right: "12%", top: "50%", width: "24%", rotate: "2deg" }}>
        Spot-check 40 a day.
        <br />
        Rest: no time.
      </div>
      <Toast style={{ right: "5%", bottom: "9%", width: "36%" }} actor={SAM} app="DM">
        I can’t keep up with the queue this week.
      </Toast>
    </>
  );
}

const THEMES: { title: string; count: number; outcome: ReactNode }[] = [
  { title: "Seat and billing questions", count: 640, outcome: "Answered" },
  { title: "Exports time out", count: 312, outcome: <Filed id="ACM-151" /> },
  { title: "Login loop on Safari", count: 188, outcome: <Filed id="ACM-153" /> },
  { title: "Refunds pending", count: 96, outcome: <Filed id="ACM-155" /> },
];

const RUNS = [1980, 2210, 2054, 2301, 2160, 2392, 2418];

function Filed({ id }: { id: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      <StatusIcon status="todo" className="h-3 w-3" /> {id}
    </span>
  );
}

export function DigestAfter() {
  const max = THEMES[0]!.count;
  const peak = Math.max(...RUNS);
  return (
    <>
      <Card style={{ left: "5%", top: "12%", width: "40%" }}>
        <p className="flex items-center gap-2">
          <Zap className="size-3.5 text-muted-foreground" />
          <span className="text-label font-medium">Daily support digest</span>
          <span className="ml-auto flex h-4 w-7 shrink-0 items-center rounded-full bg-brand p-0.5">
            <span className="ml-auto size-3 rounded-full bg-background" />
          </span>
        </p>
        <p className="mt-1.5 flex items-center gap-1.5 text-caption text-muted-foreground tabular-nums">
          Every day · 02:00 · <Avatar actor={CONTENT} size={16} /> Content Agent
        </p>
      </Card>
      <Card style={{ left: "5%", top: "40%", width: "40%" }}>
        <p className="flex text-caption">
          <span className="font-medium">Conversations read</span>
          <span className="ml-auto text-muted-foreground">Last 7 runs</span>
        </p>
        <div className="mt-2 flex h-12 items-end gap-1.5">
          {RUNS.map((count, index) => (
            <span
              key={index}
              className={cn("flex-1 rounded-sm", index === RUNS.length - 1 ? "bg-brand" : "bg-muted")}
              style={{ height: `${(count / peak) * 100}%` }}
            />
          ))}
        </div>
      </Card>
      <App active="autopilots">
        <AppHeader>
          <span className="flex min-w-0 items-center gap-1.5 truncate text-caption text-muted-foreground">
            Support digest <ChevronRight className="size-3 shrink-0" />
            <span className="text-foreground">Oct 25</span>
          </span>
        </AppHeader>
        <div className="flex flex-col gap-3 p-4">
          <div className="flex gap-2.5">
            <Avatar actor={CONTENT} size={24} />
            <div className="min-w-0 flex-1">
              <p className="flex items-baseline gap-2">
                <span className="text-label font-semibold">{CONTENT.name}</span>
                <span className="text-caption text-muted-foreground tabular-nums">02:41</span>
              </p>
              <p className="mt-0.5 text-label">Read all 2,418 conversations from yesterday.</p>
            </div>
          </div>
          <ul className="flex flex-col gap-2.5">
            {THEMES.map((theme) => (
              <li key={theme.title} className="flex flex-col gap-1 text-caption">
                <span className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate">{theme.title}</span>
                  <span className="shrink-0 text-muted-foreground">{theme.outcome}</span>
                </span>
                <span className="flex items-center gap-1.5">
                  <span className="h-1.5 rounded-full bg-brand" style={{ width: `${(theme.count / max) * 80}%` }} />
                  <span className="tabular-nums text-muted-foreground">{theme.count}</span>
                </span>
              </li>
            ))}
          </ul>
          <div className="flex items-center gap-2 border-t border-border pt-3 text-caption text-muted-foreground">
            <Avatar actor={MIA} size={16} />
            <span className="text-foreground">{MIA.name}</span> reviewed · 09:12
          </div>
        </div>
      </App>
    </>
  );
}

// --- 3. A process that waits on people -----------------------------------------

const DAYS = ["M", "T", "W", "T", "F", "M", "T", "W", "T"];
const WAIT = "repeating-linear-gradient(135deg, var(--site-line-strong) 0 1px, transparent 1px 5px)";

const TRACK: { step: string; owner: Actor; wait?: [number, number]; work: [number, number] }[] = [
  { step: "Analyze", owner: MIA, work: [0, 1] },
  { step: "Fix", owner: LEO, wait: [1, 2], work: [2, 5] },
  { step: "Test", owner: SAM, wait: [5, 6], work: [6, 7] },
  { step: "Release", owner: SAM, wait: [7, 8], work: [8, 9] },
];

function Bar({ span, waiting }: { span: [number, number]; waiting?: boolean }) {
  return (
    <span
      className={cn("absolute inset-y-1.5 rounded-sm", !waiting && "bg-site-ink-secondary")}
      style={{
        left: `${(span[0] / DAYS.length) * 100}%`,
        width: `${((span[1] - span[0]) / DAYS.length) * 100}%`,
        backgroundImage: waiting ? WAIT : undefined,
      }}
    />
  );
}

export function PipelineBefore() {
  return (
    <>
      <Desk style={{ left: "4%", top: "10%", width: "44%" }} title="Refund fix · tracker">
        <div className="p-3">
          <div className="grid grid-cols-[76px_1fr] text-caption text-muted-foreground">
            <span />
            <span className="grid grid-cols-9 text-center">
              {DAYS.map((day, index) => (
                <span key={index}>{day}</span>
              ))}
            </span>
          </div>
          {TRACK.map((row) => (
            <div key={row.step} className="mt-1 grid grid-cols-[76px_1fr] items-center text-caption">
              <span className="flex items-center gap-1.5">
                <Avatar actor={row.owner} size={16} /> {row.step}
              </span>
              <span className="relative h-6 rounded-sm bg-site-sunken">
                {row.wait ? <Bar span={row.wait} waiting /> : null}
                <Bar span={row.work} />
              </span>
            </div>
          ))}
          <p className="mt-2.5 flex items-center gap-3 text-caption text-muted-foreground">
            <span className="flex items-center gap-1.5">
              <span className="h-2 w-4 rounded-sm bg-site-ink-secondary" /> Work
            </span>
            <span className="flex items-center gap-1.5">
              <span className="h-2 w-4 rounded-sm" style={{ backgroundImage: WAIT }} /> Waiting
            </span>
          </p>
          <p className="mt-2 border-t border-site-line pt-2 text-caption font-medium">9 working days, 4 spent waiting</p>
        </div>
      </Desk>
      <Desk style={{ right: "5%", top: "9%", width: "38%" }} title="# risk-fixes">
        <div className="flex flex-col gap-2 p-2.5 text-caption">
          {[
            { actor: MIA, day: "Mon", text: "Found the cause. @Leo can you take the fix?" },
            { actor: LEO, day: "Wed", text: "Just saw this, starting now." },
            { actor: LEO, day: "Fri", text: "Fix is in. Who can run the evals?" },
          ].map((message) => (
            <div key={message.day} className="flex gap-2">
              <Avatar actor={message.actor} size={16} />
              <p className="min-w-0">
                <span className="font-medium">{message.actor.name}</span> <span className="text-muted-foreground">{message.day}</span>
                <span className="block text-muted-foreground">{message.text}</span>
              </p>
            </div>
          ))}
        </div>
      </Desk>
      <Toast style={{ left: "10%", top: "64%", width: "36%" }} actor={SAM} app="Calendar">
        Next release slot: Thursday next week.
      </Toast>
    </>
  );
}

const STAGES: { step: string; actor: Actor; when: string; human?: boolean }[] = [
  { step: "Analyze", actor: RESEARCH, when: "Day 1" },
  { step: "Fix", actor: BACKEND, when: "Day 1" },
  { step: "Test", actor: QA, when: "Day 2" },
  { step: "Approve", actor: MIA, when: "Day 3", human: true },
  { step: "Ship", actor: BACKEND, when: "Day 3" },
];

const ACTIVITY: { actor: Actor; when: string; text: ReactNode }[] = [
  { actor: RESEARCH, when: "Day 1 · 09:20", text: "Found the cause and handed the issue to Fix." },
  { actor: BACKEND, when: "Day 1 · 15:05", text: "Opened a pull request. Checks are passing." },
  { actor: QA, when: "Day 2 · 11:40", text: "Ran 1,204 test cases with no regressions." },
  {
    actor: MIA,
    when: "Day 3 · 10:30",
    text: (
      <span className="inline-flex items-center gap-1.5 font-medium text-foreground">
        <Check className="size-3 text-success" /> Approved the release
      </span>
    ),
  },
];

export function PipelineAfter() {
  return (
    <>
      <Card style={{ left: "5%", top: "22%", width: "40%" }}>
        <p className="text-caption font-medium">Cycle time</p>
        {[
          { label: "This fix", days: 3, current: true },
          { label: "Before workflows", days: 9 },
        ].map((row) => (
          <div key={row.label} className="mt-2 flex flex-col gap-1 text-caption">
            <span className="flex">
              <span className={cn(!row.current && "text-muted-foreground")}>{row.label}</span>
              <span className="ml-auto tabular-nums text-muted-foreground">{row.days} days</span>
            </span>
            <span className={cn("h-1.5 rounded-full", row.current ? "bg-brand" : "bg-muted")} style={{ width: `${(row.days / 9) * 100}%` }} />
          </div>
        ))}
      </Card>
      <Card style={{ left: "9%", top: "58%", width: "34%" }}>
        <div className="flex items-center gap-2 text-caption">
          <Avatar actor={MIA} size={16} />
          <span className="font-medium">Mia Chen</span>
          <span className="text-muted-foreground">approved the release</span>
          <Check className="ml-auto size-3.5 shrink-0 text-success" />
        </div>
      </Card>
      <App active="issues">
      <AppHeader>
        <span className="text-caption text-muted-foreground tabular-nums">ACM-160</span>
        <span className="truncate text-label font-medium">Refunds flagged as fraud</span>
        <span className="ml-auto flex shrink-0 items-center gap-1.5 rounded-full border border-border px-2 py-0.5 text-caption">
          <StatusIcon status="done" className="h-3 w-3" /> Shipped
        </span>
      </AppHeader>
      <div className="flex flex-col gap-4 p-4">
        <ol className="flex items-start">
          {STAGES.map((stage, index) => (
            <li key={stage.step} className="relative flex flex-1 flex-col items-center gap-1 text-center">
              {index > 0 ? <span className="absolute top-3 right-1/2 left-[-50%] h-px bg-brand" /> : null}
              <span className={cn("relative rounded-full ring-4 ring-background", stage.human && "ring-brand/20")}>
                <Avatar actor={stage.actor} size={24} />
              </span>
              <span className="text-caption font-medium">{stage.step}</span>
              <span className="text-micro text-muted-foreground">{stage.when}</span>
            </li>
          ))}
        </ol>
        <div className="flex flex-col gap-2.5 border-t border-border pt-3">
          {ACTIVITY.map((item) => (
            <div key={item.when} className="flex gap-2.5">
              <Avatar actor={item.actor} size={20} />
              <div className="min-w-0 text-caption">
                <span className="font-medium">{item.actor.name}</span> <span className="text-muted-foreground tabular-nums">{item.when}</span>
                <span className="block text-muted-foreground">{item.text}</span>
              </div>
            </div>
          ))}
        </div>
      </div>
    </App>
    </>
  );
}
