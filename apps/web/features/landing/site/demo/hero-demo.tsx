"use client";

import { useMemo, useState } from "react";
import { GitPullRequest, Inbox, Megaphone, UserRound, Zap } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useLocale } from "../../i18n";
import type { DemoDict } from "../../i18n/types";
import { focusRing } from "../primitives";
import { BarList, Checklist, DocPreview, LoadChart, PullRequest, Replies, ReportCluster, TimeoutChart } from "./hero-blocks";
import { HeroBoard, type Card, type Priority, type Status, type Workflow } from "./hero-board";
import { BACKEND, CONTENT, FRONTEND, LEAD, LEO, MIA, QA, RESEARCH, SAM, TRIAGE, formatAge, type Actor, type Age } from "./mock-kit";

/*
 * The hero demo: three workflows, each with its own custom statuses and
 * handoffs, played on the same board. The switcher advances on its own each
 * time a story finishes, until a visitor picks one — then that one loops.
 * Each workflow's shape lives here; its words come from the page's language.
 */

type StatusShape = Omit<Status, "name">;
type CardShape = { id: string; priority: Priority; assignee?: Actor; working?: boolean; age: Age };

const SHIP_STATUSES: StatusShape[] = [
  { key: "spec", category: "unstarted", icon: "circle", color: "#8b5cf6", handler: LEAD },
  { key: "build", category: "started", icon: "half", color: "#f97316", handler: BACKEND },
  { key: "review", category: "started", icon: "three_quarters", color: "#3b82f6", handler: QA },
  { key: "approve", category: "started", icon: "three_quarters", color: "#ec4899", handler: MIA },
  { key: "shipped", category: "done", icon: "check", color: "#22c55e" },
];

const SHIP_CARDS: Record<string, CardShape[]> = {
  spec: [
    { id: "ACM-145", priority: "medium", assignee: LEAD, age: "1h" },
    { id: "ACM-144", priority: "low", assignee: LEAD, age: "3h" },
    { id: "ACM-143", priority: "medium", assignee: LEAD, age: "4h" },
    { id: "ACM-141", priority: "medium", assignee: LEAD, age: "6h" },
    { id: "ACM-140", priority: "low", assignee: MIA, age: "1d" },
  ],
  build: [
    { id: "ACM-136", priority: "high", assignee: BACKEND, working: true, age: "2m" },
    { id: "ACM-138", priority: "low", assignee: BACKEND, age: "40m" },
    { id: "ACM-135", priority: "medium", assignee: BACKEND, age: "1h" },
    { id: "ACM-134", priority: "medium", assignee: FRONTEND, age: "2h" },
  ],
  review: [
    { id: "ACM-133", priority: "urgent", assignee: QA, working: true, age: "now" },
    { id: "ACM-131", priority: "medium", assignee: QA, age: "1h" },
    { id: "ACM-127", priority: "low", assignee: QA, age: "2h" },
    { id: "ACM-126", priority: "medium", assignee: QA, age: "3h" },
    { id: "ACM-124", priority: "low", assignee: QA, age: "5h" },
  ],
  approve: [
    { id: "ACM-129", priority: "medium", assignee: MIA, age: "2h" },
    { id: "ACM-119", priority: "low", assignee: MIA, age: "3h" },
    { id: "ACM-118", priority: "medium", assignee: MIA, age: "4h" },
    { id: "ACM-117", priority: "high", assignee: MIA, age: "6h" },
  ],
  shipped: [
    { id: "ACM-125", priority: "medium", assignee: MIA, age: "1h" },
    { id: "ACM-122", priority: "high", assignee: LEO, age: "3h" },
    { id: "ACM-120", priority: "medium", assignee: MIA, age: "1d" },
    { id: "ACM-114", priority: "medium", assignee: LEO, age: "1d" },
    { id: "ACM-113", priority: "high", assignee: MIA, age: "2d" },
    { id: "ACM-112", priority: "medium", assignee: LEO, age: "2d" },
  ],
};

const LAUNCH_STATUSES: StatusShape[] = [
  { key: "brief", category: "unstarted", icon: "circle", color: "#64748b", handler: SAM },
  { key: "research", category: "started", icon: "half", color: "#06b6d4", handler: RESEARCH },
  { key: "draft", category: "started", icon: "half", color: "#8b5cf6", handler: CONTENT },
  { key: "edit", category: "started", icon: "three_quarters", color: "#ec4899", handler: SAM },
  { key: "published", category: "done", icon: "check", color: "#22c55e" },
];

const LAUNCH_CARDS: Record<string, CardShape[]> = {
  brief: [
    { id: "ACM-213", priority: "low", assignee: SAM, age: "2h" },
    { id: "ACM-211", priority: "medium", assignee: SAM, age: "5h" },
    { id: "ACM-210", priority: "medium", assignee: SAM, age: "6h" },
    { id: "ACM-208", priority: "low", assignee: MIA, age: "1d" },
    { id: "ACM-207", priority: "medium", assignee: SAM, age: "1d" },
  ],
  research: [
    { id: "ACM-209", priority: "medium", assignee: RESEARCH, working: true, age: "3m" },
    { id: "ACM-203", priority: "high", assignee: RESEARCH, age: "1h" },
    { id: "ACM-202", priority: "medium", assignee: RESEARCH, age: "2h" },
    { id: "ACM-201", priority: "low", assignee: RESEARCH, age: "4h" },
  ],
  draft: [
    { id: "ACM-204", priority: "high", assignee: CONTENT, working: true, age: "now" },
    { id: "ACM-199", priority: "medium", assignee: CONTENT, age: "1h" },
    { id: "ACM-195", priority: "medium", assignee: CONTENT, age: "2h" },
    { id: "ACM-194", priority: "medium", assignee: CONTENT, age: "3h" },
    { id: "ACM-192", priority: "low", assignee: CONTENT, age: "5h" },
  ],
  edit: [
    { id: "ACM-198", priority: "medium", assignee: SAM, age: "20m" },
    { id: "ACM-196", priority: "medium", assignee: SAM, age: "3h" },
    { id: "ACM-188", priority: "low", assignee: SAM, age: "4h" },
    { id: "ACM-186", priority: "medium", assignee: SAM, age: "5h" },
  ],
  published: [
    { id: "ACM-193", priority: "medium", assignee: SAM, age: "1h" },
    { id: "ACM-190", priority: "high", assignee: MIA, age: "4h" },
    { id: "ACM-187", priority: "medium", assignee: SAM, age: "1d" },
    { id: "ACM-182", priority: "medium", assignee: CONTENT, age: "2d" },
    { id: "ACM-181", priority: "medium", assignee: SAM, age: "3d" },
    { id: "ACM-180", priority: "low", assignee: CONTENT, age: "4d" },
  ],
};

const TRIAGE_STATUSES: StatusShape[] = [
  { key: "inbox", category: "unstarted", icon: "dotted", color: "#64748b", handler: TRIAGE },
  { key: "repro", category: "started", icon: "half", color: "#f97316", handler: QA },
  { key: "fix", category: "started", icon: "half", color: "#3b82f6", handler: BACKEND },
  { key: "reply", category: "started", icon: "three_quarters", color: "#8b5cf6", handler: TRIAGE },
  { key: "resolved", category: "done", icon: "check", color: "#22c55e" },
];

const TRIAGE_CARDS: Record<string, CardShape[]> = {
  inbox: [
    { id: "ACM-150", priority: "low", assignee: TRIAGE, age: "2h" },
    { id: "ACM-148", priority: "medium", assignee: TRIAGE, age: "4h" },
    { id: "ACM-149", priority: "low", assignee: TRIAGE, age: "5h" },
    { id: "ACM-147", priority: "medium", assignee: TRIAGE, age: "6h" },
    { id: "ACM-145", priority: "low", assignee: TRIAGE, age: "8h" },
  ],
  repro: [
    { id: "ACM-146", priority: "medium", assignee: QA, working: true, age: "5m" },
    { id: "ACM-142", priority: "medium", assignee: QA, age: "1h" },
    { id: "ACM-141", priority: "low", assignee: QA, age: "3h" },
    { id: "ACM-140", priority: "high", assignee: QA, age: "4h" },
  ],
  fix: [
    { id: "ACM-139", priority: "high", assignee: FRONTEND, working: true, age: "4m" },
    { id: "ACM-137", priority: "high", assignee: BACKEND, age: "1h" },
    { id: "ACM-135", priority: "high", assignee: BACKEND, age: "2h" },
    { id: "ACM-134", priority: "low", assignee: FRONTEND, age: "3h" },
    { id: "ACM-133", priority: "medium", assignee: BACKEND, age: "5h" },
  ],
  reply: [
    { id: "ACM-132", priority: "low", assignee: TRIAGE, age: "30m" },
    { id: "ACM-129", priority: "low", assignee: TRIAGE, age: "1h" },
    { id: "ACM-127", priority: "medium", assignee: TRIAGE, age: "2h" },
    { id: "ACM-126", priority: "medium", assignee: TRIAGE, age: "4h" },
  ],
  resolved: [
    { id: "ACM-128", priority: "urgent", assignee: TRIAGE, age: "2h" },
    { id: "ACM-124", priority: "high", assignee: TRIAGE, age: "5h" },
    { id: "ACM-121", priority: "low", assignee: TRIAGE, age: "8h" },
    { id: "ACM-122", priority: "low", assignee: TRIAGE, age: "1d" },
    { id: "ACM-120", priority: "medium", assignee: TRIAGE, age: "1d" },
    { id: "ACM-119", priority: "low", assignee: TRIAGE, age: "2d" },
  ],
};

/** Every card the board shows, by workflow, so the dictionaries can be checked for a title per card. */
export const HERO_CARD_IDS = {
  ship: Object.values(SHIP_CARDS).flat().map((card) => card.id),
  launch: Object.values(LAUNCH_CARDS).flat().map((card) => card.id),
  triage: Object.values(TRIAGE_CARDS).flat().map((card) => card.id),
};

function named(shapes: StatusShape[], names: Record<string, string>): Status[] {
  return shapes.map((shape) => ({ ...shape, name: names[shape.key] ?? shape.key }));
}

function columns(
  shapes: Record<string, CardShape[]>,
  titles: Record<string, string>,
  ago: DemoDict["app"]["ago"],
): Workflow["cards"] {
  return Object.fromEntries(
    Object.entries(shapes).map(([status, cards]) => [
      status,
      {
        total: cards.length,
        cards: cards.map(({ age, ...card }): Card => ({ ...card, title: titles[card.id] ?? card.id, updated: formatAge(age, ago) })),
      },
    ]),
  );
}

function buildWorkflows({ app, hero }: DemoDict, due: (month: number, day: number) => string) {
  const { ship, launch, triage, blocks } = hero;
  const checks = (tests: number) => [blocks.checks.build, blocks.tests.replace("{count}", String(tests)), blocks.checks.lint];

  const SHIP: Workflow = {
    id: "ship",
    project: ship.project,
    statuses: named(SHIP_STATUSES, ship.statuses),
    cards: columns(SHIP_CARDS, ship.cards, app.ago),
    focus: { id: "ACM-142", priority: "high", due: due(6, 12), ...ship.focus },
    timeline: [
      { kind: "created", by: "Mia Chen", icon: <UserRound className="size-3.5" />, handoff: LEAD },
      {
        kind: "comment",
        actor: LEAD,
        body: (
          <>
            {ship.plan.comment}
            <Checklist title={ship.plan.title} items={ship.plan.items} />
          </>
        ),
      },
      { kind: "status", actor: LEAD, from: "spec", to: "build", handoff: BACKEND },
      {
        kind: "comment",
        actor: BACKEND,
        body: (
          <>
            {ship.built.comment}
            <PullRequest number={318} title={ship.built.pr} added={184} removed={12} checks={checks(212)} />
          </>
        ),
      },
      { kind: "status", actor: BACKEND, from: "build", to: "review", handoff: QA },
      {
        kind: "comment",
        actor: QA,
        body: (
          <>
            {ship.tested}
            <LoadChart />
          </>
        ),
      },
      { kind: "status", actor: QA, from: "review", to: "approve", handoff: MIA },
      { kind: "comment", actor: MIA, body: ship.approved },
      { kind: "status", actor: MIA, from: "approve", to: "shipped" },
    ],
    stages: [
      { status: "spec", panel: false, entries: 1, working: true, hold: 2400 },
      { status: "spec", panel: true, entries: 1, working: true, hold: 1500 },
      { status: "spec", panel: true, entries: 2, working: true, hold: 2600 },
      { status: "build", panel: true, entries: 3, working: true, hold: 2000 },
      { status: "build", panel: true, entries: 4, working: true, hold: 2700 },
      { status: "review", panel: true, entries: 5, working: true, hold: 2000 },
      { status: "review", panel: true, entries: 6, working: true, hold: 2700 },
      { status: "approve", panel: true, entries: 7, working: false, hold: 2000 },
      { status: "approve", panel: true, entries: 8, working: false, hold: 1800 },
      { status: "shipped", panel: true, entries: 9, working: false, hold: 2600 },
      { status: "shipped", panel: false, entries: 9, working: false, hold: 3000 },
    ],
    still: 6,
  };

  const LAUNCH: Workflow = {
    id: "launch",
    project: launch.project,
    statuses: named(LAUNCH_STATUSES, launch.statuses),
    cards: columns(LAUNCH_CARDS, launch.cards, app.ago),
    focus: { id: "ACM-212", priority: "high", due: due(6, 18), ...launch.focus },
    timeline: [
      { kind: "created", by: "Sam Rivera", icon: <UserRound className="size-3.5" /> },
      { kind: "status", actor: SAM, from: "brief", to: "research", handoff: RESEARCH },
      {
        kind: "comment",
        actor: RESEARCH,
        body: (
          <>
            {launch.research.comment}
            <BarList
              title={launch.research.title}
              aside={launch.research.aside}
              bars={[18, 12, 9].map((value, index) => ({ label: launch.research.bars[index] ?? "", value }))}
            />
          </>
        ),
      },
      { kind: "status", actor: RESEARCH, from: "research", to: "draft", handoff: CONTENT },
      {
        kind: "comment",
        actor: CONTENT,
        body: (
          <>
            {launch.draft.comment}
            <DocPreview title="autopilots-launch.md" meta={launch.draft.meta} />
          </>
        ),
      },
      { kind: "status", actor: CONTENT, from: "draft", to: "edit", handoff: SAM },
      { kind: "comment", actor: SAM, body: launch.edited },
      { kind: "status", actor: SAM, from: "edit", to: "published" },
    ],
    stages: [
      { status: "brief", panel: false, entries: 1, working: false, hold: 2400 },
      { status: "brief", panel: true, entries: 1, working: false, hold: 1600 },
      { status: "research", panel: true, entries: 2, working: true, hold: 2000 },
      { status: "research", panel: true, entries: 3, working: true, hold: 2700 },
      { status: "draft", panel: true, entries: 4, working: true, hold: 2000 },
      { status: "draft", panel: true, entries: 5, working: true, hold: 2700 },
      { status: "edit", panel: true, entries: 6, working: false, hold: 2000 },
      { status: "edit", panel: true, entries: 7, working: false, hold: 2000 },
      { status: "published", panel: true, entries: 8, working: false, hold: 2600 },
      { status: "published", panel: false, entries: 8, working: false, hold: 3000 },
    ],
    still: 5,
  };

  const [northwind, globex, dana] = triage.grouped.quotes;
  const FEEDBACK: Workflow = {
    id: "triage",
    project: triage.project,
    statuses: named(TRIAGE_STATUSES, triage.statuses),
    cards: columns(TRIAGE_CARDS, triage.cards, app.ago),
    focus: { id: "ACM-151", priority: "none", due: due(6, 10), ...triage.focus },
    timeline: [
      { kind: "created", by: triage.autopilot, icon: <Zap className="size-3.5" />, handoff: TRIAGE },
      {
        kind: "comment",
        actor: TRIAGE,
        body: (
          <>
            {triage.grouped.comment}
            <ReportCluster
              reports={[
                { source: "Intercom", from: "Northwind", quote: northwind ?? "" },
                { source: "Intercom", from: "Globex", quote: globex ?? "" },
                { source: "Discord", from: "@dana", quote: dana ?? "" },
              ]}
            />
          </>
        ),
      },
      { kind: "status", actor: TRIAGE, from: "inbox", to: "repro", handoff: QA },
      {
        kind: "comment",
        actor: QA,
        body: (
          <>
            {triage.reproduced}
            <TimeoutChart />
          </>
        ),
      },
      { kind: "status", actor: QA, from: "repro", to: "fix", handoff: BACKEND },
      {
        kind: "comment",
        actor: BACKEND,
        body: (
          <>
            {triage.fixed.comment}
            <PullRequest number={326} title={triage.fixed.pr} added={96} removed={31} checks={checks(148)} />
          </>
        ),
      },
      { kind: "status", actor: BACKEND, from: "fix", to: "reply", handoff: TRIAGE },
      {
        kind: "comment",
        actor: TRIAGE,
        body: (
          <>
            {triage.replied}
            <Replies
              people={[
                { initials: "NR", name: "Northwind" },
                { initials: "GX", name: "Globex" },
                { initials: "DA", name: "@dana" },
              ]}
            />
          </>
        ),
      },
      { kind: "status", actor: TRIAGE, from: "reply", to: "resolved" },
    ],
    stages: [
      { status: "inbox", panel: false, entries: 1, working: true, hold: 2400 },
      { status: "inbox", panel: true, entries: 1, working: true, hold: 1500 },
      { status: "inbox", panel: true, entries: 2, working: true, priority: "urgent", hold: 2700 },
      { status: "repro", panel: true, entries: 3, working: true, priority: "urgent", hold: 2000 },
      { status: "repro", panel: true, entries: 4, working: true, priority: "urgent", hold: 2600 },
      { status: "fix", panel: true, entries: 5, working: true, priority: "urgent", hold: 2000 },
      { status: "fix", panel: true, entries: 6, working: true, priority: "urgent", hold: 2700 },
      { status: "reply", panel: true, entries: 7, working: true, priority: "urgent", hold: 2000 },
      { status: "reply", panel: true, entries: 8, working: true, priority: "urgent", hold: 2200 },
      { status: "resolved", panel: true, entries: 9, working: false, priority: "urgent", hold: 2600 },
      { status: "resolved", panel: false, entries: 9, working: false, priority: "urgent", hold: 3000 },
    ],
    still: 6,
  };

  return [
    { key: "ship", workflow: SHIP, icon: GitPullRequest },
    { key: "launch", workflow: LAUNCH, icon: Megaphone },
    { key: "triage", workflow: FEEDBACK, icon: Inbox },
  ] as const;
}

export function HeroDemo() {
  const [active, setActive] = useState(0);
  const [pinned, setPinned] = useState(false);
  const { locale, t } = useLocale();
  const workflows = useMemo(() => {
    const format = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", timeZone: "UTC" });
    return buildWorkflows(t.demo, (month, day) => format.format(Date.UTC(2026, month - 1, day)));
  }, [locale, t]);
  const { key, workflow } = workflows[active]!;
  const copy = t.home.hero;

  return (
    <>
      <div className="flex justify-center">
        <div
          role="group"
          aria-label={copy.workflowGroup}
          className="inline-flex items-center gap-0.5 rounded-full border border-site-line bg-site-sunken p-0.5"
        >
          {workflows.map(({ key: itemKey, workflow: item, icon: Icon }, index) => {
            const selected = index === active;
            const { label, short } = copy.workflows[itemKey];
            return (
              <button
                key={item.id}
                type="button"
                aria-pressed={selected}
                aria-label={label}
                onClick={() => {
                  setActive(index);
                  setPinned(true);
                }}
                className={cn(
                  "flex h-7 items-center gap-1.5 rounded-full px-2.5 text-[13px] transition-[color,background-color,box-shadow] duration-200",
                  focusRing,
                  selected
                    ? "bg-site-surface text-site-ink shadow-[0_1px_2px_rgb(0_0_0/0.06),0_0_0_1px_var(--site-line)]"
                    : "text-site-muted hover:text-site-ink",
                )}
              >
                <Icon className="size-3.5 shrink-0" aria-hidden />
                <span className="sm:hidden">{short}</span>
                <span className="hidden sm:inline">{label}</span>
              </button>
            );
          })}
        </div>
      </div>
      <div className="relative mt-3 aspect-square sm:aspect-[16/10]">
        <HeroBoard
          workflow={workflow}
          label={copy.workflows[key].description}
          onLoop={() => {
            if (!pinned) setActive((current) => (current + 1) % workflows.length);
          }}
        />
      </div>
    </>
  );
}
