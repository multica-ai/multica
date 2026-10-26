"use client";

import { Fragment, type ReactNode } from "react";
import { Check, ChevronRight, Hash, ListTodo, Zap } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import { GroupBand, Header } from "./feature-visuals";
import {
  Avatar,
  BACKEND,
  CONTENT,
  FRONTEND,
  LEO,
  MIA,
  QA,
  RESEARCH,
  SAM,
  Window,
  Working,
  ZoomCanvas,
  type Actor,
} from "./mock-kit";

/*
 * Paired scenes for the home page before-and-after rows. A "before" scene is
 * the tooling a team already has, drawn plain and grey so it never reads as
 * any one vendor's product; its "after" is the Multica screen that replaces
 * it. Both are cropped the same way as the feature cards and share one
 * 480 × 300 canvas. Data is fictional.
 */

const CANVAS = { width: 480, height: 300 } as const;
const CROP = "top-7 left-7 -right-12 -bottom-12";

function Scene({ children }: { children: ReactNode }) {
  return <ZoomCanvas {...CANVAS}>{children}</ZoomCanvas>;
}

/** A plain, shadowless window for the tools teams use before Multica. */
function Plain({ className, children }: { className: string; children: ReactNode }) {
  return (
    <div className={cn("absolute flex flex-col overflow-hidden rounded-lg border border-site-line-strong bg-site-surface", className)}>
      {children}
    </div>
  );
}

// --- Thirty agents, thirty tabs ----------------------------------------------

const TABS = ["api · export-fix", "web · settings", "api · auth", "infra · ci"];

export function TabsBefore() {
  return (
    <Scene>
      <Plain className={CROP}>
        <div className="flex h-8 shrink-0 items-stretch border-b border-site-line bg-site-sunken text-caption text-muted-foreground">
          {TABS.map((tab, index) => (
            <span
              key={tab}
              className={cn(
                "flex w-[104px] shrink-0 items-center gap-1.5 border-r border-site-line px-2.5",
                index === 1 && "bg-site-surface text-foreground",
              )}
            >
              <span className="truncate">{tab}</span>
              {index === 0 || index === 3 ? <span className="ml-auto size-1.5 shrink-0 rounded-full bg-site-agent" /> : null}
            </span>
          ))}
          <span className="flex items-center px-2.5 tabular-nums">+26</span>
        </div>
        <div className="flex flex-col gap-1.5 p-4 font-mono text-caption text-muted-foreground">
          <span className="text-foreground">› Redesign the settings page</span>
          <span>Reading app/settings/layout.tsx</span>
          <span>Editing app/settings/profile.tsx</span>
          <span>Editing app/settings/billing.tsx</span>
          <span>Running tests… 86 passed</span>
          <span className="text-foreground">Done. 4 files changed.</span>
        </div>
      </Plain>
      <Plain className="right-6 bottom-6 w-[236px]">
        <div className="flex items-center gap-1.5 border-b border-site-line bg-site-sunken px-2.5 py-1.5 text-caption text-muted-foreground">
          api · export-fix <span className="ml-auto">2h ago</span>
        </div>
        <p className="p-2.5 font-mono text-caption text-foreground">? Apply this migration to the shared database? (y/n)</p>
      </Plain>
    </Scene>
  );
}

type Row = { id: string; title: string; actor: Actor; status: "blocked" | "in_progress"; note?: ReactNode };

const BOARD: { title: string; status: Row["status"]; rows: Row[] }[] = [
  {
    title: "Blocked",
    status: "blocked",
    rows: [{ id: "ACM-151", title: "Apply export migration", actor: BACKEND, status: "blocked", note: "Waiting for you" }],
  },
  {
    title: "In progress",
    status: "in_progress",
    rows: [
      { id: "ACM-152", title: "Redesign the settings page", actor: FRONTEND, status: "in_progress", note: <Working /> },
      { id: "ACM-148", title: "Rotate auth tokens", actor: QA, status: "in_progress", note: <Working /> },
      { id: "ACM-146", title: "Speed up CI cache", actor: LEO, status: "in_progress" },
    ],
  },
];

export function TabsAfter() {
  return (
    <Scene>
      <Window className={CROP}>
        <Header icon={ListTodo}>
          <span className="text-label font-medium">Issues</span>
          <span className="ml-2 rounded-md bg-accent px-2 py-0.5 text-caption">All agents</span>
        </Header>
        {BOARD.map((group) => (
          <Fragment key={group.title}>
            <GroupBand title={group.title} count={group.rows.length} />
            {group.rows.map((row) => (
              <div key={row.id} className="flex h-9 items-center gap-2.5 border-b border-border px-4">
                <StatusIcon status={row.status} className="h-3.5 w-3.5 shrink-0" />
                <span className="w-[60px] shrink-0 whitespace-nowrap text-caption text-muted-foreground tabular-nums">{row.id}</span>
                <span className="w-[184px] shrink-0 truncate text-label">{row.title}</span>
                <span className="w-[92px] shrink-0">
                  {row.note ? (
                    <span className={cn("text-caption", row.status === "blocked" && "font-medium text-warning")}>{row.note}</span>
                  ) : null}
                </span>
                <Avatar actor={row.actor} size={20} />
              </div>
            ))}
          </Fragment>
        ))}
      </Window>
    </Scene>
  );
}

// --- The work nobody has time to do ------------------------------------------

const CONVERSATIONS: { customer: string; topic: string; reviewer?: string }[] = [
  { customer: "Globex", topic: "Export stuck at 90%", reviewer: "Sam" },
  { customer: "Initech", topic: "Can't log in on Safari" },
  { customer: "Umbrella", topic: "Invoice shows wrong VAT" },
  { customer: "Hooli", topic: "Export times out", reviewer: "Sam" },
  { customer: "Stark Ind.", topic: "How do I add a seat?" },
  { customer: "Wayne Co.", topic: "Login loop after reset" },
];

const SHEET_COLUMNS = "grid grid-cols-[28px_88px_168px_76px_1fr]";

export function DigestBefore() {
  return (
    <Scene>
      <Plain className={CROP}>
        <div className="flex h-9 shrink-0 items-center gap-2 border-b border-site-line px-3 text-caption">
          <span className="font-medium text-foreground">Support conversations · Oct 25</span>
          <span className="text-muted-foreground tabular-nums">2,418 rows</span>
        </div>
        <div className={cn(SHEET_COLUMNS, "border-b border-site-line bg-site-sunken text-caption text-muted-foreground")}>
          {["", "A", "B", "C", "D"].map((column, index) => (
            <span key={index} className="border-r border-site-line px-2 py-1 text-center">
              {column}
            </span>
          ))}
        </div>
        <div className={cn(SHEET_COLUMNS, "border-b border-site-line text-caption font-medium")}>
          <span className="border-r border-site-line bg-site-sunken px-2 py-1 text-center text-muted-foreground">1</span>
          <span className="border-r border-site-line px-2 py-1">Customer</span>
          <span className="border-r border-site-line px-2 py-1">Topic</span>
          <span className="border-r border-site-line px-2 py-1">Reviewed</span>
          <span />
        </div>
        {CONVERSATIONS.map((row, index) => (
          <div key={row.customer} className={cn(SHEET_COLUMNS, "border-b border-site-line text-caption")}>
            <span className="border-r border-site-line bg-site-sunken px-2 py-1 text-center text-muted-foreground tabular-nums">{index + 2}</span>
            <span className="truncate border-r border-site-line px-2 py-1">{row.customer}</span>
            <span className="truncate border-r border-site-line px-2 py-1 text-muted-foreground">{row.topic}</span>
            <span className="border-r border-site-line px-2 py-1 text-muted-foreground">{row.reviewer ? `✓ ${row.reviewer}` : "—"}</span>
            <span />
          </div>
        ))}
      </Plain>
      <Plain className="right-6 bottom-6 px-3 py-2 text-caption">
        <span className="text-muted-foreground">Reviewed</span> <span className="font-medium tabular-nums">40 of 2,418</span>
      </Plain>
    </Scene>
  );
}

const THEMES: { title: string; count: string; outcome: ReactNode }[] = [
  { title: "Exports time out", count: "312", outcome: <IssueLink id="ACM-151" /> },
  { title: "Login loop on Safari", count: "188", outcome: <IssueLink id="ACM-153" /> },
  { title: "Seat and billing questions", count: "640", outcome: "Answered" },
];

function IssueLink({ id }: { id: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      <StatusIcon status="todo" className="h-3 w-3" /> Filed {id}
    </span>
  );
}

export function DigestAfter() {
  return (
    <Scene>
      <Window className={CROP}>
        <Header icon={Zap}>
          <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
            Daily support digest <ChevronRight className="size-3" />
            <span className="text-foreground">Oct 25</span>
          </span>
        </Header>
        <div className="flex flex-col gap-3 p-4">
          <div className="flex gap-2.5">
            <Avatar actor={CONTENT} size={24} />
            <div className="min-w-0 flex-1">
              <p className="flex items-baseline gap-2">
                <span className="text-label font-semibold">{CONTENT.name}</span>
                <span className="text-caption text-muted-foreground tabular-nums">02:41 · Autopilot run</span>
              </p>
              <p className="mt-0.5 text-body">Read all 2,418 conversations from yesterday. Three themes:</p>
              <ul className="mt-2 flex w-[340px] flex-col overflow-hidden rounded-lg border border-border">
                {THEMES.map((theme) => (
                  <li key={theme.title} className="flex h-9 items-center gap-3 border-b border-border px-3 last:border-b-0">
                    <span className="w-8 shrink-0 text-caption font-medium tabular-nums">{theme.count}</span>
                    <span className="min-w-0 flex-1 truncate text-label">{theme.title}</span>
                    <span className="shrink-0 text-caption text-muted-foreground">{theme.outcome}</span>
                  </li>
                ))}
              </ul>
            </div>
          </div>
          <div className="flex items-center gap-2 pl-[34px] text-caption text-muted-foreground">
            <Avatar actor={MIA} size={16} />
            <span className="text-foreground">{MIA.name}</span> reviewed the digest · 09:12
          </div>
        </div>
      </Window>
    </Scene>
  );
}

// --- A process that waits on people ------------------------------------------

const THREAD: { day: string; actor: Actor; text: ReactNode }[] = [
  { day: "Monday", actor: MIA, text: "Found why refunds get flagged as fraud. @Leo can you pick up the fix?" },
  { day: "Wednesday", actor: LEO, text: "Just saw this, starting today." },
  { day: "Friday", actor: LEO, text: "Fix is in. Who can run the eval set?" },
  { day: "Next Thursday", actor: SAM, text: "Eval passed. Waiting for a release slot." },
];

export function PipelineBefore() {
  return (
    <Scene>
      <Plain className={CROP}>
        <div className="flex h-9 shrink-0 items-center gap-1.5 border-b border-site-line px-3 text-caption">
          <Hash className="size-3.5 text-muted-foreground" />
          <span className="font-medium">risk-fixes</span>
        </div>
        <div className="flex flex-col gap-2 px-3 py-2.5">
          {THREAD.map((message) => (
            <Fragment key={message.day}>
              <p className="flex items-center gap-2 text-caption text-muted-foreground">
                <span className="h-px flex-1 bg-site-line" />
                {message.day}
                <span className="h-px flex-1 bg-site-line" />
              </p>
              <div className="flex gap-2">
                <Avatar actor={message.actor} size={20} />
                <div className="min-w-0 text-caption">
                  <span className="font-medium">{message.actor.name}</span>
                  <p className="text-muted-foreground">{message.text}</p>
                </div>
              </div>
            </Fragment>
          ))}
        </div>
      </Plain>
    </Scene>
  );
}

const STAGES: { when: string; actor: Actor; text: string; approval?: boolean }[] = [
  { when: "Day 1 · 09:20", actor: RESEARCH, text: "Found the cause and handed it to Build" },
  { when: "Day 1 · 15:05", actor: BACKEND, text: "Opened a pull request, checks passing" },
  { when: "Day 2 · 11:40", actor: QA, text: "Ran 1,204 eval cases, no regressions" },
  { when: "Day 3 · 10:30", actor: MIA, text: "Approved the release", approval: true },
];

export function PipelineAfter() {
  return (
    <Scene>
      <Window className={CROP}>
        <Header icon={ListTodo}>
          <span className="text-caption text-muted-foreground tabular-nums">ACM-160</span>
          <span className="truncate text-label font-medium">Refunds flagged as fraud</span>
        </Header>
        <ol className="flex flex-col p-4">
          {STAGES.map((stage, index) => (
            <li key={stage.when} className="relative flex gap-3 pb-4 last:pb-0">
              {index < STAGES.length - 1 ? <span className="absolute top-6 bottom-0 left-[11px] w-px bg-border" /> : null}
              <Avatar actor={stage.actor} size={24} />
              <div className="min-w-0 flex-1">
                <p className="flex items-baseline gap-2">
                  <span className="text-label font-semibold">{stage.actor.name}</span>
                  <span className="text-caption text-muted-foreground tabular-nums">{stage.when}</span>
                </p>
                {stage.approval ? (
                  <p className="mt-1 inline-flex items-center gap-1.5 rounded-md border border-border px-2 py-0.5 text-caption font-medium">
                    <Check className="size-3 text-success" /> {stage.text}
                  </p>
                ) : (
                  <p className="text-caption text-muted-foreground">{stage.text}</p>
                )}
              </div>
            </li>
          ))}
        </ol>
      </Window>
    </Scene>
  );
}
