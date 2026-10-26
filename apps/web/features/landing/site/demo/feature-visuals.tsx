"use client";

import type { ReactNode } from "react";
import {
  ArrowUp,
  BookOpen,
  ChevronDown,
  ChevronRight,
  Cloud,
  CornerDownRight,
  FileText,
  FolderKanban,
  Hash,
  Laptop,
  Monitor,
  Paperclip,
  Plus,
  Search,
  Server,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import { PriorityIcon } from "@multica/views/issues/components/priority-icon";
import type { Status } from "./hero-board";
import {
  AppWindow,
  Avatar,
  BACKEND,
  CONTENT,
  FRONTEND,
  LEAD,
  LEO,
  MIA,
  PROVIDER_MARKS,
  QA,
  RAISED,
  TRIAGE,
  Working,
  ZoomCanvas,
  type Actor,
} from "./mock-kit";

/*
 * Product screens for the feature cards. Each one is the Multica app (see
 * AppWindow) opened on the page the card is about, starting just inside the
 * card's top-left corner and running off the right and bottom edges like a
 * cropped screenshot. The canvas zooms with the card (see ZoomCanvas): keep
 * what matters in the first 340 × 240 of each page. Data is fictional.
 */

const CANVAS = { width: 440, height: 330 } as const;
const BLEED = 48;

function Screen({ active, children }: { active: string; children: ReactNode }) {
  return (
    <ZoomCanvas {...CANVAS}>
      <AppWindow className="top-8 left-8 -right-12 -bottom-12" active={active} bleed={BLEED}>
        {children}
      </AppWindow>
    </ZoomCanvas>
  );
}

export function Header({ icon: Icon, children }: { icon: LucideIcon; children: ReactNode }) {
  return (
    <div className="flex h-11 shrink-0 items-center gap-2 border-b border-border px-4">
      <Icon className="size-4 shrink-0 text-muted-foreground" />
      {children}
    </div>
  );
}

function Marks({ names }: { names: string[] }) {
  return (
    <span className="flex shrink-0 -space-x-1">
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

export function Chip({ actor }: { actor: Actor }) {
  return (
    <span className="flex min-w-0 items-center gap-1.5 rounded-full border border-border py-0.5 pr-2 pl-0.5 text-caption">
      <Avatar actor={actor} size={16} />
      <span className="truncate">{actor.name}</span>
    </span>
  );
}

export function GroupBand({ title, count }: { title: string; count: number }) {
  return (
    <div className="flex h-8 shrink-0 items-center gap-2 border-b border-border bg-muted/40 px-4 text-caption">
      <span className="font-semibold">{title}</span>
      <span className="tabular-nums text-muted-foreground">{count}</span>
    </div>
  );
}

// --- Runtimes ---------------------------------------------------------------

type Runtime = { name: string; icon: LucideIcon; detail: string; agents: string[]; load: string; fresh?: boolean };

const MACHINES: Runtime[] = [
  { name: "build-02", icon: Server, detail: "Linux · Leo Park", agents: ["Claude Code", "Codex", "OpenCode"], load: "Connected", fresh: true },
  { name: "Mia’s MacBook Pro", icon: Laptop, detail: "macOS · This device", agents: ["Claude Code", "Codex", "Cursor"], load: "1 running" },
  { name: "build-01", icon: Server, detail: "Linux · Leo Park", agents: ["Claude Code", "Codex"], load: "2 running" },
  { name: "Sam’s Mac mini", icon: Monitor, detail: "macOS · Sam Rivera", agents: ["Claude Code"], load: "Idle" },
];

const CLOUD: Runtime[] = [
  { name: "Multica Cloud", icon: Cloud, detail: "Managed sandboxes", agents: ["Claude Code", "Codex"], load: "1 running" },
];

function RuntimeRow({ runtime }: { runtime: Runtime }) {
  return (
    <div className={cn("flex h-12 shrink-0 items-center gap-3 border-b border-border px-4", runtime.fresh && "bg-brand/5")}>
      <span className={cn(RAISED, "relative flex size-7 shrink-0 items-center justify-center")}>
        <runtime.icon className="size-3.5 text-muted-foreground" />
        <span className="absolute -right-0.5 -bottom-0.5 size-2 rounded-full bg-success ring-2 ring-background" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-label font-medium">{runtime.name}</span>
        <span className="block truncate text-caption text-muted-foreground">{runtime.detail}</span>
      </span>
      <Marks names={runtime.agents} />
      <span
        className={cn(
          "w-[72px] shrink-0 text-right text-caption whitespace-nowrap tabular-nums",
          runtime.fresh ? "font-medium text-brand" : runtime.load === "Idle" && "text-muted-foreground",
        )}
      >
        {runtime.load}
      </span>
    </div>
  );
}

export function RuntimesVisual() {
  return (
    <Screen active="runtimes">
      <Header icon={Monitor}>
        <span className="text-label font-medium">Runtimes</span>
        <span className="text-caption tabular-nums text-muted-foreground">5</span>
      </Header>
      <div className="flex shrink-0 items-center gap-1 px-3 py-2.5">
        <span className="flex h-7 items-center rounded-md border border-border bg-muted px-2.5 text-caption font-medium">All</span>
        <span className="flex h-7 items-center rounded-md px-2.5 text-caption text-muted-foreground">Machines</span>
        <span className="flex h-7 items-center rounded-md px-2.5 text-caption text-muted-foreground">Cloud</span>
        <Search className="ml-auto size-3.5 text-muted-foreground" />
      </div>
      <div className="border-t border-border">
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
      <p className="text-caption text-muted-foreground">{label}</p>
      {children}
    </div>
  );
}

function Select({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-8 items-center gap-2 rounded-md border border-border bg-background px-2.5 text-caption">
      <span className="flex min-w-0 flex-1 items-center gap-1.5 truncate">{children}</span>
      <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
    </div>
  );
}

export function WorkflowVisual() {
  return (
    <Screen active="projects">
      <Header icon={FolderKanban}>
        <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
          Workflows <ChevronRight className="size-3" /> <span className="font-medium text-foreground">Ship a feature</span>
        </span>
      </Header>
      <div className="flex min-h-0 flex-1 gap-4 p-3">
        <ol className="flex w-[176px] shrink-0 flex-col max-sm:w-[148px] gap-1.5 self-start rounded-xl bg-muted/40 p-1.5">
          {STEPS.map((step) => (
            <li key={step.key} className={cn(RAISED, "px-2.5 py-2", step === SELECTED_STEP && "ring-2 ring-brand/50")}>
              <p className="flex items-center gap-2">
                <StepIcon step={step} className="h-3.5 w-3.5 shrink-0" />
                <span className="truncate text-label font-medium">{step.name}</span>
              </p>
              <div className="mt-1 flex min-w-0 items-center gap-1 whitespace-nowrap text-caption text-muted-foreground">
                {step.handler ? (
                  <>
                    <CornerDownRight className="size-3 shrink-0" />
                    <Avatar actor={step.handler} size={16} />
                    <span className="truncate text-foreground">{step.handler.name}</span>
                  </>
                ) : (
                  "Final step"
                )}
              </div>
            </li>
          ))}
        </ol>
        <div className="flex min-w-0 flex-1 flex-col gap-3 pt-1">
          <p className="flex items-center gap-2">
            <StepIcon step={SELECTED_STEP} className="h-4 w-4 shrink-0" />
            <span className="text-label font-semibold">{SELECTED_STEP.name}</span>
          </p>
          <Field label="On entering, hand to">
            <Select>
              <Avatar actor={BACKEND} size={16} /> Backend Agent
            </Select>
          </Field>
          <Field label="When done, move to">
            <Select>
              <StepIcon step={STEPS[2]!} className="h-3.5 w-3.5 shrink-0" /> Code review
            </Select>
          </Field>
          <Field label="In this step">
            <div className={cn(RAISED, "px-2.5 py-2.5")}>
              <div className="flex items-center justify-between gap-2">
                <span className="flex min-w-0 items-center gap-1.5">
                  <PriorityIcon priority="high" />
                  <span className="truncate text-caption text-muted-foreground">ACM-136</span>
                </span>
                <Working />
              </div>
              <p className="mt-1 line-clamp-2 text-label leading-snug font-medium">Move auth webhooks onto the job queue</p>
              <p className="mt-2 flex items-center gap-1.5 text-caption">
                <Avatar actor={BACKEND} size={16} />
                <span className="truncate">Backend Agent</span>
              </p>
            </div>
          </Field>
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
    <Screen active="chat">
      <Header icon={Hash}>
        <span className="text-label font-semibold">eng-support</span>
        <ChevronDown className="size-3.5 text-muted-foreground" />
      </Header>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-hidden p-4">
        <Message actor={MIA} time="11:55">
          Exports over 10k rows time out. <span className="font-medium text-brand">@Backend Agent</span> can you take a look?
        </Message>
        <Message actor={BACKEND} time="11:56">
          Filed it and started on a fix.
          <span className={cn(RAISED, "mt-2 flex w-fit max-w-full items-center gap-2 px-2.5 py-1.5")}>
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
      <div className="shrink-0 px-4 pb-4">
        <div className="flex items-center justify-between rounded-lg border border-border px-3 py-2.5 text-label text-muted-foreground">
          Message #eng-support
          <span className="flex items-center gap-2">
            <Paperclip className="size-3.5" />
            <span className="flex size-6 items-center justify-center rounded-md bg-muted">
              <ArrowUp className="size-3.5" />
            </span>
          </span>
        </div>
      </div>
    </Screen>
  );
}

// --- Autopilots -------------------------------------------------------------

const AUTOPILOTS: { name: string; when: string; agent: Actor }[] = [
  { name: "Nightly triage", when: "Every day · 02:00", agent: TRIAGE },
  { name: "Flaky test sweep", when: "Every 6 hours", agent: QA },
  { name: "Dependency bumps", when: "Mondays · 09:00", agent: BACKEND },
  { name: "Weekly digest", when: "Fridays · 17:00", agent: CONTENT },
  { name: "Visual regression pass", when: "Every release", agent: FRONTEND },
];

function Toggle() {
  return (
    <span className="flex h-[18px] w-8 shrink-0 items-center rounded-full bg-primary p-px">
      <span className="ml-auto size-4 rounded-full bg-background" />
    </span>
  );
}

export function AutopilotsVisual() {
  return (
    <Screen active="autopilots">
      <Header icon={Zap}>
        <span className="text-label font-medium">Autopilots</span>
        <span className="text-caption tabular-nums text-muted-foreground">{AUTOPILOTS.length}</span>
        <Plus className="ml-auto size-4 text-muted-foreground" />
      </Header>
      <ul>
        {AUTOPILOTS.map((autopilot) => (
          <li key={autopilot.name} className="flex h-14 items-center gap-3 border-b border-border px-4">
            <Toggle />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-label font-medium">{autopilot.name}</span>
              <span className="block truncate text-caption text-muted-foreground tabular-nums">{autopilot.when}</span>
            </span>
            <span className="flex max-w-[128px] shrink-0">
              <Chip actor={autopilot.agent} />
            </span>
          </li>
        ))}
      </ul>
    </Screen>
  );
}

// --- Skills -----------------------------------------------------------------

const SKILLS = ["Write release notes", "Review a migration", "Triage a bug report", "Deploy checklist", "Write a changelog", "Reply to a customer"];
const SKILL_RULES = ["Group changes by product area.", "Link every pull request.", "Lead with what users can do now.", "Keep it under 200 words."];
const SKILL_USERS: Actor[] = [LEAD, BACKEND, FRONTEND, CONTENT];

export function SkillsVisual() {
  return (
    <Screen active="skills">
      <div className="flex min-h-0 flex-1">
        <div className="w-40 shrink-0 border-r border-border bg-muted/40 p-2">
          <p className="flex h-8 items-center gap-2 px-2 text-label font-medium">
            <BookOpen className="size-4 text-muted-foreground" /> Skills
          </p>
          <ul className="mt-1 flex flex-col gap-0.5">
            {SKILLS.map((skill, index) => (
              <li
                key={skill}
                className={cn(
                  "truncate rounded-md px-2 py-1.5 text-caption",
                  index === 0 ? cn(RAISED, "font-medium") : "text-muted-foreground",
                )}
              >
                {skill}
              </li>
            ))}
          </ul>
        </div>
        <div className="flex min-w-0 flex-1 flex-col px-5 py-4">
          <p className="flex items-center gap-1.5 text-caption text-muted-foreground">
            <FileText className="size-3.5 shrink-0" /> SKILL.md
          </p>
          <p className="mt-1.5 truncate text-title-sm font-semibold">Write release notes</p>
          <div className="mt-1.5 flex items-center gap-2 text-caption whitespace-nowrap text-muted-foreground">
            <span className="flex -space-x-1">
              {SKILL_USERS.map((agent) => (
                <span key={agent.name} className="rounded-full ring-2 ring-background">
                  <Avatar actor={agent} size={20} />
                </span>
              ))}
            </span>
            4 agents
          </div>
          <ul className="mt-4 flex flex-col gap-1.5 border-t border-border pt-4">
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
