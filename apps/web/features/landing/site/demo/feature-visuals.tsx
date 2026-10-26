"use client";

import type { ReactNode } from "react";
import {
  BookOpen,
  ChevronDown,
  ChevronRight,
  Cloud,
  CornerDownRight,
  FolderKanban,
  Hash,
  Laptop,
  Monitor,
  Search,
  Server,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import type { Status } from "./hero-board";
import {
  Avatar,
  BACKEND,
  CONTENT,
  FRONTEND,
  LEAD,
  LEO,
  MIA,
  PROVIDER_MARKS,
  QA,
  TRIAGE,
  Window,
  Working,
  ZoomCanvas,
  type Actor,
} from "./mock-kit";

/*
 * Product screens for the feature cards. Each window starts just inside the
 * card's top-left corner and runs off the right and bottom edges like a
 * cropped screenshot, so the product fills the card and the frosted field
 * only frames it. The canvas zooms with the card (see ZoomCanvas): keep what
 * matters in the first 400 × 290 of each window. Data is fictional.
 */

const CANVAS = { width: 440, height: 330 } as const;
const SCREEN = "top-8 left-8 -right-12 -bottom-12";

function Screen({ children }: { children: ReactNode }) {
  return (
    <ZoomCanvas {...CANVAS}>
      <Window className={SCREEN}>{children}</Window>
    </ZoomCanvas>
  );
}

function Header({ icon: Icon, children }: { icon: LucideIcon; children: ReactNode }) {
  return (
    <div className="flex h-11 shrink-0 items-center gap-2 border-b border-border px-4">
      <Icon className="size-4 shrink-0 text-muted-foreground" />
      {children}
    </div>
  );
}

function Marks({ names }: { names: string[] }) {
  return (
    <span className="flex -space-x-1">
      {names.map((name) => {
        const Mark = PROVIDER_MARKS.get(name);
        return (
          <span key={name} className="inline-flex size-5 items-center justify-center rounded-full bg-background ring-1 ring-border">
            {Mark ? <Mark className="size-3" /> : null}
          </span>
        );
      })}
    </span>
  );
}

function Chip({ actor }: { actor: Actor }) {
  return (
    <span className="flex min-w-0 items-center gap-1.5 rounded-full border border-border py-0.5 pr-2 pl-0.5 text-caption">
      <Avatar actor={actor} size={16} />
      <span className="truncate">{actor.name}</span>
    </span>
  );
}

// --- Runtimes ---------------------------------------------------------------

type Runtime = { name: string; icon: LucideIcon; detail: string; agents: string[]; load: string };

const MACHINES: Runtime[] = [
  { name: "Mia’s MacBook Pro", icon: Laptop, detail: "macOS · This device", agents: ["Claude Code", "Codex", "Cursor"], load: "1 running" },
  { name: "build-01", icon: Monitor, detail: "Linux · Leo Park", agents: ["Claude Code", "Codex"], load: "2 running · 1 queued" },
  { name: "Sam’s Mac mini", icon: Monitor, detail: "macOS · Sam Rivera", agents: ["Claude Code", "Codex"], load: "All idle" },
];

const CLOUD: Runtime[] = [
  { name: "Multica Cloud", icon: Cloud, detail: "Managed sandboxes", agents: ["Claude Code", "Codex"], load: "1 running" },
];

const RUNTIME_COLUMNS = "grid grid-cols-[minmax(176px,1fr)_72px_136px_80px] items-center gap-x-3 px-4";

function RuntimeRow({ runtime }: { runtime: Runtime }) {
  return (
    <div className={cn(RUNTIME_COLUMNS, "h-12 border-b border-border")}>
      <div className="flex min-w-0 items-center gap-2.5">
        <span className="relative flex size-7 shrink-0 items-center justify-center rounded-lg border border-border">
          <runtime.icon className="size-3.5 text-muted-foreground" />
          <span className="absolute -right-0.5 -bottom-0.5 size-2 rounded-full bg-success ring-2 ring-background" />
        </span>
        <span className="min-w-0">
          <span className="block truncate text-label font-medium">{runtime.name}</span>
          <span className="block truncate text-caption text-muted-foreground">{runtime.detail}</span>
        </span>
      </div>
      <Marks names={runtime.agents} />
      <span className={cn("truncate text-caption tabular-nums", runtime.load === "All idle" && "text-muted-foreground")}>{runtime.load}</span>
      <span className="flex items-center gap-1.5 text-caption">
        <span className="size-1.5 rounded-full bg-success" /> Online
      </span>
    </div>
  );
}

function GroupBand({ title, count }: { title: string; count: number }) {
  return (
    <div className="flex h-8 items-center gap-2 border-b border-border bg-muted/40 px-4 text-caption">
      <span className="font-semibold">{title}</span>
      <span className="tabular-nums text-muted-foreground">{count}</span>
    </div>
  );
}

export function RuntimesVisual() {
  return (
    <Screen>
      <Header icon={Server}>
        <span className="text-label font-medium">Runtimes</span>
        <span className="text-caption tabular-nums text-muted-foreground">4</span>
        <span className="ml-3 truncate text-caption text-muted-foreground">Machines and cloud services your agents run on</span>
      </Header>
      <div className="flex items-center gap-2 px-4 py-3">
        <span className="flex h-7 w-40 items-center gap-2 rounded-md border border-border px-2.5 text-caption text-muted-foreground">
          <Search className="size-3.5" /> Search…
        </span>
        <span className="flex h-7 items-center gap-1.5 rounded-md border border-border bg-accent px-2.5 text-caption">
          All <span className="text-muted-foreground">4</span>
        </span>
        <span className="flex h-7 items-center gap-1.5 rounded-md border border-border px-2.5 text-caption text-muted-foreground">
          Online <span>4</span>
        </span>
      </div>
      <div className="border-t border-border">
        <div className={cn(RUNTIME_COLUMNS, "h-8 border-b border-border text-caption text-muted-foreground")}>
          <span>Name</span>
          <span>Agents</span>
          <span>Workload</span>
          <span>Status</span>
        </div>
        <GroupBand title="Machines" count={MACHINES.length} />
        {MACHINES.map((runtime) => (
          <RuntimeRow key={runtime.name} runtime={runtime} />
        ))}
        <GroupBand title="Cloud services" count={CLOUD.length} />
        {CLOUD.map((runtime) => (
          <RuntimeRow key={runtime.name} runtime={runtime} />
        ))}
      </div>
    </Screen>
  );
}

// --- Workflows --------------------------------------------------------------

const STEPS: Status[] = [
  { key: "spec", name: "Spec", category: "unstarted", icon: "circle", color: "#8b5cf6", handler: LEAD },
  { key: "build", name: "Build", category: "started", icon: "half", color: "#f97316", handler: BACKEND },
  { key: "review", name: "Code review", category: "started", icon: "three_quarters", color: "#3b82f6", handler: QA },
  { key: "approve", name: "Approve", category: "started", icon: "three_quarters", color: "#ec4899", handler: MIA },
  { key: "shipped", name: "Shipped", category: "done", icon: "check", color: "#22c55e" },
];

const SELECTED_STEP = STEPS[1]!;

function StepIcon({ step, className }: { step: Status; className: string }) {
  return <StatusIcon status={step.key} category={step.category} color={step.color} icon={step.icon} className={className} />;
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <p className="text-caption font-medium">{label}</p>
      {children}
    </div>
  );
}

function Select({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-8 items-center gap-2 rounded-md border border-border px-2.5 text-caption">
      <span className="flex min-w-0 flex-1 items-center gap-1.5 truncate">{children}</span>
      <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
    </div>
  );
}

export function WorkflowVisual() {
  return (
    <Screen>
      <Header icon={FolderKanban}>
        <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
          Workflows <ChevronRight className="size-3" /> <span className="text-foreground">Ship a feature</span>
        </span>
      </Header>
      <div className="flex min-h-0 flex-1 gap-4 p-4">
        <ol className="flex w-[196px] shrink-0 flex-col gap-1.5">
          {STEPS.map((step) => (
            <li
              key={step.key}
              className={cn(
                "rounded-lg border px-2.5 py-2",
                step === SELECTED_STEP ? "border-foreground/20 bg-accent" : "border-border",
              )}
            >
              <p className="flex items-center gap-2">
                <StepIcon step={step} className="h-3.5 w-3.5 shrink-0" />
                <span className="truncate text-label font-medium">{step.name}</span>
              </p>
              <div className="mt-1 flex min-w-0 items-center gap-1 whitespace-nowrap text-caption text-muted-foreground">
                {step.handler ? (
                  <>
                    <CornerDownRight className="size-3 shrink-0" /> Hand to <Avatar actor={step.handler} size={16} />
                    <span className="truncate text-foreground">{step.handler.name}</span>
                  </>
                ) : (
                  "Final step"
                )}
              </div>
            </li>
          ))}
        </ol>
        <div className="min-w-[260px] flex-1 rounded-xl border border-border">
          <div className="flex h-10 items-center gap-2 border-b border-border px-4">
            <StepIcon step={SELECTED_STEP} className="h-4 w-4" />
            <span className="text-label font-semibold">{SELECTED_STEP.name}</span>
            <span className="ml-auto">
              <Working label="1 issue in progress" />
            </span>
          </div>
          <div className="flex flex-col gap-3 p-4">
            <Field label="On entering, hand to">
              <Select>
                <Avatar actor={BACKEND} size={16} /> Backend Agent
              </Select>
            </Field>
            <Field label="What this step does">
              <p className="rounded-md border border-border px-2.5 py-2 text-caption">
                Build the change with tests and open a pull request.
              </p>
            </Field>
            <Field label="When done, move to">
              <Select>
                <StepIcon step={STEPS[2]!} className="h-3.5 w-3.5" /> Code review
              </Select>
            </Field>
          </div>
        </div>
      </div>
    </Screen>
  );
}

// --- Chat -------------------------------------------------------------------

function Message({ actor, time, children }: { actor: Actor; time: string; children: ReactNode }) {
  return (
    <div className="flex gap-2.5">
      <Avatar actor={actor} size={24} />
      <div className="min-w-0 flex-1">
        <p className="flex items-baseline gap-2">
          <span className="text-label font-semibold">{actor.name}</span>
          <span className="text-caption text-muted-foreground tabular-nums">{time}</span>
        </p>
        <div className="mt-0.5 text-body">{children}</div>
      </div>
    </div>
  );
}

export function ChatVisual() {
  return (
    <Screen>
      <Header icon={Hash}>
        <span className="text-label font-semibold">eng-support</span>
        <ChevronDown className="size-3.5 text-muted-foreground" />
      </Header>
      <div className="flex flex-col gap-4 p-4">
        <Message actor={MIA} time="11:55">
          Exports over 10k rows time out for Globex. <span className="font-medium text-brand">@Backend Agent</span> can you take a
          look?
        </Message>
        <Message actor={BACKEND} time="11:56">
          Filed it and started on a fix.
          <span className="mt-2 flex w-fit max-w-full items-center gap-2 rounded-lg border border-border px-2.5 py-1.5">
            <StatusIcon status="in_progress" className="h-3.5 w-3.5 shrink-0" />
            <span className="shrink-0 text-caption text-muted-foreground">ACM-151</span>
            <span className="truncate text-label font-medium">CSV export times out</span>
            <Working />
          </span>
        </Message>
        <Message actor={LEO} time="11:58">
          Thanks. I’ll review the PR when it’s up.
        </Message>
      </div>
    </Screen>
  );
}

// --- Autopilots -------------------------------------------------------------

const AUTOPILOTS: { name: string; when: string; agent: Actor; next: string }[] = [
  { name: "Nightly triage", when: "Every day · 02:00", agent: TRIAGE, next: "in 6h" },
  { name: "Flaky test sweep", when: "Every 6 hours", agent: QA, next: "in 1h" },
  { name: "Dependency bumps", when: "Mondays · 09:00", agent: BACKEND, next: "in 2d" },
  { name: "Weekly digest", when: "Fridays · 17:00", agent: CONTENT, next: "in 4d" },
];

export function AutopilotsVisual() {
  return (
    <Screen>
      <Header icon={Zap}>
        <span className="text-label font-medium">Autopilots</span>
        <span className="text-caption tabular-nums text-muted-foreground">{AUTOPILOTS.length}</span>
      </Header>
      <ul>
        {AUTOPILOTS.map((autopilot) => (
          <li key={autopilot.name} className="flex h-14 items-center gap-3 border-b border-border px-4">
            <span className="flex h-4 w-7 shrink-0 items-center rounded-full bg-brand p-0.5">
              <span className="ml-auto size-3 rounded-full bg-background" />
            </span>
            <span className="w-36 min-w-0 shrink-0">
              <span className="block truncate text-label font-medium">{autopilot.name}</span>
              <span className="block truncate text-caption text-muted-foreground tabular-nums">{autopilot.when}</span>
            </span>
            <span className="w-32 shrink-0">
              <Chip actor={autopilot.agent} />
            </span>
            <span className="text-caption text-muted-foreground tabular-nums">Next run {autopilot.next}</span>
          </li>
        ))}
      </ul>
    </Screen>
  );
}

// --- Skills -----------------------------------------------------------------

const SKILLS = ["Write release notes", "Review a migration", "Triage a bug report", "Deploy checklist", "Write a changelog"];
const SKILL_RULES = ["Group changes by product area.", "Link every pull request.", "Lead with what users can do now.", "Keep it under 200 words."];
const SKILL_USERS: Actor[] = [LEAD, BACKEND, FRONTEND, CONTENT];

export function SkillsVisual() {
  return (
    <Screen>
      <div className="flex min-h-0 flex-1">
        <div className="w-40 shrink-0 border-r border-border bg-muted/40 p-2">
          <p className="flex h-8 items-center gap-2 px-2 text-label font-medium">
            <BookOpen className="size-4 text-muted-foreground" /> Skills
          </p>
          <ul className="mt-1 flex flex-col gap-0.5">
            {SKILLS.map((skill, index) => (
              <li
                key={skill}
                className={cn("truncate rounded-md px-2 py-1.5 text-caption", index === 0 ? "bg-accent font-medium" : "text-muted-foreground")}
              >
                {skill}
              </li>
            ))}
          </ul>
        </div>
        <div className="min-w-[240px] flex-1 px-5 py-4">
          <p className="text-title-sm font-semibold">Write release notes</p>
          <div className="mt-1.5 flex items-center gap-2 text-caption text-muted-foreground">
            <span className="flex -space-x-1">
              {SKILL_USERS.map((agent) => (
                <span key={agent.name} className="rounded-full ring-2 ring-background">
                  <Avatar actor={agent} size={16} />
                </span>
              ))}
            </span>
            Used by 4 agents
          </div>
          <p className="mt-4 text-caption font-medium text-muted-foreground">When writing release notes</p>
          <ul className="mt-2 flex flex-col gap-1.5">
            {SKILL_RULES.map((rule) => (
              <li key={rule} className="flex gap-2 text-label">
                <span className="mt-2 size-1 shrink-0 rounded-full bg-foreground" />
                {rule}
              </li>
            ))}
          </ul>
        </div>
      </div>
    </Screen>
  );
}
