"use client";

import type { CSSProperties, ReactNode, Ref } from "react";
import { Check, Cloud, FileText, Hash, Laptop, Server, Workflow, Zap, type LucideIcon } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { ShimmerText } from "@multica/ui/components/common/shimmer-text";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import {
  Avatar,
  BACKEND,
  ENTER,
  FRONTEND,
  LEAD,
  LEO,
  MIA,
  PROVIDER_MARKS,
  Pulse,
  QA,
  Window,
  WindowHeader,
  Working,
  ZoomCanvas,
  useStoryLoop,
  type Actor,
} from "./mock-kit";

/*
 * Mockups for the home page feature cards. Large cards use a 520 × 390
 * canvas and small ones 360 × 240, matching their 4:3 and 3:2 frames and
 * sized so product text lands near its real size. Each card draws its idea
 * as a small diagram of product pieces (nodes, a map, a timeline) and loops a
 * short story while on screen, holding its most telling frame under reduced
 * motion. Positions are canvas pixels, so the connector lines, the travelling
 * dots, and the pieces they join share one coordinate space. Data is
 * fictional.
 */

const LARGE = { width: 520, height: 390 } as const;
const SMALL = { width: 360, height: 240 } as const;

/** Fades the whole scene out on a story's last step so the loop restarts cleanly. */
function Scene({ sceneRef, resetting, children }: { sceneRef: Ref<HTMLDivElement>; resetting: boolean; children: ReactNode }) {
  return (
    <div ref={sceneRef} className={cn("absolute inset-0 transition-opacity duration-300", resetting && "opacity-0")}>
      {children}
    </div>
  );
}

function Lines({ width, height, children }: { width: number; height: number; children: ReactNode }) {
  return (
    <svg
      className="absolute inset-0 overflow-visible"
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      fill="none"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {children}
    </svg>
  );
}

/** A connector; `lit` darkens it while something is moving along it. */
function Edge({ d, lit, head }: { d: string; lit?: boolean; head?: string }) {
  return (
    <g
      className={cn("transition-[stroke] duration-300", lit ? "stroke-foreground/45" : "stroke-site-line-strong")}
      strokeWidth={1.5}
    >
      <path d={d} />
      {head ? <path d={head} /> : null}
    </g>
  );
}

function Card({ className, style, children }: { className?: string; style?: CSSProperties; children: ReactNode }) {
  return (
    <div
      className={cn("absolute rounded-lg border bg-background transition-[border-color,box-shadow] duration-300", className)}
      style={style}
    >
      {children}
    </div>
  );
}

function Muted({ children, className }: { children: ReactNode; className?: string }) {
  return <span className={cn("text-micro text-muted-foreground", className)}>{children}</span>;
}

function Done({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1 text-micro text-success">
      <Check className="size-3" />
      {children}
    </span>
  );
}

// --- Workflows --------------------------------------------------------------
// An issue walks the workflow's steps as a chip moving node to node. Review
// sends it back once, then it goes through to release.

type FlowNode = {
  status: "todo" | "in_progress" | "in_review" | "done";
  label: string;
  owner: Actor;
  brief: string;
  x: number;
  y: number;
};

const NODE = { width: 184, height: 100 } as const;

const FLOW: FlowNode[] = [
  { status: "todo", label: "Todo", owner: MIA, brief: "Scope it", x: 28, y: 44 },
  { status: "in_progress", label: "In Progress", owner: BACKEND, brief: "Build it and open a PR", x: 308, y: 44 },
  { status: "in_review", label: "In Review", owner: QA, brief: "Run the test plan", x: 308, y: 246 },
  { status: "done", label: "Done", owner: MIA, brief: "Merge and release", x: 28, y: 246 },
];

const FLOW_EDGES = {
  scope: { d: "M212 94 H306", head: "M300 90 L306 94 L300 98" },
  build: { d: "M364 144 V244", head: "M360 238 L364 244 L368 238" },
  rework: { d: "M452 246 V146", head: "M448 152 L452 146 L456 152" },
  release: { d: "M308 296 H214", head: "M220 292 L214 296 L220 300" },
} as const;

// Node the chip sits on, and the edge it just crossed, per step.
const FLOW_STORY: { at: number; edge?: keyof typeof FLOW_EDGES; state?: ReactNode }[] = [
  { at: 0, state: <Muted>Scoping</Muted> },
  { at: 1, edge: "scope", state: <Working /> },
  { at: 2, edge: "build", state: <Working label="Testing" /> },
  { at: 1, edge: "rework", state: <Working label="Fixing" /> },
  { at: 2, edge: "build", state: <Done>Passed</Done> },
  { at: 3, edge: "release", state: <Done>Merged</Done> },
  { at: 0 },
];

const FLOW_HOLDS = [1300, 1500, 1500, 1700, 1300, 2200, 500] as const;

export function WorkflowVisual() {
  const { ref, step } = useStoryLoop(FLOW_HOLDS, 3);
  const scene = FLOW_STORY[step]!;
  const chip = FLOW[scene.at]!;
  return (
    <ZoomCanvas {...LARGE}>
      <Scene sceneRef={ref} resetting={step === FLOW_STORY.length - 1}>
        <p className="absolute top-3.5 left-7 flex items-center gap-1.5 text-caption text-muted-foreground">
          <Workflow className="size-3.5" />
          <span className="font-medium text-foreground">Feature delivery</span>· 4 steps
        </p>
        <Lines {...LARGE}>
          {Object.entries(FLOW_EDGES).map(([name, edge]) => (
            <Edge key={name} d={edge.d} head={edge.head} lit={scene.edge === name} />
          ))}
        </Lines>
        <span
          className={cn(
            "absolute top-[185px] left-[397px] rounded-full border bg-background px-2 py-0.5 text-micro transition-colors duration-300",
            scene.edge === "rework" ? "border-foreground/30 text-foreground" : "border-border text-muted-foreground",
          )}
        >
          Changes requested
        </span>
        {FLOW.map((node, index) => {
          const active = index === scene.at && scene.state;
          return (
            <Card
              key={node.status}
              className={cn(active ? "border-foreground/20 shadow-site-float" : "border-border")}
              style={{ left: node.x, top: node.y, width: NODE.width, height: NODE.height }}
            >
              <div className="flex h-5 items-center gap-1.5 px-3 pt-3">
                <StatusIcon status={node.status} className="h-3.5 w-3.5 shrink-0" />
                <span className="flex-1 text-caption text-muted-foreground">{node.label}</span>
                {active ? (
                  <span key={step} className="animate-in fade-in duration-300 motion-reduce:animate-none">
                    {scene.state}
                  </span>
                ) : null}
              </div>
              <div className="mt-2.5 flex items-center gap-2 px-3 text-label font-medium">
                <Avatar actor={node.owner} size={20} />
                {node.owner.name}
              </div>
              <p
                className={cn(
                  "mt-4 px-3 text-caption text-muted-foreground transition-opacity duration-300",
                  index === scene.at && "opacity-0",
                )}
              >
                {node.brief}
              </p>
            </Card>
          );
        })}
        <span
          className="absolute flex h-[26px] w-[160px] items-center gap-1.5 rounded-md border border-surface-border bg-surface px-2 text-caption shadow-[var(--surface-shadow)] transition-[left,top] duration-700 ease-in-out motion-reduce:transition-none"
          style={{ left: chip.x + 12, top: chip.y + 62 }}
        >
          <span className="text-muted-foreground tabular-nums">ACM-142</span>
          <span className="min-w-0 flex-1 truncate font-medium">Rate limiting</span>
        </span>
      </Scene>
    </ZoomCanvas>
  );
}

// --- Runtimes ---------------------------------------------------------------
// The workspace sits in the middle of its machines and sends each one work;
// partway through, a new machine connects and takes a task too.

type Machine = {
  name: string;
  detail: string;
  icon: LucideIcon;
  providers: string[];
  x: number;
  y: number;
};

const MACHINE = { width: 176, height: 66 } as const;
const HUB = { x: 260, y: 195 } as const;

const MACHINES: Machine[] = [
  { name: "Mia’s MacBook Pro", detail: "macOS", icon: Laptop, providers: ["Claude Code", "Codex", "Cursor"], x: 24, y: 24 },
  { name: "build-01", detail: "Linux", icon: Server, providers: ["Claude Code", "OpenCode"], x: 320, y: 24 },
  { name: "Multica Cloud", detail: "Sandboxes", icon: Cloud, providers: ["Claude Code", "Codex"], x: 24, y: 300 },
  { name: "Sam’s Mac mini", detail: "macOS", icon: Laptop, providers: ["Claude Code", "Cursor"], x: 320, y: 300 },
];

function wire(machine: Machine) {
  return `M${HUB.x} ${HUB.y} L${machine.x + MACHINE.width / 2} ${machine.y + MACHINE.height / 2}`;
}

// What each machine shows per step; the fourth one connects at step 3.
const RUNTIME_STATES: ReactNode[][] = [
  [<Muted key="a">Idle</Muted>, <Working key="b" />, <Muted key="c">Idle</Muted>],
  [<Muted key="a">Idle</Muted>, <Working key="b" />, <Muted key="c">Idle</Muted>],
  [<Working key="a" />, <Working key="b" />, <Muted key="c">Idle</Muted>],
  [<Working key="a" />, <Working key="b" />, <Working key="c" />, <ShimmerText key="d" className="text-micro text-muted-foreground">Connecting</ShimmerText>],
  [<Working key="a" />, <Working key="b" />, <Working key="c" />, <span key="d" className="text-micro text-success">Connected</span>],
  [<Working key="a" />, <Working key="b" />, <Working key="c" />, <Working key="d" />],
  [<Working key="a" />, <Working key="b" />, <Working key="c" />, <Working key="d" />],
];

// Which machine receives a task on each step.
const RUNTIME_DISPATCH: (number | null)[] = [null, 0, 2, null, 3, null, null];
const RUNTIME_HOLDS = [900, 1000, 1200, 1500, 1100, 2400, 500] as const;

export function RuntimesVisual() {
  const { ref, step } = useStoryLoop(RUNTIME_HOLDS, 5);
  const states = RUNTIME_STATES[step]!;
  const target = RUNTIME_DISPATCH[step];
  return (
    <ZoomCanvas {...LARGE}>
      <Scene sceneRef={ref} resetting={step === RUNTIME_HOLDS.length - 1}>
        <Lines {...LARGE}>
          {MACHINES.map((machine, index) => {
            const joined = index < states.length;
            return (
              <path
                key={machine.name}
                d={wire(machine)}
                pathLength={1}
                strokeDasharray={1}
                strokeDashoffset={joined ? 0 : 1}
                strokeWidth={1.5}
                className={cn(
                  "transition-[stroke-dashoffset,stroke] duration-700 ease-out motion-reduce:transition-none",
                  target === index ? "stroke-foreground/45" : "stroke-site-line-strong",
                )}
              />
            );
          })}
        </Lines>
        {target != null ? <Pulse key={step} path={wire(MACHINES[target]!)} /> : null}
        <div
          className="absolute flex h-12 w-[148px] items-center gap-2.5 rounded-xl border border-foreground/15 bg-background px-3 shadow-site-float"
          style={{ left: HUB.x - 74, top: HUB.y - 24 }}
        >
          <MulticaIcon className="size-5" />
          <div className="leading-tight">
            <p className="text-label font-medium">Acme Labs</p>
            <Muted>Workspace</Muted>
          </div>
        </div>
        {MACHINES.map((machine, index) => {
          const state = states[index];
          if (state === undefined) return null;
          return (
            <Card
              key={machine.name}
              className={cn("border-border px-3 py-2.5", index === 3 && step === 3 && ENTER)}
              style={{ left: machine.x, top: machine.y, width: MACHINE.width, height: MACHINE.height }}
            >
              <p className="flex items-center gap-2 text-label font-medium">
                <machine.icon className="size-3.5 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate">{machine.name}</span>
              </p>
              <div className="mt-2 flex items-center gap-1">
                {machine.providers.map((provider) => {
                  const Mark = PROVIDER_MARKS.get(provider);
                  return Mark ? <Mark key={provider} className="size-3.5" /> : null;
                })}
                <Muted className="ml-1">{machine.detail}</Muted>
                <span className="ml-auto">{state}</span>
              </div>
            </Card>
          );
        })}
      </Scene>
    </ZoomCanvas>
  );
}

// --- Chat -------------------------------------------------------------------
// A teammate mentions the agent in a channel; it replies with an issue, and
// the issue flies out of the conversation onto the board.

const ISSUE_CHIP = "flex h-7 w-[114px] items-center gap-1.5 rounded-md border bg-background px-2 text-micro";
const BOARD_ISSUES = ["Fix webhooks", "Stripe SDK"];
const CHAT_HOLDS = [900, 1100, 1300, 2600, 400] as const;

function IssueChip({ title, className, style }: { title: string; className?: string; style?: CSSProperties }) {
  return (
    <span className={cn(ISSUE_CHIP, className)} style={style}>
      <StatusIcon status="in_progress" className="h-3 w-3 shrink-0" />
      <span className="min-w-0 flex-1 truncate font-medium">{title}</span>
    </span>
  );
}

export function ChatVisual() {
  const { ref, step } = useStoryLoop(CHAT_HOLDS, 3);
  const filed = step >= 3;
  return (
    <ZoomCanvas {...SMALL}>
      <Scene sceneRef={ref} resetting={step === CHAT_HOLDS.length - 1}>
        <Window className="top-3.5 left-3.5 h-[260px] w-[196px]">
          <WindowHeader icon={Hash} title="payments" />
          <div className="flex flex-col gap-3 p-3 text-caption">
            <div className="flex gap-2">
              <Avatar actor={LEO} size={20} />
              <p className="min-w-0 flex-1 leading-relaxed">
                EU cards fail at checkout.{" "}
                <span className="rounded bg-brand/10 px-0.5 whitespace-nowrap text-brand">@Backend Agent</span> can
                you look?
              </p>
            </div>
            {step === 1 ? (
              <div className={cn(ENTER, "flex items-center gap-2")}>
                <Avatar actor={BACKEND} size={20} />
                <ShimmerText className="text-caption text-muted-foreground">typing…</ShimmerText>
              </div>
            ) : null}
            {step >= 2 ? (
              <div className={cn(ENTER, "flex gap-2")}>
                <Avatar actor={BACKEND} size={20} />
                <div className="min-w-0 flex-1">
                  <p className="leading-relaxed">On it. Filed this and started on the logs.</p>
                  <IssueChip title="EU checkout" className="mt-1.5 border-border" />
                </div>
              </div>
            ) : null}
          </div>
        </Window>
        <div className="absolute top-3.5 left-[222px] w-[126px]">
          <p className="flex h-7 items-center gap-1.5 px-1 text-micro text-muted-foreground">
            <StatusIcon status="in_progress" className="h-3 w-3" />
            <span className="font-medium text-foreground">In Progress</span>
            <span className="tabular-nums">{filed ? 3 : 2}</span>
          </p>
          <div className="relative mt-1 flex flex-col gap-1.5 rounded-lg bg-muted/60 p-1.5">
            {filed ? (
              <IssueChip
                title="EU checkout"
                className="animate-site-fly border-foreground/20 shadow-site-float motion-reduce:animate-none"
                style={{ "--site-fly-from": "-173px 154px" } as CSSProperties}
              />
            ) : null}
            {BOARD_ISSUES.map((title) => (
              <IssueChip key={title} title={title} className="border-border" />
            ))}
          </div>
        </div>
      </Scene>
    </ZoomCanvas>
  );
}

// --- Autopilots -------------------------------------------------------------
// A playhead sweeps today's schedule; each trigger it passes files an issue
// for its agent.

const DAY = { left: 24, width: 312 } as const;
const AUTOPILOT_RUNS: { hour: number; title: string; agent: Actor }[] = [
  { hour: 2, title: "Nightly flaky-test sweep", agent: QA },
  { hour: 9, title: "Triage new bug reports", agent: LEAD },
  { hour: 13, title: "Upgrade dependencies", agent: FRONTEND },
  { hour: 20, title: "Daily usage report", agent: BACKEND },
];
const SWEEP_MS = 1000;
const AUTOPILOT_HOLDS = [500, 1600, 1600, 1600, 1600, 2200, 500] as const;

function hourX(hour: number) {
  return DAY.left + (DAY.width * hour) / 24;
}

function clock(hour: number) {
  return `${String(hour).padStart(2, "0")}:00`;
}

export function AutopilotsVisual() {
  const { ref, step } = useStoryLoop(AUTOPILOT_HOLDS, 5);
  const last = AUTOPILOT_HOLDS.length - 1;
  const head = step === 0 || step === last ? 0 : step <= AUTOPILOT_RUNS.length ? AUTOPILOT_RUNS[step - 1]!.hour : 24;
  return (
    <ZoomCanvas {...SMALL}>
      <Scene sceneRef={ref} resetting={step === last}>
        <p className="absolute top-3 left-6 flex items-center gap-1.5 text-caption text-muted-foreground">
          <Zap className="size-3.5" />
          <span className="font-medium text-foreground">Autopilot</span>· Today
        </p>
        <div className="absolute top-[58px] h-px bg-site-line-strong" style={{ left: DAY.left, width: DAY.width }} />
        {[0, 6, 12, 18, 24].map((hour) => (
          <span key={hour} className="absolute top-[52px] h-[13px] w-px bg-site-line-strong" style={{ left: hourX(hour) }}>
            <span className="absolute top-4 -translate-x-1/2 font-mono text-[10px] text-muted-foreground">{clock(hour)}</span>
          </span>
        ))}
        {AUTOPILOT_RUNS.map((run, index) => (
          <span
            key={run.hour}
            className="absolute top-[28px] flex -translate-x-1/2 flex-col items-center gap-1"
            style={{ left: hourX(run.hour) }}
          >
            <Avatar actor={run.agent} size={16} />
            <span
              className={cn(
                "size-2 rotate-45 border transition-colors duration-300",
                step > index ? "border-foreground bg-foreground" : "border-foreground/40 bg-background",
              )}
            />
          </span>
        ))}
        <span
          className="absolute top-[24px] h-[42px] w-px bg-foreground ease-linear motion-reduce:transition-none"
          style={{
            left: hourX(head),
            transitionProperty: "left",
            transitionDuration: `${step === 0 ? 0 : step === last ? 400 : SWEEP_MS}ms`,
          }}
        >
          <span className="absolute -top-1 -left-[3px] size-[7px] rounded-full bg-foreground" />
        </span>
        <ul className="absolute top-[96px] right-6 left-6 flex flex-col gap-1.5">
          {AUTOPILOT_RUNS.map((run, index) =>
            step > index && step < last ? (
              <li
                key={run.hour}
                className={cn(
                  ENTER,
                  "flex h-7 items-center gap-2 rounded-md border border-border bg-background px-2 text-caption [animation-fill-mode:backwards]",
                )}
                style={{ animationDelay: step === index + 1 ? `${SWEEP_MS}ms` : undefined }}
              >
                <StatusIcon status="todo" className="h-3 w-3 shrink-0" />
                <span className="min-w-0 flex-1 truncate font-medium">{run.title}</span>
                <span className="font-mono text-[10px] text-muted-foreground">{clock(run.hour)}</span>
                <Avatar actor={run.agent} size={16} />
              </li>
            ) : null,
          )}
        </ul>
      </Scene>
    </ZoomCanvas>
  );
}

// --- Skills -----------------------------------------------------------------
// One skill file feeds every agent that uses it: add a line, and the change
// reaches all of them at once.

const SKILL_AGENTS: Actor[] = [BACKEND, FRONTEND, QA, LEAD];
const AGENT_PILL = { x: 212, width: 136, height: 32, top: 28, gap: 50 } as const;
const SKILL_LINES = ["## Before a deploy", "- Run all tests", "- Check migrations"];
const SKILL_HOLDS = [900, 1300, 900, 2400, 500] as const;

function skillWire(index: number) {
  const y = AGENT_PILL.top + index * AGENT_PILL.gap + AGENT_PILL.height / 2;
  return `M178 124 C196 124 194 ${y} ${AGENT_PILL.x} ${y}`;
}

export function SkillsVisual() {
  const { ref, step } = useStoryLoop(SKILL_HOLDS, 3);
  return (
    <ZoomCanvas {...SMALL}>
      <Scene sceneRef={ref} resetting={step === SKILL_HOLDS.length - 1}>
        <Lines {...SMALL}>
          {SKILL_AGENTS.map((agent, index) => (
            <Edge key={agent.name} d={skillWire(index)} lit={step === 2} />
          ))}
        </Lines>
        {step === 2
          ? SKILL_AGENTS.map((agent, index) => <Pulse key={agent.name} path={skillWire(index)} delay={index * 60} />)
          : null}
        <Window className="top-3.5 left-3.5 h-[260px] w-[164px]">
          <div className="flex h-9 shrink-0 items-center gap-1.5 border-b border-border px-3 text-caption">
            <FileText className="size-3.5 text-muted-foreground" />
            <span className="truncate font-mono text-micro font-medium">release-checklist</span>
          </div>
          <div className="flex flex-col gap-1 p-3 font-mono text-[10.5px] leading-4">
            {SKILL_LINES.map((line) => (
              <p key={line} className={cn(line.startsWith("#") && "text-muted-foreground")}>
                {line}
              </p>
            ))}
            {step >= 1 ? (
              <p className={cn(ENTER, "-mx-1 rounded bg-success/10 px-1 text-success")}>+ Smoke-test staging</p>
            ) : null}
            <p>- Post in #releases</p>
          </div>
        </Window>
        {SKILL_AGENTS.map((agent, index) => (
          <Card
            key={agent.name}
            className="flex items-center gap-2 border-border px-2 text-caption"
            style={{
              left: AGENT_PILL.x,
              top: AGENT_PILL.top + index * AGENT_PILL.gap,
              width: AGENT_PILL.width,
              height: AGENT_PILL.height,
            }}
          >
            <Avatar actor={agent} size={20} />
            <span className="min-w-0 flex-1 truncate font-medium">{agent.name}</span>
            {step === 3 ? (
              <span
                className="absolute -top-1.5 -right-1.5 flex size-4 animate-in items-center justify-center rounded-full bg-success text-background ring-2 ring-site-sunken fade-in zoom-in-50 duration-300 [animation-fill-mode:backwards] motion-reduce:animate-none"
                style={{ animationDelay: `${index * 80}ms` }}
              >
                <Check className="size-2.5" />
              </span>
            ) : null}
          </Card>
        ))}
      </Scene>
    </ZoomCanvas>
  );
}
