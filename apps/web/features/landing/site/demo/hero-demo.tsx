"use client";

import { useState, type ReactNode } from "react";
import {
  CalendarClock,
  FilePen,
  FileSearch,
  FileText,
  GitMerge,
  GitPullRequest,
  Inbox,
  Megaphone,
  MessageSquare,
  Rows3,
  SquareTerminal,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { focusRing } from "../primitives";
import {
  ActivityRow,
  Comment,
  Entry,
  HeroBoard,
  RunCard,
  type RunStep,
  type Stage,
  type Workflow,
} from "./hero-board";
import { Avatar, BACKEND, CONTENT, FRONTEND, LEO, MIA, QA, SAM, TRIAGE } from "./mock-kit";

/*
 * The hero demo: three workflows played on the same board, with tabs above
 * it. Tabs advance on their own each time a story finishes, until a visitor
 * picks one — then that workflow loops.
 */

const SHIP_RUN: RunStep[] = [
  { icon: FileSearch, text: "Read 14 files in api/middleware" },
  {
    icon: FilePen,
    text: (
      <>
        Edited 3 files <span className="text-success">+184</span> <span className="text-destructive">−12</span>
      </>
    ),
  },
  {
    icon: SquareTerminal,
    text: (
      <>
        go test ./... <span className="text-muted-foreground">212 passed</span>
      </>
    ),
  },
];

const SHIP: Workflow = {
  id: "ship",
  label: "Ship a feature",
  description:
    "A Multica issue board: an issue is assigned to a coding agent, which works on it, opens a pull request, and moves it to done after review",
  focus: { id: "ACM-142", title: "Add rate limiting to the public API", priority: "high", updated: "just now" },
  columns: [
    {
      status: "todo",
      label: "Todo",
      total: 11,
      cards: [
        { id: "ACM-140", title: "Dark mode for billing settings", priority: "medium", assignee: MIA, updated: "1h ago" },
        { id: "ACM-138", title: "Upgrade the Postgres driver to v5", priority: "low", assignee: LEO, updated: "3h ago" },
        { id: "ACM-135", title: "Add SAML login for enterprise workspaces", priority: "medium", updated: "5h ago" },
        { id: "ACM-134", title: "Search results ignore archived projects", priority: "low", assignee: SAM, updated: "1d ago" },
      ],
    },
    {
      status: "in_progress",
      label: "In Progress",
      total: 4,
      cards: [
        { id: "ACM-136", title: "Move auth webhooks onto the job queue", priority: "high", assignee: BACKEND, working: true, updated: "2m ago" },
        { id: "ACM-133", title: "Fix the flaky checkout test on CI", priority: "urgent", assignee: QA, working: true, updated: "just now" },
        { id: "ACM-131", title: "Rewrite the onboarding emails", priority: "medium", assignee: SAM, updated: "40m ago" },
        { id: "ACM-130", title: "Usage chart for the admin dashboard", priority: "medium", assignee: FRONTEND, updated: "1h ago" },
      ],
    },
    {
      status: "in_review",
      label: "In Review",
      total: 3,
      cards: [
        { id: "ACM-129", title: "Paginate the audit log endpoint", priority: "medium", assignee: BACKEND, updated: "12m ago" },
        { id: "ACM-127", title: "Refresh the pricing page copy", priority: "low", assignee: MIA, updated: "2h ago" },
        { id: "ACM-126", title: "Cache the plan lookup on checkout", priority: "medium", assignee: QA, updated: "4h ago" },
      ],
    },
    {
      status: "done",
      label: "Done",
      total: 28,
      cards: [
        { id: "ACM-125", title: "CSV export for reports", priority: "medium", assignee: FRONTEND, updated: "1h ago" },
        { id: "ACM-122", title: "Retry failed Stripe webhooks", priority: "high", assignee: BACKEND, updated: "3h ago" },
        { id: "ACM-120", title: "Update the SSO setup guide", priority: "low", assignee: LEO, updated: "5h ago" },
        { id: "ACM-118", title: "Remove the legacy v1 export endpoint", priority: "medium", assignee: BACKEND, updated: "1d ago" },
        { id: "ACM-117", title: "Fix timezone drift in weekly digests", priority: "high", assignee: QA, updated: "1d ago" },
      ],
    },
  ],
  stages: [
    { column: "todo", working: false, panel: false, entries: 0, hold: 1800 },
    { column: "todo", assignee: BACKEND, working: false, panel: false, entries: 0, hold: 900 },
    { column: "in_progress", assignee: BACKEND, working: true, panel: true, entries: 1, hold: 1100 },
    { column: "in_progress", assignee: BACKEND, working: true, panel: true, entries: 2, hold: 1000 },
    { column: "in_progress", assignee: BACKEND, working: true, panel: true, entries: 3, hold: 1000 },
    { column: "in_progress", assignee: BACKEND, working: true, panel: true, entries: 4, hold: 1300 },
    { column: "in_review", assignee: BACKEND, working: false, panel: true, entries: 5, hold: 1700 },
    { column: "in_review", assignee: BACKEND, working: false, panel: true, entries: 6, hold: 1300 },
    { column: "done", assignee: BACKEND, working: false, panel: true, entries: 7, hold: 1500 },
    { column: "done", assignee: BACKEND, working: false, panel: false, entries: 7, hold: 2200 },
  ],
  still: 7,
  Activity: ShipActivity,
};

function ShipActivity({ stage }: { stage: Stage }) {
  return (
    <>
      {stage.entries >= 1 ? (
        <Entry>
          <ActivityRow icon={<Avatar actor={BACKEND} size={16} />}>
            <span className="font-medium text-foreground">Backend Agent</span> picked this up
          </ActivityRow>
        </Entry>
      ) : null}
      {stage.entries >= 2 ? (
        <Entry>
          <RunCard actor={BACKEND} runtime="Claude Code" steps={SHIP_RUN} at={2} entries={stage.entries} duration="3m 48s" />
        </Entry>
      ) : null}
      {stage.entries >= 5 ? (
        <Entry>
          <Comment actor={BACKEND}>
            <p>Added a token bucket per API key: 100 requests a minute by default, 429 with Retry-After past that.</p>
            <Attachment icon={GitPullRequest} title="#318 Rate limit the public API">
              <span className="text-success tabular-nums">+184</span>
              <span className="text-destructive tabular-nums">−12</span>
            </Attachment>
          </Comment>
        </Entry>
      ) : null}
      {stage.entries >= 6 ? (
        <Entry>
          <Comment actor={MIA}>
            <p>Reviewed, nice tests. Approved.</p>
          </Comment>
        </Entry>
      ) : null}
      {stage.entries >= 7 ? (
        <Entry>
          <ActivityRow icon={<GitMerge className="size-3.5 text-muted-foreground" />}>
            <span className="font-medium text-foreground">Mia Chen</span> merged #318
          </ActivityRow>
        </Entry>
      ) : null}
    </>
  );
}

const LAUNCH_RUN: RunStep[] = [
  { icon: FileSearch, text: "Read the Autopilots spec and 6 changelogs" },
  {
    icon: FilePen,
    text: (
      <>
        Drafted autopilots-launch.md <span className="text-muted-foreground">1,180 words</span>
      </>
    ),
  },
  { icon: Megaphone, text: "Wrote 3 social posts and a newsletter blurb" },
];

const LAUNCH: Workflow = {
  id: "launch",
  label: "Launch content",
  description:
    "A Multica issue board for marketing: a content agent drafts a launch post and social copy, a teammate reviews it, and the post is scheduled",
  focus: { id: "ACM-212", title: "Launch post for Autopilots", priority: "high", updated: "just now" },
  columns: [
    {
      status: "todo",
      label: "Todo",
      total: 9,
      cards: [
        { id: "ACM-210", title: "Customer story: Northwind Robotics", priority: "medium", assignee: MIA, updated: "1h ago" },
        { id: "ACM-208", title: "Webinar invite email for June", priority: "low", updated: "3h ago" },
        { id: "ACM-207", title: "Comparison page: Multica vs. spreadsheets", priority: "medium", assignee: CONTENT, updated: "5h ago" },
        { id: "ACM-205", title: "Refresh the pricing page FAQ", priority: "low", assignee: SAM, updated: "1d ago" },
      ],
    },
    {
      status: "in_progress",
      label: "In Progress",
      total: 4,
      cards: [
        { id: "ACM-204", title: "Changelog for v0.9", priority: "high", assignee: CONTENT, working: true, updated: "2m ago" },
        { id: "ACM-203", title: "SEO audit of the docs site", priority: "medium", assignee: QA, working: true, updated: "just now" },
        { id: "ACM-201", title: "Product Hunt launch kit", priority: "high", assignee: SAM, updated: "30m ago" },
        { id: "ACM-199", title: "Translate the homepage into Japanese", priority: "medium", assignee: CONTENT, updated: "2h ago" },
      ],
    },
    {
      status: "in_review",
      label: "In Review",
      total: 3,
      cards: [
        { id: "ACM-198", title: "Newsletter: May product roundup", priority: "medium", assignee: CONTENT, updated: "20m ago" },
        { id: "ACM-196", title: "Case study: Acme Robotics", priority: "medium", assignee: MIA, updated: "3h ago" },
        { id: "ACM-195", title: "Launch thread for the desktop app", priority: "low", assignee: CONTENT, updated: "5h ago" },
      ],
    },
    {
      status: "done",
      label: "Done",
      total: 36,
      cards: [
        { id: "ACM-193", title: "Autopilots one-pager for sales", priority: "medium", assignee: CONTENT, updated: "1h ago" },
        { id: "ACM-190", title: "Q2 messaging framework", priority: "high", assignee: SAM, updated: "4h ago" },
        { id: "ACM-188", title: "Script for the getting started video", priority: "low", assignee: LEO, updated: "6h ago" },
        { id: "ACM-187", title: "Social cards for the v0.8 release", priority: "medium", assignee: CONTENT, updated: "1d ago" },
        { id: "ACM-185", title: "Rewrite the onboarding emails", priority: "medium", assignee: SAM, updated: "1d ago" },
      ],
    },
  ],
  stages: [
    { column: "todo", working: false, panel: false, entries: 0, hold: 1800 },
    { column: "todo", assignee: CONTENT, working: false, panel: false, entries: 0, hold: 900 },
    { column: "in_progress", assignee: CONTENT, working: true, panel: true, entries: 1, hold: 1100 },
    { column: "in_progress", assignee: CONTENT, working: true, panel: true, entries: 2, hold: 1000 },
    { column: "in_progress", assignee: CONTENT, working: true, panel: true, entries: 3, hold: 1000 },
    { column: "in_progress", assignee: CONTENT, working: true, panel: true, entries: 4, hold: 1300 },
    { column: "in_review", assignee: CONTENT, working: false, panel: true, entries: 5, hold: 1700 },
    { column: "in_review", assignee: CONTENT, working: false, panel: true, entries: 6, hold: 1300 },
    { column: "done", assignee: CONTENT, working: false, panel: true, entries: 7, hold: 1500 },
    { column: "done", assignee: CONTENT, working: false, panel: false, entries: 7, hold: 2200 },
  ],
  still: 7,
  Activity: LaunchActivity,
};

function LaunchActivity({ stage }: { stage: Stage }) {
  return (
    <>
      {stage.entries >= 1 ? (
        <Entry>
          <ActivityRow icon={<Avatar actor={CONTENT} size={16} />}>
            <span className="font-medium text-foreground">Content Agent</span> picked this up
          </ActivityRow>
        </Entry>
      ) : null}
      {stage.entries >= 2 ? (
        <Entry>
          <RunCard actor={CONTENT} runtime="Hermes" steps={LAUNCH_RUN} at={2} entries={stage.entries} duration="6m 12s" />
        </Entry>
      ) : null}
      {stage.entries >= 5 ? (
        <Entry>
          <Comment actor={CONTENT}>
            <p>Draft is ready in the brand voice: the launch post, 3 social posts and a newsletter blurb.</p>
            <Attachment icon={FileText} title="autopilots-launch.md">
              <span className="text-muted-foreground tabular-nums">1,180 words</span>
            </Attachment>
          </Comment>
        </Entry>
      ) : null}
      {stage.entries >= 6 ? (
        <Entry>
          <Comment actor={SAM}>
            <p>Tightened the intro. Ship it Tuesday morning.</p>
          </Comment>
        </Entry>
      ) : null}
      {stage.entries >= 7 ? (
        <Entry>
          <ActivityRow icon={<CalendarClock className="size-3.5 text-muted-foreground" />}>
            <span className="font-medium text-foreground">Sam Rivera</span> scheduled it for Tue 9:00
          </ActivityRow>
        </Entry>
      ) : null}
    </>
  );
}

const TRIAGE_RUN: RunStep[] = [
  { icon: Inbox, text: "Scanned 46 new messages from Intercom and Discord" },
  { icon: Rows3, text: "Grouped 3 reports: exports over 10k rows" },
  {
    icon: SquareTerminal,
    text: (
      <>
        Reproduced on staging <span className="text-muted-foreground">timeout at 30s</span>
      </>
    ),
  },
];

const FEEDBACK: Workflow = {
  id: "triage",
  label: "Triage feedback",
  description:
    "A Multica issue board for support: an autopilot files an issue from user reports, a triage agent reproduces it, sets the priority, hands it to an engineering agent and replies to the customers",
  focus: { id: "ACM-151", title: "CSV export times out on large workspaces", priority: "none", updated: "just now" },
  columns: [
    {
      status: "backlog",
      label: "Backlog",
      total: 14,
      cards: [
        { id: "ACM-150", title: "Request: Slack alerts for mentions", priority: "low", updated: "2h ago" },
        { id: "ACM-148", title: "Board columns are hard to scroll on mobile", priority: "medium", updated: "4h ago" },
        { id: "ACM-147", title: "Typo in the workspace invite email", priority: "low", updated: "1d ago" },
        { id: "ACM-145", title: "Dark mode resets after a reload", priority: "low", assignee: FRONTEND, updated: "1d ago" },
      ],
    },
    {
      status: "todo",
      label: "Todo",
      total: 6,
      cards: [
        { id: "ACM-144", title: "Webhook retries show the wrong status", priority: "high", assignee: BACKEND, updated: "1h ago" },
        { id: "ACM-143", title: "Invite links expire after one hour", priority: "medium", updated: "3h ago" },
        { id: "ACM-141", title: "Search ignores archived projects", priority: "low", assignee: SAM, updated: "6h ago" },
      ],
    },
    {
      status: "in_progress",
      label: "In Progress",
      total: 4,
      cards: [
        { id: "ACM-139", title: "Billing page shows the old plan after upgrade", priority: "high", assignee: FRONTEND, working: true, updated: "4m ago" },
        { id: "ACM-137", title: "Fix the flaky checkout test on CI", priority: "urgent", assignee: QA, working: true, updated: "just now" },
        { id: "ACM-132", title: "Rate limit errors miss Retry-After", priority: "medium", assignee: LEO, updated: "1h ago" },
      ],
    },
    {
      status: "done",
      label: "Done",
      total: 52,
      cards: [
        { id: "ACM-128", title: "Login loop on Safari 17", priority: "urgent", assignee: FRONTEND, updated: "2h ago" },
        { id: "ACM-124", title: "Duplicate notifications on mention", priority: "high", assignee: BACKEND, updated: "5h ago" },
        { id: "ACM-121", title: "Broken link in the SSO guide", priority: "low", assignee: TRIAGE, updated: "8h ago" },
        { id: "ACM-119", title: "Export button hidden on small screens", priority: "medium", assignee: FRONTEND, updated: "1d ago" },
        { id: "ACM-116", title: "Weekly digest sent twice", priority: "high", assignee: BACKEND, updated: "1d ago" },
      ],
    },
  ],
  stages: [
    { column: "backlog", working: false, panel: false, entries: 0, hold: 1800 },
    { column: "backlog", assignee: TRIAGE, working: true, panel: true, entries: 1, hold: 1200 },
    { column: "backlog", assignee: TRIAGE, working: true, panel: true, entries: 2, hold: 1000 },
    { column: "backlog", assignee: TRIAGE, working: true, panel: true, entries: 3, hold: 1000 },
    { column: "backlog", assignee: TRIAGE, working: true, panel: true, entries: 4, hold: 1300 },
    { column: "todo", assignee: BACKEND, priority: "urgent", working: false, panel: true, entries: 5, hold: 1900 },
    { column: "in_progress", assignee: BACKEND, priority: "urgent", working: true, panel: true, entries: 6, hold: 1300 },
    { column: "in_progress", assignee: BACKEND, priority: "urgent", working: true, panel: true, entries: 7, hold: 1700 },
    { column: "in_progress", assignee: BACKEND, priority: "urgent", working: true, panel: false, entries: 7, hold: 2200 },
  ],
  still: 7,
  Activity: TriageActivity,
};

function TriageActivity({ stage }: { stage: Stage }) {
  return (
    <>
      {stage.entries >= 1 ? (
        <Entry>
          <ActivityRow icon={<Zap className="size-3.5 text-muted-foreground" />}>
            <span className="font-medium text-foreground">Feedback sweep</span> filed this from 3 reports
          </ActivityRow>
        </Entry>
      ) : null}
      {stage.entries >= 2 ? (
        <Entry>
          <RunCard actor={TRIAGE} runtime="OpenCode" steps={TRIAGE_RUN} at={2} entries={stage.entries} duration="1m 54s" />
        </Entry>
      ) : null}
      {stage.entries >= 5 ? (
        <Entry>
          <Comment actor={TRIAGE}>
            <p>
              Confirmed bug, not a duplicate. Exports run inside the request and time out above ~10k rows. Set to
              Urgent and handed to Backend Agent.
            </p>
          </Comment>
        </Entry>
      ) : null}
      {stage.entries >= 6 ? (
        <Entry>
          <ActivityRow icon={<Avatar actor={BACKEND} size={16} />}>
            <span className="font-medium text-foreground">Backend Agent</span> picked this up
          </ActivityRow>
        </Entry>
      ) : null}
      {stage.entries >= 7 ? (
        <Entry>
          <ActivityRow icon={<MessageSquare className="size-3.5 text-muted-foreground" />}>
            <span className="font-medium text-foreground">Triage Agent</span> replied to 3 customers
          </ActivityRow>
        </Entry>
      ) : null}
    </>
  );
}

function Attachment({ icon: Icon, title, children }: { icon: LucideIcon; title: string; children: ReactNode }) {
  return (
    <span className="mt-2 flex items-center gap-2 rounded-md border border-border px-2.5 py-1.5">
      <Icon className="size-3.5 text-muted-foreground" />
      <span className="min-w-0 flex-1 truncate font-medium">{title}</span>
      {children}
    </span>
  );
}

const WORKFLOWS = [
  { workflow: SHIP, icon: GitPullRequest, short: "Ship" },
  { workflow: LAUNCH, icon: Megaphone, short: "Launch" },
  { workflow: FEEDBACK, icon: Inbox, short: "Triage" },
] as const;

export function HeroDemo() {
  const [active, setActive] = useState(0);
  const [pinned, setPinned] = useState(false);
  const { workflow } = WORKFLOWS[active]!;

  return (
    <>
      <div
        role="group"
        aria-label="Workflow"
        className="inline-flex items-center gap-0.5 rounded-full border border-site-line bg-site-sunken p-0.5"
      >
        {WORKFLOWS.map(({ workflow: item, icon: Icon, short }, index) => {
          const selected = index === active;
          return (
            <button
              key={item.id}
              type="button"
              aria-pressed={selected}
              aria-label={item.label}
              onClick={() => {
                setActive(index);
                setPinned(true);
              }}
              className={cn(
                "flex h-8 items-center gap-1.5 rounded-full px-3 text-site-small transition-[color,background-color,box-shadow] duration-200",
                focusRing,
                selected
                  ? "bg-site-surface text-site-ink shadow-[0_1px_2px_rgb(0_0_0/0.06),0_0_0_1px_var(--site-line)]"
                  : "text-site-muted hover:text-site-ink",
              )}
            >
              <Icon className="size-3.5 shrink-0" aria-hidden />
              <span className="sm:hidden">{short}</span>
              <span className="hidden sm:inline">{item.label}</span>
            </button>
          );
        })}
      </div>
      <div className="relative mt-3 aspect-square sm:aspect-[16/10]">
        <HeroBoard
          workflow={workflow}
          onLoop={() => {
            if (!pinned) setActive((current) => (current + 1) % WORKFLOWS.length);
          }}
        />
      </div>
    </>
  );
}
