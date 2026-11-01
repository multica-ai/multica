"use client";

import type { ReactNode } from "react";
import {
  ArrowUp,
  BookOpenText,
  ChevronDown,
  Cloud,
  CornerDownRight,
  Hash,
  Laptop,
  Plus,
  Server,
  SlidersHorizontal,
  Workflow,
  Zap,
  type IconComponent,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import type { Status } from "./hero-board";
import {
  AppWindow,
  Avatar,
  BACKEND,
  BreadcrumbHeader,
  CONTENT,
  FRONTEND,
  HeaderButton,
  LEAD,
  LIST,
  LIST_CELL,
  ListGroup,
  ListHead,
  ListRow,
  MIA,
  NameCell,
  PageHeader,
  PROVIDER_MARKS,
  ProviderStack,
  QA,
  RAISED,
  TRIAGE,
  Working,
  ZoomCanvas,
  type Actor,
  type Space,
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

function Screen({ active, children }: { active: Space; children: ReactNode }) {
  return (
    <ZoomCanvas {...CANVAS}>
      <AppWindow className="top-8 left-8 -right-12 -bottom-12" active={active} bleed={BLEED}>
        {children}
      </AppWindow>
    </ZoomCanvas>
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

// --- Runtimes ---------------------------------------------------------------

type Runtime = { name: string; icon: IconComponent; detail: string; tools: string[]; fresh?: boolean };

const MACHINES: Runtime[] = [
  { name: "build-02", icon: Server, detail: "Linux · Leo Park", tools: ["Claude Code", "Codex", "OpenCode"], fresh: true },
  { name: "Mia’s MacBook Pro", icon: Laptop, detail: "This device", tools: ["Claude Code", "Cursor"] },
];

const CLOUD: Runtime[] = [{ name: "Multica Cloud", icon: Cloud, detail: "Cloud sandboxes", tools: ["Claude Code", "Codex"] }];

const RUNTIME_COLUMNS = "minmax(0,1fr) auto";

function RuntimeRow({ runtime }: { runtime: Runtime }) {
  return (
    <ListRow columns={RUNTIME_COLUMNS} className={cn(runtime.fresh && "bg-brand/5")}>
      <NameCell
        icon={<runtime.icon className="size-4 shrink-0 text-muted-foreground" />}
        title={runtime.name}
        description={runtime.detail}
      />
      <span className={LIST_CELL}>
        <ProviderStack names={runtime.tools} />
      </span>
    </ListRow>
  );
}

export function RuntimesVisual() {
  return (
    <Screen active="team">
      <PageHeader icon={Server} title="All runtimes" filter={false}>
        <HeaderButton icon={Plus} />
      </PageHeader>
      <div className={LIST}>
        <ListGroup title="Machines" count={MACHINES.length} />
        {MACHINES.map((runtime) => (
          <RuntimeRow key={runtime.name} runtime={runtime} />
        ))}
        <ListGroup title="Cloud services" count={CLOUD.length} />
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
    <div className="flex h-8 items-center gap-1.5 rounded-md border border-border bg-background px-2 text-caption">
      <span className="flex min-w-0 flex-1 items-center gap-1.5 truncate">{children}</span>
      <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
    </div>
  );
}

export function WorkflowVisual() {
  return (
    <Screen active="settings">
      <BreadcrumbHeader
        parent={
          <>
            <Workflow className="size-3.5" /> Workflows
          </>
        }
        leaf="Ship a feature"
      />
      <div className="flex min-h-0 flex-1 gap-3 p-3">
        <ol className="flex w-[150px] shrink-0 flex-col gap-1.5 self-start rounded-xl bg-muted/40 p-1.5">
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
          <Field label="Handoff instructions">
            <div className="rounded-md border border-border bg-background px-2.5 py-2 text-caption leading-snug">
              Build to the spec, then open a pull request.
            </div>
          </Field>
        </div>
      </div>
    </Screen>
  );
}

// --- Chat -------------------------------------------------------------------

/*
 * The chat card is the team's own chat app, not Multica: a Slack-style
 * workspace where the agent is an installed app, so the frame stays generic
 * (no vendor logo) and only Multica's link preview is ours.
 */

const CHANNELS = ["general", "eng-support", "releases", "random"];

function ChatAvatar({ actor }: { actor: Actor }) {
  if (actor.kind === "agent") {
    const Mark = PROVIDER_MARKS.get(actor.provider);
    return (
      <span className="flex size-7 shrink-0 items-center justify-center rounded-md border border-border bg-background">
        {Mark ? <Mark className="size-4" /> : null}
      </span>
    );
  }
  return (
    <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-[#e9d8c4] text-micro font-semibold text-[#6b4a2b]">
      {actor.initials}
    </span>
  );
}

export function ChatMessage({ actor, time, children }: { actor: Actor; time: string; children: ReactNode }) {
  return (
    <div className="flex gap-2.5">
      <ChatAvatar actor={actor} />
      <div className="min-w-0 flex-1">
        <p className="flex items-center gap-1.5">
          <span className="text-label font-bold">{actor.name}</span>
          {actor.kind === "agent" ? (
            <span className="rounded-sm bg-muted px-1 text-micro font-semibold text-muted-foreground">APP</span>
          ) : null}
          <span className="text-micro text-muted-foreground tabular-nums">{time}</span>
        </p>
        <div className="text-label leading-snug">{children}</div>
      </div>
    </div>
  );
}

export function ChatVisual() {
  return (
    <ZoomCanvas {...CANVAS}>
      <div className="absolute top-8 -right-12 -bottom-12 left-8 flex overflow-hidden rounded-xl border border-site-line-strong bg-background shadow-site-float">
        <div className="flex w-[124px] shrink-0 flex-col gap-4 bg-[#3f0e40] px-2 py-3 text-caption text-[#d6c8d6]">
          <p className="flex items-center gap-1 px-1.5 text-label font-bold whitespace-nowrap text-white">
            Acme Labs <ChevronDown className="size-3.5" />
          </p>
          <div>
            <p className="px-1.5 pb-1 text-micro">Channels</p>
            {CHANNELS.map((channel) => (
              <p
                key={channel}
                className={cn(
                  "flex items-center gap-1.5 rounded-md px-1.5 py-1",
                  channel === "eng-support" && "bg-[#1164a3] font-semibold text-white",
                )}
              >
                <Hash className="size-3 shrink-0" />
                <span className="truncate">{channel}</span>
              </p>
            ))}
          </div>
          <div>
            <p className="px-1.5 pb-1 text-micro">Apps</p>
            <p className="flex items-center gap-1.5 rounded-md px-1.5 py-1">
              <span className="flex size-3.5 shrink-0 items-center justify-center rounded-sm bg-white text-[#3f0e40]">
                <MulticaIcon className="size-2.5" />
              </span>
              Multica
            </p>
          </div>
        </div>
        <div className="flex min-w-0 flex-1 flex-col" style={{ marginRight: BLEED, marginBottom: BLEED }}>
          <div className="flex h-11 shrink-0 items-center gap-1 border-b border-border px-4">
            <Hash className="size-3.5" />
            <span className="text-label font-bold">eng-support</span>
            <ChevronDown className="size-3.5 text-muted-foreground" />
          </div>
          <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-hidden px-4 py-3">
            <ChatMessage actor={MIA} time="11:55 AM">
              Exports over 10k rows time out.{" "}
              <span className="rounded-sm bg-[#e8f5fa] px-0.5 text-[#1264a3]">@Backend Agent</span> can you look?
            </ChatMessage>
            <ChatMessage actor={BACKEND} time="11:56 AM">
              Filed it and started a fix.
              <div className="mt-1.5 border-l-4 border-border pl-2.5">
                <p className="flex items-center gap-1 text-micro text-muted-foreground">
                  <MulticaIcon className="size-2.5" /> Multica
                </p>
                <p className="mt-0.5 truncate font-bold text-[#1264a3]">ACM-151 CSV export times out</p>
                <p className="mt-0.5 flex items-center gap-1.5 text-caption text-muted-foreground">
                  <StatusIcon status="in_progress" className="h-3 w-3 shrink-0" />
                  In progress
                  <Working />
                </p>
              </div>
            </ChatMessage>
          </div>
          <div className="shrink-0 px-4 pb-3">
            <div className="flex h-9 items-center justify-between rounded-lg border border-site-line-strong px-3 text-label text-muted-foreground">
              Message #eng-support
              <span className="flex items-center gap-2">
                <Plus className="size-3.5" />
                <ArrowUp className="size-3.5" />
              </span>
            </div>
          </div>
        </div>
      </div>
    </ZoomCanvas>
  );
}

// --- Autopilots -------------------------------------------------------------

const AUTOPILOTS: { name: string; next: string; agent: Actor }[] = [
  { name: "Nightly triage", next: "Tonight 02:00", agent: TRIAGE },
  { name: "Flaky tests", next: "In 4 hours", agent: QA },
  { name: "Dependency bumps", next: "Mon 09:00", agent: BACKEND },
  { name: "Weekly digest", next: "Fri 17:00", agent: CONTENT },
  { name: "Visual checks", next: "Next release", agent: FRONTEND },
];

const AUTOPILOT_COLUMNS = "minmax(0,1fr) 44px 84px";

export function AutopilotsVisual() {
  return (
    <Screen active="autopilots">
      <PageHeader icon={Zap} title="All autopilots">
        <HeaderButton icon={SlidersHorizontal} />
        <HeaderButton icon={Plus} />
      </PageHeader>
      <div className={LIST}>
        <ListHead columns={AUTOPILOT_COLUMNS}>
          <span>Name</span>
          <span>Agent</span>
          <span>Next run</span>
        </ListHead>
        {AUTOPILOTS.map((autopilot) => (
          <ListRow key={autopilot.name} columns={AUTOPILOT_COLUMNS}>
            <NameCell title={autopilot.name} />
            <span className={LIST_CELL}>
              <Avatar actor={autopilot.agent} size={20} />
            </span>
            <span className={cn(LIST_CELL, "tabular-nums")}>
              <span className="truncate">{autopilot.next}</span>
            </span>
          </ListRow>
        ))}
      </div>
    </Screen>
  );
}

// --- Skills -----------------------------------------------------------------

const SKILLS: { name: string; agents: Actor[] }[] = [
  { name: "Write release notes", agents: [LEAD, CONTENT, FRONTEND, BACKEND] },
  { name: "Review a migration", agents: [BACKEND, QA] },
  { name: "Triage a bug report", agents: [TRIAGE, QA, LEAD] },
  { name: "Deploy checklist", agents: [BACKEND] },
  { name: "Reply to a customer", agents: [CONTENT, TRIAGE] },
];

const SKILL_COLUMNS = "minmax(0,1fr) 84px";

function UsedBy({ agents }: { agents: Actor[] }) {
  const extra = agents.length - 3;
  return (
    <span className="flex items-center -space-x-1.5">
      {agents.slice(0, 3).map((agent) => (
        <span key={agent.name} className="inline-flex rounded-full ring-2 ring-page-canvas">
          <Avatar actor={agent} size={20} />
        </span>
      ))}
      {extra > 0 ? (
        <span className="inline-flex size-5 items-center justify-center rounded-full bg-muted text-micro font-medium text-muted-foreground ring-2 ring-page-canvas">
          +{extra}
        </span>
      ) : null}
    </span>
  );
}

export function SkillsVisual() {
  return (
    <Screen active="team">
      <PageHeader icon={BookOpenText} title="All skills">
        <HeaderButton icon={SlidersHorizontal} />
        <HeaderButton icon={Plus} />
      </PageHeader>
      <div className={LIST}>
        <ListHead columns={SKILL_COLUMNS}>
          <span>Name</span>
          <span>Used by</span>
        </ListHead>
        {SKILLS.map((skill, index) => (
          <ListRow key={skill.name} columns={SKILL_COLUMNS} className={cn(index === 0 && "bg-surface-selected")}>
            <NameCell icon={<BookOpenText className="size-4 shrink-0 text-muted-foreground" />} title={skill.name} />
            <span className={LIST_CELL}>
              <UsedBy agents={skill.agents} />
            </span>
          </ListRow>
        ))}
      </div>
    </Screen>
  );
}
