"use client";

import type { ReactNode } from "react";
import {
  ChevronDown,
  ChevronRight,
  Cloud,
  CornerDownRight,
  FolderKanban,
  Laptop,
  Loader2,
  Monitor,
  Plus,
  Search,
  Server,
  UserRound,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import { Avatar, BACKEND, ENTER, MIA, PROVIDER_MARKS, QA, Window, ZoomCanvas, useStoryLoop, type Actor } from "./mock-kit";

/*
 * Product screens for the two large feature cards, on a canvas that grows
 * past 520 × 390 on wide cards (see ZoomCanvas): each window is pinned to the
 * top-left and runs off the right and bottom edges like a cropped screenshot.
 * Data is fictional.
 */

const CANVAS = { width: 520, height: 390 } as const;
const SCREEN = "top-6 left-6 -right-6 -bottom-6";

// --- Runtimes ---------------------------------------------------------------
// The Runtimes page: machines and a cloud service with their coding agents.
// Work starts on a laptop, then a teammate's Mac mini joins and picks up work.

type Machine = {
  name: string;
  icon: LucideIcon;
  detail: string;
  device?: boolean;
  harness: string[];
  agents: number;
  workload: string | null;
  state: "online" | "starting";
};

const RUNTIME_HOLDS = [1200, 1400, 1400, 1300, 1300, 2400, 500] as const;

function machinesAt(step: number): Machine[] {
  const laptop: Machine = {
    name: "Mia’s MacBook Pro",
    icon: Laptop,
    detail: "arm64 macOS · You",
    device: true,
    harness: ["Claude Code", "Codex", "Cursor"],
    agents: 3,
    workload: step >= 1 ? "1 running · 0 queued" : "All idle",
    state: "online",
  };
  const server: Machine = {
    name: "build-01",
    icon: Monitor,
    detail: "x86_64 Linux · Leo Park",
    harness: ["Claude Code", "Codex"],
    agents: 4,
    workload: "2 running · 1 queued",
    state: "online",
  };
  const list = [laptop, server];
  if (step >= 2) {
    list.push({
      name: "Sam’s Mac mini",
      icon: Monitor,
      detail: "arm64 macOS · Sam Rivera",
      harness: step >= 3 ? ["Claude Code", "Codex"] : [],
      agents: step >= 3 ? 2 : 0,
      workload: step >= 4 ? "1 running · 0 queued" : step >= 3 ? "All idle" : null,
      state: step >= 3 ? "online" : "starting",
    });
  }
  return list;
}

// The Harness column only fits once the table is wide, as in the product.
const RUNTIME_COLUMNS =
  "grid grid-cols-[minmax(0,1fr)_40px_128px_72px] @[560px]:grid-cols-[minmax(0,1fr)_56px_40px_128px_72px] items-center gap-x-3 px-3";

function Harness({ names }: { names: string[] }) {
  if (names.length === 0) return <span className="hidden text-caption text-muted-foreground @[560px]:inline">—</span>;
  return (
    <span className="hidden -space-x-1 @[560px]:flex">
      {names.map((name) => {
        const Mark = PROVIDER_MARKS.get(name);
        return (
          <span key={name} className="inline-flex size-5 items-center justify-center rounded-xs bg-background ring-1 ring-border">
            {Mark ? <Mark className="size-3" /> : null}
          </span>
        );
      })}
    </span>
  );
}

function MachineRow({ machine, entering }: { machine: Machine; entering?: boolean }) {
  const Icon = machine.icon;
  const online = machine.state === "online";
  return (
    <div className={cn(RUNTIME_COLUMNS, "min-h-12 border-b border-border py-2", entering && ENTER)}>
      <div className="flex min-w-0 items-center gap-2.5">
        <span className="relative flex size-7 shrink-0 items-center justify-center rounded-lg border border-border bg-background">
          <Icon className="size-3.5 text-muted-foreground" />
          <span
            className={cn(
              "absolute -right-0.5 -bottom-0.5 size-2 rounded-full ring-2 ring-background transition-colors duration-300",
              online ? "bg-success" : "bg-warning",
            )}
          />
        </span>
        <div className="min-w-0">
          <p className="flex items-center gap-1.5">
            <span className="truncate text-label font-medium">{machine.name}</span>
            {machine.device ? (
              <span className="hidden shrink-0 rounded-sm bg-muted px-1.5 text-micro text-muted-foreground @[560px]:inline">This device</span>
            ) : null}
          </p>
          <p className="truncate text-caption text-muted-foreground">{machine.detail}</p>
        </div>
      </div>
      <Harness names={machine.harness} />
      <span className="text-label tabular-nums">{machine.agents}</span>
      <span className={cn("truncate text-caption tabular-nums", machine.workload === "All idle" && "text-muted-foreground")}>
        {machine.workload ?? <span className="text-muted-foreground">—</span>}
      </span>
      <span className="flex items-center gap-1.5 text-caption">
        {online ? (
          <>
            <span className="size-1.5 rounded-full bg-success" /> Online
          </>
        ) : (
          <>
            <Loader2 className="size-3 shrink-0 animate-spin text-muted-foreground motion-reduce:animate-none" />
            <span className="truncate text-muted-foreground">Starting…</span>
          </>
        )}
      </span>
    </div>
  );
}

function GroupBand({ title, count }: { title: string; count: number }) {
  return (
    <div className="flex h-8 items-center gap-2 border-b border-border bg-muted/40 px-3 text-caption">
      <span className="font-semibold">{title}</span>
      <span className="font-mono tabular-nums text-muted-foreground">{count}</span>
    </div>
  );
}

export function RuntimesVisual() {
  const { ref, step } = useStoryLoop(RUNTIME_HOLDS, 5);
  const machines = machinesAt(step);
  const online = machines.filter((machine) => machine.state === "online").length + 1;
  return (
    <ZoomCanvas {...CANVAS}>
      <div ref={ref} className={cn("absolute inset-0 transition-opacity duration-300", step === 6 && "opacity-0")}>
        <Window className={SCREEN}>
          <div className="flex h-11 shrink-0 items-center gap-2 border-b border-border pr-10 pl-4">
            <Server className="size-4 text-muted-foreground" />
            <span className="text-label font-medium">Runtimes</span>
            <span className="font-mono text-caption tabular-nums text-muted-foreground">{machines.length + 1}</span>
            <span className="ml-2 min-w-0 flex-1 truncate text-caption text-muted-foreground">
              Machines and cloud services your agents run tasks on.
            </span>
            <span className="flex h-7 shrink-0 items-center gap-1 rounded-md bg-primary px-2.5 text-caption font-medium text-primary-foreground">
              <Plus className="size-3.5" /> Add <ChevronDown className="size-3.5 opacity-70" />
            </span>
          </div>
          <div className="flex min-h-0 flex-1 flex-col gap-3 py-4 pr-10 pl-4">
            <div className="flex items-center gap-2">
              <span className="flex h-7 w-44 items-center gap-2 rounded-md border border-border px-2.5 text-caption text-muted-foreground">
                <Search className="size-3.5" /> Search…
              </span>
              {[
                ["All", machines.length + 1],
                ["Online", online],
                ["Offline", 0],
              ].map(([label, count], index) => (
                <span
                  key={label}
                  className={cn(
                    "flex h-7 items-center gap-1.5 rounded-md border border-border px-2.5 text-caption",
                    index === 0 ? "bg-accent text-foreground" : "text-muted-foreground",
                  )}
                >
                  {label} <span className="tabular-nums text-muted-foreground">{count}</span>
                </span>
              ))}
            </div>
            <div className="@container overflow-hidden rounded-lg border border-border bg-card">
              <div className={cn(RUNTIME_COLUMNS, "h-8 border-b border-border text-caption text-muted-foreground")}>
                <span>Name</span>
                <span className="hidden @[560px]:inline">Harness</span>
                <span>Agents</span>
                <span>Workload</span>
                <span>Status</span>
              </div>
              <GroupBand title="Machines" count={machines.length} />
              {machines.map((machine, index) => (
                <MachineRow key={machine.name} machine={machine} entering={index === 2 && step === 2} />
              ))}
              <GroupBand title="Cloud services" count={1} />
              <MachineRow
                machine={{
                  name: "Multica Cloud",
                  icon: Cloud,
                  detail: "Managed sandboxes",
                  harness: ["Claude Code", "Codex"],
                  agents: 2,
                  workload: "1 running · 0 queued",
                  state: "online",
                }}
              />
            </div>
          </div>
        </Window>
      </div>
    </ZoomCanvas>
  );
}

// --- Workflows --------------------------------------------------------------
// The workflow editor: the board's columns in order, each with who it hands
// the issue to on entering. The selection walks the steps and the detail
// panel shows each handoff.

type Handoff = { kind: "none" } | { kind: "agent"; actor: Actor; brief: string; next: string; back: string } | { kind: "member"; actor: Actor };

const STEPS: { status: string; label: string; start?: boolean; handoff: Handoff }[] = [
  { status: "todo", label: "Todo", start: true, handoff: { kind: "none" } },
  {
    status: "in_progress",
    label: "In Progress",
    handoff: {
      kind: "agent",
      actor: BACKEND,
      brief: "Build the change with tests and open a pull request.",
      next: "Next column (In Review)",
      back: "Todo",
    },
  },
  {
    status: "in_review",
    label: "In Review",
    handoff: {
      kind: "agent",
      actor: QA,
      brief: "Run the test plan against the pull request. Send it back with notes if anything fails.",
      next: "Next column (Done)",
      back: "In Progress",
    },
  },
  { status: "done", label: "Done", handoff: { kind: "member", actor: MIA } },
];

const WORKFLOW_HOLDS = [1800, 2600, 2600, 2000] as const;

function StepCard({ step, selected }: { step: (typeof STEPS)[number]; selected: boolean }) {
  const { handoff } = step;
  return (
    <div
      className={cn(
        "rounded-lg border px-2.5 py-2 transition-colors duration-300",
        selected ? "border-foreground/20 bg-accent" : "border-border bg-card",
      )}
    >
      <div className="flex h-5 items-center gap-2">
        <StatusIcon status={step.status} className="h-3.5 w-3.5" />
        <span className="truncate text-label font-medium">{step.label}</span>
        {step.start ? <span className="rounded-xs bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">Start</span> : null}
      </div>
      <div className="mt-1 flex min-w-0 items-center gap-1 text-caption text-muted-foreground">
        {handoff.kind === "none" ? (
          <>
            <UserRound className="size-3 shrink-0" /> Keep assignee
          </>
        ) : (
          <>
            <CornerDownRight className="size-3 shrink-0" /> Hand to <Avatar actor={handoff.actor} size={16} />
            <span className="truncate text-foreground">{handoff.actor.name}</span>
          </>
        )}
      </div>
    </div>
  );
}

function Choice({ title, hint, selected, children }: { title: string; hint: string; selected: boolean; children?: ReactNode }) {
  return (
    <div className={cn("rounded-lg border px-3 py-2", selected ? "border-brand/50 bg-brand/5" : "border-border")}>
      <div className="flex items-center gap-2">
        <span className={cn("flex size-3.5 items-center justify-center rounded-full border", selected ? "border-brand" : "border-border")}>
          {selected ? <span className="size-1.5 rounded-full bg-brand" /> : null}
        </span>
        <span className="shrink-0 text-label font-medium">{title}</span>
        {selected ? <span className="truncate text-caption text-muted-foreground">{hint}</span> : null}
      </div>
      {children}
    </div>
  );
}

function Select({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-7 items-center gap-2 rounded-md border border-border bg-background px-2.5 text-caption">
      <span className="flex min-w-0 flex-1 items-center gap-1.5 truncate">{children}</span>
      <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
    </div>
  );
}

function StepDetail({ step }: { step: (typeof STEPS)[number] }) {
  const { handoff } = step;
  return (
    <div className="min-w-0 flex-1 rounded-xl border border-border bg-card">
      <div className="flex items-center gap-2 border-b border-border px-4 py-2.5">
        <StatusIcon status={step.status} className="h-4 w-4" />
        <span className="text-label font-semibold">{step.label}</span>
        <span className="font-mono text-caption text-muted-foreground">{step.status}</span>
      </div>
      <div key={step.status} className={cn(ENTER, "flex flex-col gap-3 p-4")}>
        <div className="flex flex-col gap-1.5">
          <p className="text-caption font-medium">On entering, hand to</p>
          <Choice title="No handoff" hint="The assignee stays the same" selected={handoff.kind === "none"} />
          <Choice title="Hand to an agent" hint="Starts a run" selected={handoff.kind === "agent"}>
            {handoff.kind === "agent" ? (
              <div className="mt-2">
                <Select>
                  <Avatar actor={handoff.actor} size={16} /> {handoff.actor.name}
                </Select>
              </div>
            ) : null}
          </Choice>
          <Choice title="Hand to a member" hint="Changes the assignee" selected={handoff.kind === "member"}>
            {handoff.kind === "member" ? (
              <div className="mt-2">
                <Select>
                  <Avatar actor={handoff.actor} size={16} /> {handoff.actor.name}
                </Select>
              </div>
            ) : null}
          </Choice>
        </div>
        {handoff.kind === "agent" ? (
          <>
            <div className="flex flex-col gap-1.5">
              <p className="text-caption font-medium">What this step does</p>
              <p className="min-h-14 rounded-md border border-border bg-background px-2.5 py-2 text-caption">{handoff.brief}</p>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="flex min-w-0 flex-col gap-1.5">
                <p className="text-caption font-medium">When done, move to</p>
                <Select>{handoff.next}</Select>
              </div>
              <div className="flex min-w-0 flex-col gap-1.5">
                <p className="text-caption font-medium">If rejected, send back to</p>
                <Select>{handoff.back}</Select>
              </div>
            </div>
          </>
        ) : null}
      </div>
    </div>
  );
}

export function WorkflowVisual() {
  const { ref, step } = useStoryLoop(WORKFLOW_HOLDS, 1);
  const selected = STEPS[step] ?? STEPS[0]!;
  return (
    <ZoomCanvas {...CANVAS}>
      <div ref={ref} className="absolute inset-0">
        <Window className={SCREEN}>
          <div className="flex h-11 shrink-0 items-center gap-1.5 border-b border-border px-4 text-caption text-muted-foreground">
            Acme Labs <ChevronRight className="size-3" /> Workflows <ChevronRight className="size-3" />
            <span className="text-foreground">Delivery</span>
          </div>
          <div className="flex min-h-0 flex-1 flex-col gap-4 py-4 pr-10 pl-4">
            <div>
              <p className="text-title font-semibold tracking-tight">Delivery</p>
              <p className="mt-1 flex items-center gap-1.5 text-caption text-muted-foreground">
                <FolderKanban className="size-3.5" /> Used by 2 projects: API, Web app
              </p>
            </div>
            <div className="flex min-h-0 items-start gap-4">
              <ol className="flex w-[196px] shrink-0 flex-col gap-1.5">
                {STEPS.map((item) => (
                  <li key={item.status}>
                    <StepCard step={item} selected={item === selected} />
                  </li>
                ))}
              </ol>
              <StepDetail step={selected} />
            </div>
          </div>
        </Window>
      </div>
    </ZoomCanvas>
  );
}
