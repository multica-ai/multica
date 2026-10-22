"use client";

import type { ReactNode } from "react";
import {
  BookOpenText,
  Clock,
  Cloud,
  Hash,
  Laptop,
  Monitor,
  Plus,
  Server,
  Workflow,
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
  MIA,
  PROVIDER_MARKS,
  QA,
  SAM,
  ZoomCanvas,
  useStoryLoop,
  type Actor,
} from "./mock-kit";

/*
 * Product mockups for the home page feature cards. Large cards use a
 * 520 × 390 canvas and small ones 360 × 240, matching their 4:3 and 3:2
 * frames and sized so product text lands near its real size. Each window
 * sits inside the frame's sides and runs off its bottom edge, as if the
 * product continued past the crop. Each card loops a short story while on
 * screen and holds its most telling frame under reduced motion. Data is
 * fictional.
 */

const LARGE = { width: 520, height: 390 } as const;
const SMALL = { width: 360, height: 240 } as const;

const ENTER = "animate-in fade-in slide-in-from-bottom-1 duration-300 motion-reduce:animate-none";

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

function Working({ label = "Working" }: { label?: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="size-1.5 shrink-0 rounded-full bg-site-agent" />
      <ShimmerText className="text-micro text-muted-foreground">{label}</ShimmerText>
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
// `multica setup` runs line by line, then the new machine joins the list.

type Runtime = {
  name: string;
  detail: string;
  icon: LucideIcon;
  providers: string[];
  note: ReactNode;
};

const RUNTIMES: Runtime[] = [
  {
    name: "Mia’s MacBook Pro",
    detail: "macOS · local",
    icon: Laptop,
    providers: ["Claude Code", "Codex", "Cursor"],
    note: <Working />,
  },
  {
    name: "build-01",
    detail: "Linux · team server",
    icon: Server,
    providers: ["Claude Code", "Qwen Code", "OpenCode"],
    note: <Working />,
  },
  {
    name: "Multica Cloud",
    detail: "Sandboxes · managed",
    icon: Cloud,
    providers: ["Claude Code", "Codex"],
    note: <span className="text-micro text-muted-foreground">2 sandboxes</span>,
  },
];

const NEW_RUNTIME: Runtime = {
  name: "Sam’s Mac mini",
  detail: "macOS · local",
  icon: Laptop,
  providers: ["Claude Code", "Codex", "Cursor"],
  note: <span className="text-micro text-success">Connected</span>,
};

const SETUP_LINES: ReactNode[] = [
  <span key="command">
    <span className="text-muted-foreground">$</span> multica setup
  </span>,
  <span key="found" className="text-muted-foreground">
    Found 3 coding agents on this machine
  </span>,
  <span key="agents">
    <span className="text-success">✓</span> Claude Code · Codex · Cursor
  </span>,
  <span key="connected">
    <span className="text-success">✓</span> Connected to Acme Labs
  </span>,
];

const RUNTIME_HOLDS = [700, 700, 700, 1000, 2600, 600] as const;

function RuntimeRow({ runtime, className }: { runtime: Runtime; className?: string }) {
  return (
    <li className={cn("flex items-center gap-3 border-b border-border px-4 py-3.5", className)}>
      <span className="flex size-8 items-center justify-center rounded-lg bg-muted">
        <runtime.icon className="size-4 text-muted-foreground" />
      </span>
      <div className="min-w-0 flex-1">
        <p className="flex items-center gap-2 text-label font-medium">
          {runtime.name}
          <span className="size-1.5 rounded-full bg-success" />
        </p>
        <p className="text-caption text-muted-foreground">{runtime.detail}</p>
      </div>
      <span className="flex w-[84px] items-center gap-1">
        {runtime.providers.map((provider) => {
          const Mark = PROVIDER_MARKS.get(provider);
          return (
            <span key={provider} className="flex size-6 items-center justify-center rounded-md border border-border bg-background">
              {Mark ? <Mark className="size-3.5" /> : null}
            </span>
          );
        })}
      </span>
      <span className="w-[72px] text-right">{runtime.note}</span>
    </li>
  );
}

export function RuntimesVisual() {
  const { ref, step } = useStoryLoop(RUNTIME_HOLDS, 3);
  const connected = step >= 4;
  return (
    <ZoomCanvas {...LARGE}>
      <div ref={ref} className="absolute inset-0">
        <Window className="inset-x-7 top-7 h-[420px]">
          <WindowHeader icon={Monitor} title="Runtimes">
            <OutlineButton icon={Plus}>Connect machine</OutlineButton>
          </WindowHeader>
          <ul className="flex flex-col">
            {RUNTIMES.map((runtime) => (
              <RuntimeRow key={runtime.name} runtime={runtime} />
            ))}
            {connected ? (
              <RuntimeRow
                runtime={NEW_RUNTIME}
                className={cn(ENTER, "transition-opacity duration-300", step === 5 && "opacity-0")}
              />
            ) : null}
          </ul>
        </Window>
        <div
          className={cn(
            "absolute bottom-6 left-16 w-[280px] rounded-xl border border-site-line-strong bg-background p-4 font-mono text-micro shadow-site-float",
            "transition-[opacity,translate] duration-300 motion-reduce:transition-none",
            connected && "translate-y-2 opacity-0",
          )}
        >
          {SETUP_LINES.map((line, index) => (
            <p
              key={index}
              className={cn(
                "flex items-center gap-2 transition-opacity duration-200 not-first:mt-1.5",
                index > Math.min(step, 3) && !connected && "opacity-0",
              )}
            >
              {line}
            </p>
          ))}
        </div>
      </div>
    </ZoomCanvas>
  );
}

// --- Workflow ---------------------------------------------------------------
// An issue walks the project's steps; each step hands it to its owner.

type WorkflowStep = {
  status: "todo" | "in_progress" | "in_review" | "done";
  label: string;
  handler?: Actor;
  brief: string;
  state: ReactNode;
};

const WORKFLOW_STEPS: WorkflowStep[] = [
  {
    status: "todo",
    label: "Todo",
    handler: MIA,
    brief: "Scope it and write acceptance criteria",
    state: <span className="text-micro text-muted-foreground">Scoping</span>,
  },
  {
    status: "in_progress",
    label: "In Progress",
    handler: BACKEND,
    brief: "Build it and open a PR",
    state: <Working />,
  },
  {
    status: "in_review",
    label: "In Review",
    handler: QA,
    brief: "Run the test plan against the PR",
    state: <Working label="Testing" />,
  },
  {
    status: "done",
    label: "Done",
    brief: "Merged and released",
    state: <span className="text-micro text-success">Merged</span>,
  },
];

const WORKFLOW_HOLDS = [1500, 1900, 1900, 2100] as const;

export function WorkflowVisual() {
  const { ref, step } = useStoryLoop(WORKFLOW_HOLDS, 2);
  return (
    <ZoomCanvas {...LARGE}>
      <div ref={ref} className="absolute inset-0">
        <Window className="inset-x-7 top-7 h-[420px]">
          <WindowHeader icon={Workflow} title="Feature delivery">
            <span className="text-caption text-muted-foreground">Used by 3 projects</span>
          </WindowHeader>
          <ol className="flex flex-col gap-1 p-2">
            {WORKFLOW_STEPS.map((workflowStep, index) => {
              const active = index === step;
              return (
                <li
                  key={workflowStep.status}
                  className={cn(
                    "relative rounded-lg px-3 py-2.5 transition-colors duration-300",
                    active && "bg-muted/60",
                  )}
                >
                  <div className="flex items-center gap-3">
                    <StatusIcon status={workflowStep.status} className="h-3.5 w-3.5 shrink-0" />
                    <div className="min-w-0 flex-1">
                      <p className="text-label font-medium">{workflowStep.label}</p>
                      <p className="truncate text-caption text-muted-foreground">{workflowStep.brief}</p>
                    </div>
                    {workflowStep.handler ? (
                      <span className="flex items-center gap-1.5 text-caption">
                        <Avatar actor={workflowStep.handler} size={20} />
                        {workflowStep.handler.name}
                      </span>
                    ) : null}
                  </div>
                  {active ? (
                    <div
                      key={step}
                      className={cn(
                        ENTER,
                        "mt-2 ml-6.5 flex items-center gap-2 rounded-md border border-surface-border bg-surface px-2.5 py-1.5 text-caption shadow-[var(--surface-shadow)]",
                      )}
                    >
                      <span className="text-muted-foreground tabular-nums">ACM-142</span>
                      <span className="min-w-0 flex-1 truncate font-medium">Add rate limiting to the public API</span>
                      {workflowStep.state}
                    </div>
                  ) : null}
                </li>
              );
            })}
          </ol>
        </Window>
      </div>
    </ZoomCanvas>
  );
}

// --- Chat -------------------------------------------------------------------
// A teammate mentions the agent; it types, replies, and links the issue.

const CHAT_HOLDS = [1000, 1300, 900, 2600] as const;

export function ChatVisual() {
  const { ref, step } = useStoryLoop(CHAT_HOLDS, 3);
  return (
    <ZoomCanvas {...SMALL}>
      <div ref={ref} className="absolute inset-0">
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
                  <span className="rounded bg-brand/10 px-0.5 whitespace-nowrap text-brand">@Backend Agent</span> can you
                  look?
                </p>
              </div>
            </div>
            {step === 1 ? (
              <div className={cn(ENTER, "flex items-center gap-2.5")}>
                <Avatar actor={BACKEND} size={24} />
                <ShimmerText className="text-caption text-muted-foreground">Backend Agent is typing…</ShimmerText>
              </div>
            ) : null}
            {step >= 2 ? (
              <div className={cn(ENTER, "flex gap-2.5")}>
                <Avatar actor={BACKEND} size={24} />
                <div className="min-w-0 flex-1">
                  <p className="flex items-center gap-1.5 font-medium">
                    Backend Agent <span className="font-normal text-muted-foreground">10:42</span>
                    <span className="ml-auto">
                      <Working />
                    </span>
                  </p>
                  <p className="mt-0.5 leading-relaxed">On it. Filed an issue, reading the logs now.</p>
                  {step >= 3 ? (
                    <span className={cn(ENTER, "mt-2 flex items-center gap-2 rounded-md border border-border px-2.5 py-1.5")}>
                      <StatusIcon status="in_progress" className="h-3.5 w-3.5" />
                      <span className="text-muted-foreground tabular-nums">ACM-151</span>
                      <span className="min-w-0 flex-1 truncate font-medium">EU checkout failures</span>
                    </span>
                  ) : null}
                </div>
              </div>
            ) : null}
          </div>
        </Window>
      </div>
    </ZoomCanvas>
  );
}

// --- Autopilots -------------------------------------------------------------
// Each schedule takes its turn firing.

const AUTOPILOTS: { name: string; schedule: string; agent: Actor; on: boolean }[] = [
  { name: "Weekly dependency upgrades", schedule: "Mondays 09:00", agent: FRONTEND, on: true },
  { name: "Triage new bug reports", schedule: "Every hour", agent: QA, on: true },
  { name: "Nightly flaky-test sweep", schedule: "Daily 02:00", agent: QA, on: true },
  { name: "Monthly usage report", schedule: "1st of the month", agent: BACKEND, on: false },
];

const AUTOPILOT_HOLDS = [2000, 2000, 2000] as const;

export function AutopilotsVisual() {
  const { ref, step } = useStoryLoop(AUTOPILOT_HOLDS, 1);
  return (
    <ZoomCanvas {...SMALL}>
      <div ref={ref} className="absolute inset-0">
        <Window className="inset-x-5 top-5 h-[260px]">
          <WindowHeader icon={Zap} title="Autopilot" />
          <ul className="flex flex-col">
            {AUTOPILOTS.map((autopilot, index) => (
              <li key={autopilot.name} className="flex items-center gap-3 border-b border-border px-4 py-3 text-caption">
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">{autopilot.name}</p>
                  <p className="mt-0.5 flex h-4 items-center gap-1 text-muted-foreground">
                    {index === step ? (
                      <span key={step} className="animate-in fade-in duration-300 motion-reduce:animate-none">
                        <Working label="Running now" />
                      </span>
                    ) : (
                      <>
                        <Clock className="size-3" />
                        {autopilot.schedule}
                      </>
                    )}
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
      </div>
    </ZoomCanvas>
  );
}

// --- Skills -----------------------------------------------------------------
// Agents pick up the team's skills as they work.

const SKILLS: { name: string; summary: string; agents: Actor[] }[] = [
  { name: "release-checklist", summary: "Everything we run before a deploy", agents: [BACKEND, FRONTEND, QA] },
  { name: "postgres-migrations", summary: "How we write and review migrations", agents: [BACKEND, LEAD] },
  { name: "api-style-guide", summary: "Naming, errors, and pagination", agents: [FRONTEND, BACKEND] },
  { name: "bug-report-triage", summary: "Labels, priority, and who to page", agents: [QA, SAM] },
];

const SKILL_HOLDS = [2000, 2000, 2000] as const;

export function SkillsVisual() {
  const { ref, step } = useStoryLoop(SKILL_HOLDS, 0);
  return (
    <ZoomCanvas {...SMALL}>
      <div ref={ref} className="absolute inset-0">
        <Window className="inset-x-5 top-5 h-[260px]">
          <WindowHeader icon={BookOpenText} title="Skills" />
          <ul className="flex flex-col">
            {SKILLS.map((skill, index) => (
              <li
                key={skill.name}
                className={cn(
                  "flex items-center gap-3 border-b border-border px-4 py-3 text-caption transition-colors duration-300",
                  index === step && "bg-muted/60",
                )}
              >
                <div className="min-w-0 flex-1">
                  <p className="truncate font-mono font-medium">{skill.name}</p>
                  <p className="mt-0.5 h-4 truncate text-muted-foreground">
                    {index === step ? (
                      <span key={step} className="animate-in fade-in duration-300 motion-reduce:animate-none">
                        <Working label={`${skill.agents[0]!.name} is following this`} />
                      </span>
                    ) : (
                      skill.summary
                    )}
                  </p>
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
      </div>
    </ZoomCanvas>
  );
}
