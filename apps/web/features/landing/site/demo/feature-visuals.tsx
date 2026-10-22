"use client";

import type { ReactNode } from "react";
import {
  BookOpenText,
  ChevronRight,
  Clock,
  Cloud,
  Hash,
  Laptop,
  Monitor,
  Plus,
  Server,
  Users,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { ShimmerText } from "@multica/ui/components/common/shimmer-text";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import {
  Avatar,
  BACKEND,
  FRONTEND,
  LEAD,
  LEO,
  PROVIDER_MARKS,
  QA,
  ZoomCanvas,
  type Actor,
} from "./mock-kit";

/*
 * Static product mockups for the home page feature cards. Large cards use a
 * 520 × 390 canvas and small ones 360 × 240, matching their 4:3 and 3:2
 * frames and sized so product text lands near its real size. Each window
 * sits inside the frame's sides and runs off its bottom edge, as if the
 * product continued past the crop. Data is fictional.
 */

const LARGE = { width: 520, height: 390 } as const;
const SMALL = { width: 360, height: 240 } as const;

function Window({ className, children }: { className: string; children: ReactNode }) {
  return (
    <div
      className={cn(
        "absolute flex flex-col overflow-hidden rounded-xl border border-site-line-strong bg-background shadow-site-float",
        className,
      )}
    >
      {children}
    </div>
  );
}

function WindowHeader({ icon: Icon, title, children }: { icon: LucideIcon; title: string; children?: ReactNode }) {
  return (
    <div className="flex h-11 shrink-0 items-center gap-2 border-b border-border px-4">
      <Icon className="size-4 text-muted-foreground" />
      <span className="flex-1 text-label font-medium">{title}</span>
      {children}
    </div>
  );
}

function Working() {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="size-1.5 rounded-full bg-site-agent" />
      <ShimmerText className="text-micro text-muted-foreground">Working</ShimmerText>
    </span>
  );
}

function OutlineButton({ icon: Icon, children }: { icon?: LucideIcon; children: ReactNode }) {
  return (
    <span className="flex h-7 items-center gap-1.5 rounded-md border border-border bg-background px-2.5 text-caption">
      {Icon ? <Icon className="size-3.5 text-muted-foreground" /> : null}
      {children}
    </span>
  );
}

// --- Runtimes ---------------------------------------------------------------

const RUNTIMES: {
  name: string;
  detail: string;
  icon: LucideIcon;
  online: boolean;
  providers: string[];
  note: ReactNode;
}[] = [
  {
    name: "Mia’s MacBook Pro",
    detail: "macOS · local",
    icon: Laptop,
    online: true,
    providers: ["Claude Code", "Codex", "Cursor"],
    note: <Working />,
  },
  {
    name: "build-01",
    detail: "Linux · team server",
    icon: Server,
    online: true,
    providers: ["Claude Code", "Qwen Code", "OpenCode"],
    note: <Working />,
  },
  {
    name: "Multica Cloud",
    detail: "Sandboxes · managed",
    icon: Cloud,
    online: true,
    providers: ["Claude Code", "Codex"],
    note: <span className="text-micro text-muted-foreground">2 sandboxes</span>,
  },
];

export function RuntimesVisual() {
  return (
    <ZoomCanvas {...LARGE}>
      <Window className="inset-x-7 top-7 h-[420px]">
        <WindowHeader icon={Monitor} title="Runtimes">
          <OutlineButton icon={Plus}>Connect machine</OutlineButton>
        </WindowHeader>
        <ul className="flex flex-col">
          {RUNTIMES.map((runtime) => (
            <li key={runtime.name} className="flex items-center gap-3 border-b border-border px-4 py-3.5">
              <span className="flex size-8 items-center justify-center rounded-lg bg-muted">
                <runtime.icon className="size-4 text-muted-foreground" />
              </span>
              <div className="min-w-0 flex-1">
                <p className="flex items-center gap-2 text-label font-medium">
                  {runtime.name}
                  <span className={cn("size-1.5 rounded-full", runtime.online ? "bg-success" : "bg-muted-foreground/40")} />
                </p>
                <p className="text-caption text-muted-foreground">{runtime.detail}</p>
              </div>
              <span className="flex w-[84px] items-center gap-1">
                {runtime.providers.map((provider) => {
                  const Mark = PROVIDER_MARKS.get(provider);
                  return (
                    <span
                      key={provider}
                      className="flex size-6 items-center justify-center rounded-md border border-border bg-background"
                    >
                      {Mark ? <Mark className="size-3.5" /> : null}
                    </span>
                  );
                })}
              </span>
              <span className="w-[72px] text-right">{runtime.note}</span>
            </li>
          ))}
        </ul>
      </Window>
      <div className="absolute bottom-6 left-16 w-[280px] rounded-xl border border-site-line-strong bg-background p-4 font-mono text-micro shadow-site-float">
        <p>
          <span className="text-muted-foreground">$</span> multica setup
        </p>
        <p className="mt-2 text-muted-foreground">Found 3 coding agents on this machine</p>
        <p className="mt-1 flex items-center gap-2">
          <span className="text-success">✓</span> Claude Code · Codex · Cursor
        </p>
        <p className="mt-1 flex items-center gap-2">
          <span className="text-success">✓</span> Connected to Acme Labs
        </p>
      </div>
    </ZoomCanvas>
  );
}

// --- Squads -----------------------------------------------------------------

type SubIssue = {
  id: string;
  title: string;
  status: "todo" | "in_progress" | "done";
  assignee: Actor;
};

const STAGES: { label: string; issues: SubIssue[] }[] = [
  {
    label: "Stage 1",
    issues: [{ id: "ACM-161", title: "Design the metering schema", status: "done", assignee: BACKEND }],
  },
  {
    label: "Stage 2",
    issues: [
      { id: "ACM-162", title: "Meter API calls per workspace", status: "in_progress", assignee: BACKEND },
      { id: "ACM-163", title: "Usage chart in billing settings", status: "in_progress", assignee: FRONTEND },
    ],
  },
  {
    label: "Stage 3",
    issues: [{ id: "ACM-164", title: "End-to-end billing tests", status: "todo", assignee: QA }],
  },
];

export function SquadVisual() {
  return (
    <ZoomCanvas {...LARGE}>
      <Window className="inset-x-7 top-7 h-[420px]">
        <div className="flex h-11 shrink-0 items-center gap-1 border-b border-border px-4 text-caption text-muted-foreground">
          Acme Labs <ChevronRight className="size-3" /> <span className="text-foreground">ACM-160</span>
        </div>
        <div className="flex flex-col gap-4 px-5 py-4">
          <div>
            <p className="text-title font-semibold leading-snug">Launch usage-based billing</p>
            <p className="mt-1.5 flex items-center gap-1.5 text-caption text-muted-foreground">
              <span className="flex size-4 items-center justify-center rounded-full bg-muted">
                <Users className="size-2.5" />
              </span>
              <span className="text-foreground">Billing Squad</span> · led by
              <Avatar actor={LEAD} size={16} />
              <span className="text-foreground">{LEAD.name}</span>
            </p>
          </div>
          <div>
            <p className="mb-2 flex items-center gap-2 text-caption font-medium text-muted-foreground">
              Sub-issues <span className="tabular-nums">1/4</span>
            </p>
            <div className="flex flex-col gap-3">
              {STAGES.map((stage) => (
                <div key={stage.label}>
                  <p className="mb-1 text-micro text-muted-foreground">{stage.label}</p>
                  <ul className="overflow-hidden rounded-lg border border-border">
                    {stage.issues.map((issue) => (
                      <li
                        key={issue.id}
                        className="flex items-center gap-2.5 border-border px-3 py-2 text-caption not-first:border-t"
                      >
                        <StatusIcon status={issue.status} className="h-3.5 w-3.5" />
                        <span className="text-muted-foreground tabular-nums">{issue.id}</span>
                        <span className="min-w-0 flex-1 truncate font-medium">{issue.title}</span>
                        {issue.status === "in_progress" ? <Working /> : null}
                        <Avatar actor={issue.assignee} size={20} />
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          </div>
        </div>
      </Window>
    </ZoomCanvas>
  );
}

// --- Chat -------------------------------------------------------------------

export function ChatVisual() {
  return (
    <ZoomCanvas {...SMALL}>
      <Window className="inset-x-5 top-5 h-[260px]">
        <WindowHeader icon={Hash} title="payments" />
        <div className="flex flex-col gap-4 px-4 py-4 text-caption">
          <div className="flex gap-2.5">
            <Avatar actor={LEO} size={24} />
            <div>
              <p className="font-medium">
                Leo Park <span className="font-normal text-muted-foreground">10:42</span>
              </p>
              <p className="mt-0.5 leading-relaxed">
                EU cards are failing at checkout.{" "}
                <span className="rounded bg-brand/10 px-0.5 whitespace-nowrap text-brand">@Backend Agent</span> can you look?
              </p>
            </div>
          </div>
          <div className="flex gap-2.5">
            <Avatar actor={BACKEND} size={24} />
            <div className="min-w-0 flex-1">
              <p className="flex items-center gap-1.5 font-medium">
                Backend Agent <span className="font-normal text-muted-foreground">10:42</span>
                <span className="ml-auto">
                  <Working />
                </span>
              </p>
              <p className="mt-0.5 leading-relaxed">On it. Filed an issue, reading the logs now.</p>
              <span className="mt-2 flex items-center gap-2 rounded-md border border-border px-2.5 py-1.5">
                <StatusIcon status="in_progress" className="h-3.5 w-3.5" />
                <span className="text-muted-foreground tabular-nums">ACM-151</span>
                <span className="min-w-0 flex-1 truncate font-medium">EU checkout failures</span>
              </span>
            </div>
          </div>
        </div>
      </Window>
    </ZoomCanvas>
  );
}

// --- Autopilots -------------------------------------------------------------

const AUTOPILOTS: { name: string; schedule: string; agent: Actor; on: boolean }[] = [
  { name: "Weekly dependency upgrades", schedule: "Mondays 09:00", agent: FRONTEND, on: true },
  { name: "Triage new bug reports", schedule: "Every hour", agent: QA, on: true },
  { name: "Nightly flaky-test sweep", schedule: "Daily 02:00", agent: QA, on: true },
  { name: "Monthly usage report", schedule: "1st of the month", agent: BACKEND, on: false },
];

export function AutopilotsVisual() {
  return (
    <ZoomCanvas {...SMALL}>
      <Window className="inset-x-5 top-5 h-[260px]">
        <WindowHeader icon={Zap} title="Autopilot" />
        <ul className="flex flex-col">
          {AUTOPILOTS.map((autopilot) => (
            <li key={autopilot.name} className="flex items-center gap-3 border-b border-border px-4 py-3 text-caption">
              <div className="min-w-0 flex-1">
                <p className="truncate font-medium">{autopilot.name}</p>
                <p className="mt-0.5 flex items-center gap-1 text-muted-foreground">
                  <Clock className="size-3" />
                  {autopilot.schedule}
                </p>
              </div>
              <Avatar actor={autopilot.agent} size={20} />
              <span
                className={cn(
                  "flex h-4 w-7 items-center rounded-full p-0.5",
                  autopilot.on ? "justify-end bg-brand" : "justify-start bg-muted-foreground/30",
                )}
              >
                <span className="size-3 rounded-full bg-background" />
              </span>
            </li>
          ))}
        </ul>
      </Window>
    </ZoomCanvas>
  );
}

// --- Skills -----------------------------------------------------------------

const SKILLS: { name: string; summary: string; agents: Actor[] }[] = [
  { name: "release-checklist", summary: "Everything we run before a deploy", agents: [BACKEND, FRONTEND, QA] },
  { name: "postgres-migrations", summary: "How we write and review migrations", agents: [BACKEND, LEAD] },
  { name: "api-style-guide", summary: "Naming, errors, and pagination", agents: [BACKEND, FRONTEND] },
  { name: "bug-report-triage", summary: "Labels, priority, and who to page", agents: [QA] },
];

export function SkillsVisual() {
  return (
    <ZoomCanvas {...SMALL}>
      <Window className="inset-x-5 top-5 h-[260px]">
        <WindowHeader icon={BookOpenText} title="Skills" />
        <ul className="flex flex-col">
          {SKILLS.map((skill) => (
            <li key={skill.name} className="flex items-center gap-3 border-b border-border px-4 py-3 text-caption">
              <div className="min-w-0 flex-1">
                <p className="truncate font-mono font-medium">{skill.name}</p>
                <p className="mt-0.5 truncate text-muted-foreground">{skill.summary}</p>
              </div>
              <span className="flex -space-x-1.5">
                {skill.agents.map((agent) => (
                  <Avatar key={agent.name} actor={agent} size={20} />
                ))}
              </span>
            </li>
          ))}
        </ul>
      </Window>
    </ZoomCanvas>
  );
}
