"use client";

import type { ReactNode } from "react";
import { BookOpen, Check, Cloud, Hash, Laptop, Monitor, Server, Workflow, Zap, type LucideIcon } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import type { Status } from "./hero-board";
import { Avatar, BACKEND, CONTENT, FRONTEND, LEAD, MIA, PROVIDER_MARKS, QA, TRIAGE, Working, type Actor } from "./mock-kit";

/*
 * Product fragments for the feature cards: one or two floating surfaces that
 * carry the feature's point at a glance, centred on the card's frosted field.
 * They are fluid, at product type sizes, and hold still. Data is fictional.
 */

const SURFACE = "rounded-xl bg-background text-foreground shadow-[0_16px_40px_-16px_rgb(0_0_0/0.45)]";

function Stage({ children }: { children: ReactNode }) {
  return <div className="flex flex-1 flex-col items-center justify-center px-5 pt-8 pb-6 sm:px-8 sm:pt-10">{children}</div>;
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

function Toast({ icon: Icon, title, detail, className }: { icon: LucideIcon; title: string; detail: ReactNode; className?: string }) {
  return (
    <div className={cn(SURFACE, "flex items-center gap-2.5 py-2.5 pr-4 pl-3", className)}>
      <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-success/10 text-success">
        <Icon className="size-3.5" />
      </span>
      <span className="min-w-0">
        <span className="block truncate text-label font-medium">{title}</span>
        <span className="block truncate text-caption text-muted-foreground">{detail}</span>
      </span>
    </div>
  );
}

// --- Runtimes ---------------------------------------------------------------

const RUNTIMES: { name: string; icon: LucideIcon; detail: string; agents: string[]; load: string }[] = [
  { name: "Mia’s MacBook Pro", icon: Laptop, detail: "macOS · This device", agents: ["Claude Code", "Codex", "Cursor"], load: "1 running" },
  { name: "build-01", icon: Monitor, detail: "Linux server", agents: ["Claude Code", "Codex"], load: "2 running" },
  { name: "Multica Cloud", icon: Cloud, detail: "Managed sandboxes", agents: ["Claude Code", "Codex"], load: "1 running" },
];

export function RuntimesVisual() {
  return (
    <Stage>
      <div className="w-full max-w-[27rem]">
        <div className={cn(SURFACE, "@container overflow-hidden")}>
          <div className="flex h-11 items-center gap-2 border-b border-border px-4">
            <Server className="size-4 text-muted-foreground" />
            <span className="text-label font-medium">Runtimes</span>
            <span className="ml-auto flex items-center gap-1.5 text-caption text-muted-foreground">
              <span className="size-1.5 rounded-full bg-success" /> 3 online
            </span>
          </div>
          <ul className="divide-y divide-border">
            {RUNTIMES.map((runtime) => (
              <li key={runtime.name} className="flex items-center gap-3 px-4 py-2.5">
                <span className="relative flex size-8 shrink-0 items-center justify-center rounded-lg border border-border">
                  <runtime.icon className="size-4 text-muted-foreground" />
                  <span className="absolute -right-0.5 -bottom-0.5 size-2 rounded-full bg-success ring-2 ring-background" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-label font-medium">{runtime.name}</span>
                  <span className="block truncate text-caption text-muted-foreground">{runtime.detail}</span>
                </span>
                <Marks names={runtime.agents} />
                <span className="hidden w-16 text-right text-caption text-muted-foreground tabular-nums @[24rem]:block">{runtime.load}</span>
              </li>
            ))}
          </ul>
        </div>
        <Toast
          icon={Check}
          title="Sam’s Mac mini connected"
          detail="Found Claude Code and Codex"
          className="relative -mt-3 ml-auto w-fit max-w-[85%] sm:-mr-4"
        />
      </div>
    </Stage>
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

const CURRENT_STEP = "build";

function Handler({ actor }: { actor: Actor }) {
  return (
    <span className="flex min-w-0 items-center gap-1.5 rounded-full border border-border py-0.5 pr-2 pl-0.5 text-caption">
      <Avatar actor={actor} size={16} />
      <span className="truncate">{actor.name}</span>
    </span>
  );
}

export function WorkflowVisual() {
  return (
    <Stage>
      <div className="w-full max-w-[25rem]">
        <div className={cn(SURFACE, "overflow-hidden")}>
          <div className="flex h-11 items-center gap-2 border-b border-border px-4">
            <Workflow className="size-4 text-muted-foreground" />
            <span className="text-label font-medium">Ship a feature</span>
            <span className="ml-auto text-caption text-muted-foreground">Workflow</span>
          </div>
          <ol className="flex flex-col gap-1 p-2">
            {STEPS.map((step) => (
              <li
                key={step.key}
                className={cn("flex h-9 items-center gap-2.5 rounded-lg px-2.5", step.key === CURRENT_STEP && "bg-accent")}
              >
                <StatusIcon status={step.key} category={step.category} color={step.color} icon={step.icon} className="h-3.5 w-3.5 shrink-0" />
                <span className="min-w-0 flex-1 truncate text-label font-medium">{step.name}</span>
                {step.handler ? <Handler actor={step.handler} /> : <span className="text-caption text-muted-foreground">Done</span>}
              </li>
            ))}
          </ol>
        </div>
        <div className={cn(SURFACE, "relative -mt-3 ml-auto w-[88%] px-3.5 py-3 sm:-mr-6")}>
          <p className="flex items-center gap-2 text-caption text-muted-foreground">
            ACM-142 <span className="ml-auto"><Working /></span>
          </p>
          <p className="mt-0.5 truncate text-label font-medium">Add rate limiting to the public API</p>
          <p className="mt-1.5 flex items-center gap-1.5 text-caption text-muted-foreground">
            Handed to <Avatar actor={BACKEND} size={16} />
            <span className="truncate text-foreground">Backend Agent</span>
          </p>
        </div>
      </div>
    </Stage>
  );
}

// --- Chat -------------------------------------------------------------------

function Said({ children }: { children: ReactNode }) {
  return (
    <p className="max-w-[85%] self-end rounded-2xl border border-white/50 bg-white/10 px-3.5 py-2 text-body text-white backdrop-blur-sm">
      {children}
    </p>
  );
}

export function ChatVisual() {
  return (
    <Stage>
      <div className="flex w-full max-w-[20rem] flex-col gap-2.5">
        <p className="flex items-center gap-1 self-end text-caption text-white/75">
          <Hash className="size-3" /> eng-support · Mia Chen
        </p>
        <Said>
          <span className="font-medium">@Backend Agent</span> CSV exports over 10k rows time out
        </Said>
        <div className={cn(SURFACE, "flex max-w-[85%] items-start gap-2 self-start px-3 py-2")}>
          <Avatar actor={BACKEND} size={20} />
          <p className="text-body">On it. I filed the issue and started working.</p>
        </div>
        <div className={cn(SURFACE, "flex max-w-full items-center gap-2 self-start rounded-full py-1.5 pr-3 pl-2.5")}>
          <StatusIcon status="in_progress" className="h-3.5 w-3.5 shrink-0" />
          <span className="shrink-0 text-caption text-muted-foreground">ACM-151</span>
          <span className="min-w-0 truncate text-label font-medium">CSV export times out</span>
        </div>
      </div>
    </Stage>
  );
}

// --- Autopilots -------------------------------------------------------------

const AUTOPILOTS: { name: string; when: string; agent: Actor }[] = [
  { name: "Nightly triage", when: "Every day · 02:00", agent: TRIAGE },
  { name: "Dependency bumps", when: "Mondays · 09:00", agent: BACKEND },
  { name: "Weekly digest", when: "Fridays · 17:00", agent: CONTENT },
];

export function AutopilotsVisual() {
  return (
    <Stage>
      <div className="w-full max-w-[20rem]">
        <div className={cn(SURFACE, "overflow-hidden")}>
          <div className="flex h-10 items-center gap-2 border-b border-border px-3.5">
            <Zap className="size-3.5 text-muted-foreground" />
            <span className="text-label font-medium">Autopilots</span>
          </div>
          <ul className="divide-y divide-border">
            {AUTOPILOTS.map((autopilot) => (
              <li key={autopilot.name} className="flex items-center gap-2.5 px-3.5 py-2">
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-label font-medium">{autopilot.name}</span>
                  <span className="block truncate text-caption text-muted-foreground tabular-nums">{autopilot.when}</span>
                </span>
                <Avatar actor={autopilot.agent} size={20} />
              </li>
            ))}
          </ul>
        </div>
        <Toast
          icon={Check}
          title="Filed ACM-163"
          detail="Bump dependencies · Backend Agent"
          className="relative -mt-2.5 ml-auto w-fit max-w-[90%] sm:-mr-3"
        />
      </div>
    </Stage>
  );
}

// --- Skills -----------------------------------------------------------------

const SKILL_RULES = ["Group changes by product area", "Link every pull request", "Keep it under 200 words"];
const SKILL_USERS: Actor[] = [LEAD, BACKEND, FRONTEND, CONTENT];

export function SkillsVisual() {
  return (
    <Stage>
      <div className={cn(SURFACE, "w-full max-w-[20rem] overflow-hidden")}>
        <div className="flex h-10 items-center gap-2 border-b border-border px-3.5">
          <BookOpen className="size-3.5 text-muted-foreground" />
          <span className="text-label font-medium">Write release notes</span>
          <span className="ml-auto rounded-sm bg-muted px-1.5 text-micro text-muted-foreground">Skill</span>
        </div>
        <ul className="flex flex-col gap-1.5 px-3.5 py-3">
          {SKILL_RULES.map((rule) => (
            <li key={rule} className="flex items-center gap-2 text-label">
              <Check className="size-3.5 shrink-0 text-success" />
              <span className="truncate">{rule}</span>
            </li>
          ))}
        </ul>
        <div className="flex items-center gap-2 border-t border-border px-3.5 py-2.5">
          <span className="flex -space-x-1.5">
            {SKILL_USERS.map((agent) => (
              <span key={agent.name} className="rounded-full ring-2 ring-background">
                <Avatar actor={agent} size={20} />
              </span>
            ))}
          </span>
          <span className="text-caption text-muted-foreground">Used by 4 agents</span>
        </div>
      </div>
    </Stage>
  );
}
