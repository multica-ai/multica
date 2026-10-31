"use client";

import { Fragment, type CSSProperties, type ReactNode } from "react";
import {
  Bot,
  Check,
  ChevronRight,
  CircleUser,
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
  AppWindow,
  Avatar,
  FLOATING,
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
 * (see ZoomCanvas), so windows are placed in percentages and their contents
 * either overflow (logs pin to the bottom, lists run past the edge) or size
 * the window, never leaving a window half empty. Data is fictional.
 */

export const STAGE_CANVAS = { width: 600, height: 400 } as const;

// --- Before: plain desktop windows -------------------------------------------

function Desk({ className, style, title, children }: { className?: string; style?: CSSProperties; title: ReactNode; children?: ReactNode }) {
  return (
    <div
      className={cn("flex flex-col overflow-hidden rounded-lg border border-site-line-strong bg-site-surface shadow-site-float", className)}
      style={style}
    >
      <div className="flex h-7 shrink-0 items-center gap-1.5 border-b border-site-line bg-site-sunken px-2.5">
        {[0, 1, 2].map((dot) => (
          <span key={dot} className="size-2 shrink-0 rounded-full bg-site-line-strong" />
        ))}
        <span className="ml-1.5 min-w-0 truncate text-caption text-muted-foreground">{title}</span>
      </div>
      {children}
    </div>
  );
}

/** Terminal or chat output, pinned to the bottom like a scrolled window: older lines run off the top. */
function Log({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn("flex min-h-0 flex-1 flex-col justify-end gap-1 overflow-hidden p-3 font-mono text-caption text-muted-foreground *:shrink-0 *:truncate", className)}>
      {children}
    </div>
  );
}

/** The notification column down the right edge of a "before" desktop; the last child fills what's left. */
function Notifications({ children }: { children: ReactNode }) {
  return <div className="absolute top-[5%] right-[4%] bottom-[7%] flex w-[38%] flex-col gap-2 *:shrink-0 *:last:min-h-0 *:last:shrink">{children}</div>;
}

function Toast({ actor, app, when = "now", children }: { actor: Actor; app: string; when?: string; children: ReactNode }) {
  return (
    <div className="flex gap-2 rounded-xl border border-site-line-strong bg-site-surface p-2.5 shadow-site-float">
      <Avatar actor={actor} size={20} />
      <div className="min-w-0 flex-1 text-caption">
        <p className="flex items-center gap-1.5">
          <span className="font-medium">{actor.name}</span>
          <span className="text-muted-foreground">{app}</span>
          <span className="ml-auto text-muted-foreground">{when}</span>
        </p>
        <p className="mt-0.5 truncate text-muted-foreground">{children}</p>
      </div>
    </div>
  );
}

const NOTE = "absolute whitespace-nowrap rounded-sm bg-[#fdf3c4] px-3 py-2.5 font-mono text-caption text-[#6b5a1e] shadow-site-float";
const PROMPT = "rounded bg-[#fdf3c4] px-1.5 py-1 whitespace-normal! text-[#6b5a1e]";

// --- After: the Multica app ---------------------------------------------------

function App({ active, children }: { active: string; children: ReactNode }) {
  return (
    <AppWindow className="top-[12%] right-[4%] -bottom-6 left-[52%]" active={active}>
      {children}
    </AppWindow>
  );
}

function AppHeader({ children }: { children: ReactNode }) {
  return <div className="flex h-11 shrink-0 items-center gap-2 border-b border-border px-4">{children}</div>;
}

/** The column of floating Multica cards in the left half, top-aligned with the app window. */
function Side({ children }: { children: ReactNode }) {
  return <div className="absolute top-[12%] left-[5%] flex w-[40%] flex-col gap-3">{children}</div>;
}

function Card({ icon: Icon, title, aside, children }: { icon?: LucideIcon; title: ReactNode; aside?: ReactNode; children: ReactNode }) {
  return (
    <div className={cn(FLOATING, "p-3")}>
      <p className="flex items-center gap-1.5 text-caption text-muted-foreground">
        {Icon ? <Icon className="size-3.5 shrink-0" /> : null}
        <span className="min-w-0 truncate">{title}</span>
        {aside ? <span className="ml-auto shrink-0">{aside}</span> : null}
      </p>
      {children}
    </div>
  );
}

function Button({ primary, children }: { primary?: boolean; children: ReactNode }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-md px-2 py-1 text-caption font-medium",
        primary ? "bg-foreground text-background" : "border border-border",
      )}
    >
      {children}
    </span>
  );
}

function Ask({ actor, children }: { actor: Actor; children: ReactNode }) {
  return (
    <div className="mt-2 flex gap-2">
      <Avatar actor={actor} size={20} />
      <p className="min-w-0 text-caption">
        <span className="font-medium">{actor.name}</span> {children}
      </p>
    </div>
  );
}

// --- 1. Thirty agents, thirty tabs ---------------------------------------------

const TABS = ["web", "api", "infra", "docs"];

export function TabsBefore() {
  return (
    <>
      {["docs · changelog", "api · billing"].map((title, index) => (
        <Desk
          key={title}
          title={`zsh — ${title}`}
          className="absolute"
          style={{ left: `calc(4% + ${(2 - index) * 14}px)`, top: `calc(14% - ${(2 - index) * 26}px)`, width: "42%", height: "36%" }}
        />
      ))}
      <Desk className="absolute" style={{ left: "4%", top: "14%", width: "42%", height: "36%" }} title="zsh — 30 sessions">
        <div className="flex h-7 shrink-0 items-stretch border-b border-site-line text-caption text-muted-foreground">
          {TABS.map((tab, index) => (
            <span
              key={tab}
              className={cn("flex items-center gap-1.5 border-r border-site-line px-2.5", index === 0 && "bg-site-sunken text-foreground")}
            >
              {tab}
              {tab === "infra" ? <span className="size-1.5 shrink-0 rounded-full bg-destructive" /> : null}
            </span>
          ))}
          <span className="flex items-center px-2.5 tabular-nums">+26</span>
        </div>
        <Log>
          <span>✓ Updated 12 snapshots</span>
          <span className="text-foreground">› Add a dark theme to settings</span>
          <span>Editing app/settings/theme.ts</span>
          <span>Done. 2 files changed.</span>
          <span className="text-foreground">› Redesign the settings page</span>
          <span>Reading app/settings/layout.tsx</span>
          <span>Editing app/settings/profile.tsx</span>
          <span>Editing app/settings/billing.tsx</span>
          <span>Editing app/settings/team.tsx</span>
          <span>Running tests… 86 passed</span>
          <span className="text-foreground">Done. 4 files changed.</span>
          <span className="text-foreground">› █</span>
        </Log>
      </Desk>
      <Desk className="absolute" style={{ left: "9%", top: "54%", width: "39%" }} title="zsh — api · export-fix — idle 2h">
        <Log>
          <span className="text-foreground">› Fix the export timeout</span>
          <span>Reading src/export/jobs.ts</span>
          <span>Reading src/export/queue.ts</span>
          <span>Wrote migrations/0042_export_jobs.sql</span>
          <span className={PROMPT}>? Apply this migration to the shared database? (y/n) █</span>
        </Log>
      </Desk>
      <Notifications>
        <Toast actor={LEO} app="#eng">
          Is anyone on the export bug? Customers keep asking.
        </Toast>
        <Toast actor={MIA} app="DM" when="12m">
          Did the export migration go out?
        </Toast>
        <Desk title="zsh — infra · ci">
          <Log>
            <span className="text-foreground">› Fix the flaky auth tests</span>
            <span>Reading auth/session.ts</span>
            <span>Reading auth/session.test.ts</span>
            <span>Editing auth/session.ts</span>
            <span>Editing auth/logout.ts</span>
            <span className="text-foreground">› pnpm test --filter api</span>
            <span>PASS billing/invoice.test.ts</span>
            <span>PASS billing/seats.test.ts</span>
            <span>PASS export/jobs.test.ts</span>
            <span>PASS export/queue.test.ts</span>
            <span>PASS auth/token.test.ts</span>
            <span>PASS auth/refresh.test.ts</span>
            <span className="text-destructive">FAIL auth/session.test.ts</span>
            <span className="text-destructive">FAIL auth/logout.test.ts</span>
            <span className="text-destructive">FAIL auth/sso.test.ts</span>
            <span className="text-destructive">✗ 3 tests failed</span>
            <span>Waiting for next instruction…</span>
          </Log>
        </Desk>
      </Notifications>
    </>
  );
}

type Row = { id: string; title: string; actor: Actor; working?: boolean; note?: string };

const BOARD: { title: string; status: "blocked" | "in_progress" | "in_review" | "todo" | "done"; rows: Row[] }[] = [
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
  {
    title: "Done",
    status: "done",
    rows: [
      { id: "ACM-143", title: "Add a dark theme to settings", actor: FRONTEND },
      { id: "ACM-141", title: "Update test snapshots", actor: QA },
    ],
  },
];

const AGENT_COUNTS = [
  { count: 12, label: "Working", dot: "bg-foreground" },
  { count: 1, label: "Waiting", dot: "bg-warning" },
  { count: 17, label: "Idle", dot: "bg-border" },
];

export function TabsAfter() {
  return (
    <>
      <Side>
        <Card icon={Inbox} title="Inbox · ACM-151" aside={<span className="block size-1.5 rounded-full bg-brand" />}>
          <Ask actor={BACKEND}>asks: apply the export migration to the shared database?</Ask>
          <div className="mt-2.5 flex gap-1.5 pl-7">
            <Button primary>Approve</Button>
            <Button>Reply</Button>
          </div>
        </Card>
        <Card icon={Bot} title="Agents" aside={<span className="tabular-nums">30</span>}>
          <div className="mt-2.5 grid grid-cols-3 gap-2">
            {AGENT_COUNTS.map((item) => (
              <div key={item.label}>
                <p className="text-title-sm font-semibold tabular-nums">{item.count}</p>
                <p className="flex items-center gap-1.5 text-caption text-muted-foreground">
                  <span className={cn("size-1.5 shrink-0 rounded-full", item.dot)} />
                  {item.label}
                </p>
              </div>
            ))}
          </div>
        </Card>
      </Side>
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
  { customer: "Wonka", topic: "Seats don’t match invoice", reviewed: true },
  { customer: "Gekko", topic: "Export missing rows" },
  { customer: "Aperture", topic: "Can't reset password" },
  { customer: "Monarch", topic: "Refund still pending" },
  { customer: "Cogswell", topic: "Add a second admin" },
  { customer: "Sterling", topic: "Export times out" },
  { customer: "Gringotts", topic: "Invoice in wrong currency" },
  { customer: "Prestige", topic: "Login loop on Safari" },
];

const SHEET = "grid grid-cols-[24px_76px_minmax(0,1fr)_64px]";

const UNREAD = [
  { subject: "Export stuck at 90%", when: "2m" },
  { subject: "Refund not received", when: "4m" },
  { subject: "SSO setup question", when: "9m" },
  { subject: "Login loop on Safari", when: "11m" },
  { subject: "Invoice shows wrong VAT", when: "18m" },
  { subject: "Export times out", when: "22m" },
  { subject: "Can't reset password", when: "31m" },
  { subject: "Add a second admin", when: "40m" },
  { subject: "Seats don’t match invoice", when: "52m" },
  { subject: "Export missing rows", when: "1h" },
  { subject: "Refund still pending", when: "1h" },
  { subject: "Invoice in wrong currency", when: "2h" },
  { subject: "How do I add a seat?", when: "2h" },
  { subject: "CSV is missing columns", when: "3h" },
];

export function DigestBefore() {
  return (
    <>
      <Desk className="absolute" style={{ left: "4%", top: "8%", width: "44%", bottom: "6%" }} title="Support review · Oct 25.xlsx">
        <div className={cn(SHEET, "shrink-0 border-b border-site-line bg-site-sunken text-caption font-medium")}>
          {["", "Customer", "Topic", "Checked"].map((cell, index) => (
            <span key={index} className="truncate border-r border-site-line px-2 py-1 last:border-r-0">
              {cell}
            </span>
          ))}
        </div>
        <div className="min-h-0 flex-1 overflow-hidden">
          {CONVERSATIONS.map((row, index) => (
            <div key={row.customer} className={cn(SHEET, "border-b border-site-line text-caption")}>
              <span className="border-r border-site-line bg-site-sunken px-1 py-1 text-center text-muted-foreground tabular-nums">{index + 2}</span>
              <span className="truncate border-r border-site-line px-2 py-1">{row.customer}</span>
              <span className="truncate border-r border-site-line px-2 py-1 text-muted-foreground">{row.topic}</span>
              <span className={cn("truncate px-2 py-1", row.reviewed ? "text-foreground" : "text-muted-foreground")}>{row.reviewed ? "✓ Sam" : "—"}</span>
            </div>
          ))}
        </div>
        <div className="flex h-7 shrink-0 items-stretch border-t border-site-line bg-site-sunken text-caption text-muted-foreground">
          {["Oct 24", "Oct 25"].map((tab, index) => (
            <span key={tab} className={cn("flex shrink-0 items-center border-r border-site-line px-2.5 whitespace-nowrap", index === 1 && "bg-site-surface text-foreground")}>
              {tab}
            </span>
          ))}
          <span className="ml-auto min-w-0 truncate self-center px-2.5 tabular-nums">40 of 2,418 checked</span>
        </div>
      </Desk>
      <Notifications>
        <Toast actor={SAM} app="DM">
          I can’t keep up with the queue this week.
        </Toast>
        <Desk title="Support inbox">
          <p className="flex shrink-0 items-baseline gap-1.5 border-b border-site-line px-3 py-2">
            <span className="text-title-sm font-semibold tabular-nums">2,418</span>
            <span className="text-caption text-muted-foreground">unread</span>
          </p>
          <div className="min-h-0 flex-1 overflow-hidden">
            {UNREAD.map((item) => (
              <p key={item.subject} className="flex items-center gap-2 border-b border-site-line px-3 py-1.5 text-caption">
                <span className="size-1.5 shrink-0 rounded-full bg-brand" />
                <span className="min-w-0 flex-1 truncate">{item.subject}</span>
                <span className="shrink-0 text-muted-foreground tabular-nums">{item.when}</span>
              </p>
            ))}
          </div>
        </Desk>
      </Notifications>
      <div className={NOTE} style={{ right: "8%", bottom: "11%", rotate: "-2deg" }}>
        Spot-check 40 a day.
        <br />
        Rest: no time.
      </div>
    </>
  );
}

const THEMES: { title: string; count: number; filed?: boolean }[] = [
  { title: "Seat and billing questions", count: 640 },
  { title: "Exports time out", count: 312, filed: true },
  { title: "Login loop on Safari", count: 188, filed: true },
  { title: "Refunds pending", count: 96, filed: true },
];

const FILED: { id: string; title: string; actor: Actor }[] = [
  { id: "ACM-151", title: "Exports time out for large accounts", actor: BACKEND },
  { id: "ACM-153", title: "Login loop on Safari after reset", actor: FRONTEND },
  { id: "ACM-155", title: "Refunds stuck in pending", actor: LEAD },
];

const RUNS = [
  { day: "Oct 25", read: "2,418", filed: 3 },
  { day: "Oct 24", read: "2,392", filed: 2 },
  { day: "Oct 23", read: "2,160", filed: 4 },
  { day: "Oct 22", read: "2,301", filed: 1 },
];

export function DigestAfter() {
  const max = THEMES[0]!.count;
  return (
    <>
      <Side>
        <Card
          icon={Zap}
          title="Autopilot"
          aside={
            <span className="flex h-4 w-7 items-center rounded-full bg-brand p-0.5">
              <span className="ml-auto size-3 rounded-full bg-background" />
            </span>
          }
        >
          <p className="mt-1.5 text-label font-medium">Daily support digest</p>
          <p className="mt-1 flex items-center gap-1.5 text-caption text-muted-foreground tabular-nums">
            Every day · 02:00 · <Avatar actor={CONTENT} size={16} /> {CONTENT.name}
          </p>
        </Card>
        <Card title="Recent runs">
          <div className="mt-1.5 flex flex-col">
            {RUNS.map((run) => (
              <p key={run.day} className="flex items-center gap-2 border-b border-border py-1.5 text-caption last:border-b-0 last:pb-0">
                <Check className="size-3 shrink-0 text-success" />
                <span className="w-11 shrink-0">{run.day}</span>
                <span className="text-muted-foreground tabular-nums">{run.read} read</span>
                <span className="ml-auto text-muted-foreground tabular-nums">{run.filed} filed</span>
              </p>
            ))}
          </div>
        </Card>
      </Side>
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
                  <span className="shrink-0 text-muted-foreground">{theme.filed ? "Issue filed" : "Answered"}</span>
                </span>
                <span className="flex items-center gap-1.5">
                  <span className="h-1.5 rounded-full bg-brand" style={{ width: `${(theme.count / max) * 80}%` }} />
                  <span className="tabular-nums text-muted-foreground">{theme.count}</span>
                </span>
              </li>
            ))}
          </ul>
          <div className="flex flex-col border-t border-border pt-1">
            {FILED.map((issue) => (
              <div key={issue.id} className="flex h-8 items-center gap-2.5 text-caption">
                <StatusIcon status="todo" className="h-3.5 w-3.5 shrink-0" />
                <span className="w-[56px] shrink-0 whitespace-nowrap text-muted-foreground tabular-nums">{issue.id}</span>
                <span className="min-w-0 flex-1 truncate">{issue.title}</span>
                <Avatar actor={issue.actor} size={20} />
              </div>
            ))}
          </div>
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

const CHECKLIST: { item: string; note: string; done?: boolean }[] = [
  { item: "Find the root cause", note: "Mia · Mon", done: true },
  { item: "Merge the fix", note: "Leo · Fri", done: true },
  { item: "Run the tests", note: "Waiting on Sam" },
  { item: "Sign off the release", note: "Waiting on Mia" },
  { item: "Book a release slot", note: "Waiting on Sam" },
];

const THREAD: { actor: Actor; day: string; text: string }[] = [
  { actor: MIA, day: "Mon", text: "Found the cause. @Leo can you take the fix?" },
  { actor: LEO, day: "Wed", text: "Just saw this, starting now." },
  { actor: LEO, day: "Fri", text: "Fix is in. Who can run the tests?" },
  { actor: SAM, day: "Tue", text: "Was out Monday, running them now." },
  { actor: SAM, day: "Wed", text: "Tests pass. @Mia can you sign off?" },
];

export function PipelineBefore() {
  return (
    <>
      <Desk className="absolute" style={{ left: "4%", top: "8%", width: "44%" }} title="Refund fix · tracker">
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
      <Desk className="absolute" style={{ left: "10%", top: "56%", width: "38%" }} title="Release checklist">
        <ul className="flex flex-col gap-1.5 p-3 text-caption">
          {CHECKLIST.map((row) => (
            <li key={row.item} className="flex items-center gap-2">
              <span
                className={cn(
                  "flex size-3.5 shrink-0 items-center justify-center rounded-[3px] border",
                  row.done ? "border-site-ink-secondary bg-site-ink-secondary text-site-surface" : "border-site-line-strong",
                )}
              >
                {row.done ? <Check className="size-2.5" strokeWidth={3} /> : null}
              </span>
              <span className={cn("min-w-0 flex-1 truncate", row.done && "text-muted-foreground line-through")}>{row.item}</span>
              <span className="shrink-0 text-muted-foreground">{row.note}</span>
            </li>
          ))}
        </ul>
      </Desk>
      <Notifications>
        <Toast actor={SAM} app="Calendar">
          Next release slot: Thursday next week.
        </Toast>
        <Desk title="# risk-fixes">
          <div className="flex min-h-0 flex-1 flex-col justify-end gap-2.5 overflow-hidden p-3 text-caption *:shrink-0">
            {THREAD.map((message, index) => (
              <div key={index} className="flex gap-2">
                <Avatar actor={message.actor} size={16} />
                <p className="min-w-0">
                  <span className="font-medium">{message.actor.name}</span> <span className="text-muted-foreground">{message.day}</span>
                  <span className="block truncate text-muted-foreground">{message.text}</span>
                </p>
              </div>
            ))}
          </div>
        </Desk>
      </Notifications>
    </>
  );
}

const STAGES: { step: string; actor: Actor; when: string }[] = [
  { step: "Analyze", actor: RESEARCH, when: "Day 1" },
  { step: "Fix", actor: BACKEND, when: "Day 1" },
  { step: "Test", actor: QA, when: "Day 2" },
  { step: "Approve", actor: MIA, when: "Day 3" },
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
  { actor: BACKEND, when: "Day 3 · 11:05", text: "Shipped in v2.14.1 and closed the issue." },
];

const CYCLE = [
  { label: "This fix", days: 3, current: true },
  { label: "Before workflows", days: 9 },
];

export function PipelineAfter() {
  return (
    <>
      <Side>
        <Card icon={Inbox} title="Inbox · ACM-160" aside="Day 3">
          <Ask actor={QA}>
            <span className="text-muted-foreground">Tests pass. Ready to ship the refund fix?</span>
          </Ask>
          <div className="mt-2.5 flex items-center gap-1.5 pl-7">
            <Button primary>
              <Check className="size-3" /> Approved
            </Button>
            <span className="text-caption text-muted-foreground">by {MIA.name}</span>
          </div>
        </Card>
        <Card title="Cycle time">
          {CYCLE.map((row) => (
            <div key={row.label} className="mt-2 flex flex-col gap-1 text-caption">
              <span className="flex">
                <span className={cn(!row.current && "text-muted-foreground")}>{row.label}</span>
                <span className="ml-auto tabular-nums text-muted-foreground">{row.days} days</span>
              </span>
              <span className={cn("h-1.5 rounded-full", row.current ? "bg-brand" : "bg-muted")} style={{ width: `${(row.days / 9) * 100}%` }} />
            </div>
          ))}
        </Card>
      </Side>
      <App active="issues">
        <AppHeader>
          <span className="text-caption text-muted-foreground tabular-nums">ACM-160</span>
          <span className="truncate text-label font-medium">Refunds flagged as fraud</span>
          <span className="ml-auto flex shrink-0 items-center gap-1.5 rounded-full border border-border px-2 py-0.5 text-caption">
            <StatusIcon status="done" className="h-3 w-3" /> Shipped
          </span>
        </AppHeader>
        <div className="flex flex-col gap-4 p-4">
          <ol className="isolate flex items-start">
            {STAGES.map((stage, index) => (
              <li key={stage.step} className="relative flex flex-1 flex-col items-center gap-1 text-center">
                {index > 0 ? <span className="absolute top-3 right-1/2 left-[-50%] h-px bg-brand" /> : null}
                <span className="relative z-10 flex h-6 items-center rounded-full bg-background px-1">
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
