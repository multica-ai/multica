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
import { useLocale } from "../../i18n";
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
  useDemoCopy,
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
    <span className="flex min-w-0 items-center gap-1.5 rounded-md border border-border py-0.5 pr-2 pl-0.5 text-caption">
      <Avatar actor={actor} size={16} />
      <span className="truncate">{actor.name}</span>
    </span>
  );
}

// --- Runtimes ---------------------------------------------------------------

type Runtime = { name: string; icon: IconComponent; detail: string; tools: string[]; fresh?: boolean };

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
  const copy = useDemoCopy().features.runtimes;
  const machines: Runtime[] = [
    { name: "build-02", icon: Server, detail: copy.server, tools: ["Claude Code", "Codex", "OpenCode"], fresh: true },
    { name: copy.laptop, icon: Laptop, detail: copy.thisDevice, tools: ["Claude Code", "Cursor"] },
  ];
  const cloud: Runtime[] = [{ name: "Multica Cloud", icon: Cloud, detail: copy.sandboxes, tools: ["Claude Code", "Codex"] }];
  return (
    <Screen active="team">
      <PageHeader icon={Server} title={copy.title} filter={false}>
        <HeaderButton icon={Plus} />
      </PageHeader>
      <div className={LIST}>
        <ListGroup title={copy.machines} count={machines.length} />
        {machines.map((runtime) => (
          <RuntimeRow key={runtime.name} runtime={runtime} />
        ))}
        <ListGroup title={copy.cloud} count={cloud.length} />
        {cloud.map((runtime) => (
          <RuntimeRow key={runtime.name} runtime={runtime} />
        ))}
      </div>
    </Screen>
  );
}

// --- Workflows --------------------------------------------------------------

const STEPS: Omit<Status, "name">[] = [
  { key: "spec", category: "unstarted", icon: "circle", color: "#8b5cf6", handler: LEAD },
  { key: "build", category: "started", icon: "half", color: "#f97316", handler: BACKEND },
  { key: "review", category: "started", icon: "three_quarters", color: "#3b82f6", handler: QA },
  { key: "approve", category: "started", icon: "three_quarters", color: "#ec4899", handler: MIA },
  { key: "shipped", category: "done", icon: "check", color: "#22c55e" },
];

function StepIcon({ step, className }: { step: Omit<Status, "name">; className: string }) {
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
  const { t } = useLocale();
  const copy = t.demo.features.workflow;
  const names = t.demo.hero.ship.statuses;
  const steps: Status[] = STEPS.map((step) => ({ ...step, name: names[step.key as keyof typeof names] }));
  const selected = steps[1]!;
  return (
    <Screen active="settings">
      <BreadcrumbHeader
        parent={
          <>
            <Workflow className="size-3.5" /> {copy.breadcrumb}
          </>
        }
        leaf={t.home.hero.workflows.ship.label}
      />
      <div className="flex min-h-0 flex-1 gap-3 p-3">
        <ol className="flex w-[150px] shrink-0 flex-col gap-1.5 self-start rounded-xl bg-muted/40 p-1.5">
          {steps.map((step) => (
            <li key={step.key} className={cn(RAISED, "px-2.5 py-2", step === selected && "ring-2 ring-brand/50")}>
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
                  copy.finalStep
                )}
              </div>
            </li>
          ))}
        </ol>
        <div className="flex min-w-0 flex-1 flex-col gap-3 pt-1">
          <p className="flex items-center gap-2">
            <StepIcon step={selected} className="h-4 w-4 shrink-0" />
            <span className="text-label font-semibold">{selected.name}</span>
          </p>
          <Field label={copy.onEnter}>
            <Select>
              <Avatar actor={BACKEND} size={16} /> {BACKEND.name}
            </Select>
          </Field>
          <Field label={copy.next}>
            <Select>
              <StepIcon step={steps[2]!} className="h-3.5 w-3.5 shrink-0" /> {steps[2]!.name}
            </Select>
          </Field>
          <Field label={copy.instructions}>
            <div className="rounded-md border border-border bg-background px-2.5 py-2 text-caption leading-snug">
              {copy.instructionsBody}
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
  const { locale, t } = useLocale();
  const copy = t.demo.features.chat;
  const time = new Intl.DateTimeFormat(locale, { hour: "numeric", minute: "2-digit", timeZone: "UTC" });
  const at = (minute: number) => time.format(Date.UTC(2026, 5, 9, 11, minute));
  return (
    <ZoomCanvas {...CANVAS}>
      <div className="absolute top-8 -right-12 -bottom-12 left-8 flex overflow-hidden rounded-xl border border-site-line-strong bg-background shadow-site-float">
        <div className="flex w-[124px] shrink-0 flex-col gap-4 bg-[#3f0e40] px-2 py-3 text-caption text-[#d6c8d6]">
          <p className="flex items-center gap-1 px-1.5 text-label font-bold whitespace-nowrap text-white">
            Acme Labs <ChevronDown className="size-3.5" />
          </p>
          <div>
            <p className="px-1.5 pb-1 text-micro">{copy.channels}</p>
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
            <p className="px-1.5 pb-1 text-micro">{copy.apps}</p>
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
            <ChatMessage actor={MIA} time={at(55)}>
              {copy.ask.before}{" "}
              <span className="rounded-sm bg-[#e8f5fa] px-0.5 text-[#1264a3]">@{BACKEND.name}</span> {copy.ask.after}
            </ChatMessage>
            <ChatMessage actor={BACKEND} time={at(56)}>
              {copy.reply}
              <div className="mt-1.5 border-l-4 border-border pl-2.5">
                <p className="flex items-center gap-1 text-micro text-muted-foreground">
                  <MulticaIcon className="size-2.5" /> Multica
                </p>
                <p className="mt-0.5 truncate font-bold text-[#1264a3]">ACM-151 {copy.issue}</p>
                <p className="mt-0.5 flex items-center gap-1.5 text-caption text-muted-foreground">
                  <StatusIcon status="in_progress" className="h-3 w-3 shrink-0" />
                  {t.demo.app.inProgress}
                  <Working />
                </p>
              </div>
            </ChatMessage>
          </div>
          <div className="shrink-0 px-4 pb-3">
            <div className="flex h-9 items-center justify-between rounded-lg border border-site-line-strong px-3 text-label text-muted-foreground">
              {copy.composer.replace("{channel}", "eng-support")}
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

const AUTOPILOT_AGENTS: Actor[] = [TRIAGE, QA, BACKEND, CONTENT, FRONTEND];

const AUTOPILOT_COLUMNS = "minmax(0,1fr) auto 84px";

export function AutopilotsVisual() {
  const copy = useDemoCopy().features.autopilots;
  return (
    <Screen active="autopilots">
      <PageHeader icon={Zap} title={copy.title}>
        <HeaderButton icon={SlidersHorizontal} />
        <HeaderButton icon={Plus} />
      </PageHeader>
      <div className={LIST}>
        <ListHead columns={AUTOPILOT_COLUMNS}>
          <span>{copy.name}</span>
          <span>{copy.agent}</span>
          <span>{copy.nextRun}</span>
        </ListHead>
        {AUTOPILOT_AGENTS.map((agent, index) => {
          const autopilot = copy.rows[index];
          return autopilot ? (
          <ListRow key={autopilot.name} columns={AUTOPILOT_COLUMNS}>
            <NameCell title={autopilot.name} />
            <span className={LIST_CELL}>
              <Avatar actor={agent} size={20} />
            </span>
            <span className={cn(LIST_CELL, "tabular-nums")}>
              <span className="truncate">{autopilot.next}</span>
            </span>
          </ListRow>
          ) : null;
        })}
      </div>
    </Screen>
  );
}

// --- Skills -----------------------------------------------------------------

const SKILL_AGENTS: Actor[][] = [[LEAD, CONTENT, FRONTEND, BACKEND], [BACKEND, QA], [TRIAGE, QA, LEAD], [BACKEND], [CONTENT, TRIAGE]];

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
  const copy = useDemoCopy().features.skills;
  return (
    <Screen active="team">
      <PageHeader icon={BookOpenText} title={copy.title}>
        <HeaderButton icon={SlidersHorizontal} />
        <HeaderButton icon={Plus} />
      </PageHeader>
      <div className={LIST}>
        <ListHead columns={SKILL_COLUMNS}>
          <span>{copy.name}</span>
          <span>{copy.usedBy}</span>
        </ListHead>
        {SKILL_AGENTS.map((agents, index) => (
          <ListRow key={index} columns={SKILL_COLUMNS} className={cn(index === 0 && "bg-surface-selected")}>
            <NameCell icon={<BookOpenText className="size-4 shrink-0 text-muted-foreground" />} title={copy.rows[index]} />
            <span className={LIST_CELL}>
              <UsedBy agents={agents} />
            </span>
          </ListRow>
        ))}
      </div>
    </Screen>
  );
}
