"use client";

import { useState } from "react";
import { GitPullRequest, Inbox, Megaphone, UserRound, Zap } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useLocale } from "../../i18n";
import { focusRing } from "../primitives";
import { BarList, Checklist, DocPreview, LoadChart, PullRequest, Replies, ReportCluster, TimeoutChart } from "./hero-blocks";
import { HeroBoard, type Workflow } from "./hero-board";
import { BACKEND, CONTENT, FRONTEND, LEAD, LEO, MIA, QA, RESEARCH, SAM, TRIAGE } from "./mock-kit";

/*
 * The hero demo: three workflows, each with its own custom statuses and
 * handoffs, played on the same board. The switcher advances on its own each
 * time a story finishes, until a visitor picks one — then that one loops.
 */

const SHIP: Workflow = {
  id: "ship",
  project: "API Platform",
  statuses: [
    { key: "spec", name: "Spec", category: "unstarted", icon: "circle", color: "#8b5cf6", handler: LEAD },
    { key: "build", name: "Build", category: "started", icon: "half", color: "#f97316", handler: BACKEND },
    { key: "review", name: "Code review", category: "started", icon: "three_quarters", color: "#3b82f6", handler: QA },
    { key: "approve", name: "Approve", category: "started", icon: "three_quarters", color: "#ec4899", handler: MIA },
    { key: "shipped", name: "Shipped", category: "done", icon: "check", color: "#22c55e" },
  ],
  cards: {
    spec: {
      total: 5,
      cards: [
        { id: "ACM-145", title: "SAML login for enterprise workspaces", priority: "medium", assignee: LEAD, updated: "1h ago" },
        { id: "ACM-144", title: "Usage alerts for billing admins", priority: "low", assignee: LEAD, updated: "3h ago" },
        { id: "ACM-143", title: "Per-workspace API keys with scopes", priority: "medium", assignee: LEAD, updated: "4h ago" },
        { id: "ACM-141", title: "Rotate webhook signing secrets", priority: "medium", assignee: LEAD, updated: "6h ago" },
        { id: "ACM-140", title: "Bulk invite members from a CSV", priority: "low", assignee: MIA, updated: "1d ago" },
      ],
    },
    build: {
      total: 4,
      cards: [
        { id: "ACM-136", title: "Move auth webhooks onto the job queue", priority: "high", assignee: BACKEND, working: true, updated: "2m ago" },
        { id: "ACM-138", title: "Upgrade the Postgres driver to v5", priority: "low", assignee: BACKEND, updated: "40m ago" },
        { id: "ACM-135", title: "Stream large exports instead of buffering", priority: "medium", assignee: BACKEND, updated: "1h ago" },
        { id: "ACM-134", title: "Settings page for API key scopes", priority: "medium", assignee: FRONTEND, updated: "2h ago" },
      ],
    },
    review: {
      total: 5,
      cards: [
        { id: "ACM-133", title: "Fix the flaky checkout test on CI", priority: "urgent", assignee: QA, working: true, updated: "just now" },
        { id: "ACM-131", title: "Paginate the audit log endpoint", priority: "medium", assignee: QA, updated: "1h ago" },
        { id: "ACM-127", title: "Rename the billing plan enum", priority: "low", assignee: QA, updated: "2h ago" },
        { id: "ACM-126", title: "Timezone bug in weekly reports", priority: "medium", assignee: QA, updated: "3h ago" },
        { id: "ACM-124", title: "Lazy-load the members table", priority: "low", assignee: QA, updated: "5h ago" },
      ],
    },
    approve: {
      total: 4,
      cards: [
        { id: "ACM-129", title: "Cache the plan lookup on checkout", priority: "medium", assignee: MIA, updated: "2h ago" },
        { id: "ACM-119", title: "Raise the default page size to 100", priority: "low", assignee: MIA, updated: "3h ago" },
        { id: "ACM-118", title: "New error codes for quota limits", priority: "medium", assignee: MIA, updated: "4h ago" },
        { id: "ACM-117", title: "Session timeout for admin roles", priority: "high", assignee: MIA, updated: "6h ago" },
      ],
    },
    shipped: {
      total: 6,
      cards: [
        { id: "ACM-125", title: "CSV export for reports", priority: "medium", assignee: MIA, updated: "1h ago" },
        { id: "ACM-122", title: "Retry failed Stripe webhooks", priority: "high", assignee: LEO, updated: "3h ago" },
        { id: "ACM-120", title: "Remove the legacy v1 export endpoint", priority: "medium", assignee: MIA, updated: "1d ago" },
        { id: "ACM-114", title: "Faster cold starts for the worker", priority: "medium", assignee: LEO, updated: "1d ago" },
        { id: "ACM-113", title: "Delivery logs for webhooks", priority: "high", assignee: MIA, updated: "2d ago" },
        { id: "ACM-112", title: "SSO errors that say what went wrong", priority: "medium", assignee: LEO, updated: "2d ago" },
      ],
    },
  },
  focus: {
    id: "ACM-142",
    title: "Add rate limiting to the public API",
    priority: "high",
    description:
      "Public endpoints have no limits, so one noisy key can slow everyone down. Add per-key limits with sane defaults and a clear 429.",
    label: "api",
    due: "Jun 12",
  },
  timeline: [
    { kind: "created", by: "Mia Chen", icon: <UserRound className="size-3.5" />, handoff: LEAD },
    {
      kind: "comment",
      actor: LEAD,
      body: (
        <>
          Here is the plan.
          <Checklist
            title="Build plan"
            items={["Token bucket per API key", "Shared counters in Redis", "429 with a Retry-After header"]}
          />
        </>
      ),
    },
    { kind: "status", actor: LEAD, from: "spec", to: "build", handoff: BACKEND },
    {
      kind: "comment",
      actor: BACKEND,
      body: (
        <>
          Limiter and tests are in.
          <PullRequest number={318} title="Rate limit the public API" added={184} removed={12} checks={["Build", "212 tests", "Lint"]} />
        </>
      ),
    },
    { kind: "status", actor: BACKEND, from: "build", to: "review", handoff: QA },
    {
      kind: "comment",
      actor: QA,
      body: (
        <>
          Load-tested on staging.
          <LoadChart />
        </>
      ),
    },
    { kind: "status", actor: QA, from: "review", to: "approve", handoff: MIA },
    { kind: "comment", actor: MIA, body: "Looks good. Merging." },
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
  project: "Autopilots launch",
  statuses: [
    { key: "brief", name: "Brief", category: "unstarted", icon: "circle", color: "#64748b", handler: SAM },
    { key: "research", name: "Research", category: "started", icon: "half", color: "#06b6d4", handler: RESEARCH },
    { key: "draft", name: "Draft", category: "started", icon: "half", color: "#8b5cf6", handler: CONTENT },
    { key: "edit", name: "Edit", category: "started", icon: "three_quarters", color: "#ec4899", handler: SAM },
    { key: "published", name: "Published", category: "done", icon: "check", color: "#22c55e" },
  ],
  cards: {
    brief: {
      total: 5,
      cards: [
        { id: "ACM-213", title: "Webinar invite email for June", priority: "low", assignee: SAM, updated: "2h ago" },
        { id: "ACM-211", title: "Comparison page: Multica vs. spreadsheets", priority: "medium", assignee: SAM, updated: "5h ago" },
        { id: "ACM-210", title: "Landing page for the Japan launch", priority: "medium", assignee: SAM, updated: "6h ago" },
        { id: "ACM-208", title: "Co-marketing kit for partners", priority: "low", assignee: MIA, updated: "1d ago" },
        { id: "ACM-207", title: "Onboarding emails for week one", priority: "medium", assignee: SAM, updated: "1d ago" },
      ],
    },
    research: {
      total: 4,
      cards: [
        { id: "ACM-209", title: "Customer story: Northwind Robotics", priority: "medium", assignee: RESEARCH, working: true, updated: "3m ago" },
        { id: "ACM-203", title: "Survey: why teams churn in month one", priority: "high", assignee: RESEARCH, updated: "1h ago" },
        { id: "ACM-202", title: "Pricing pages across the category", priority: "medium", assignee: RESEARCH, updated: "2h ago" },
        { id: "ACM-201", title: "Top search terms in the docs", priority: "low", assignee: RESEARCH, updated: "4h ago" },
      ],
    },
    draft: {
      total: 5,
      cards: [
        { id: "ACM-204", title: "Changelog for v0.9", priority: "high", assignee: CONTENT, working: true, updated: "just now" },
        { id: "ACM-199", title: "Translate the homepage into Japanese", priority: "medium", assignee: CONTENT, updated: "1h ago" },
        { id: "ACM-195", title: "Release notes for the desktop app", priority: "medium", assignee: CONTENT, updated: "2h ago" },
        { id: "ACM-194", title: "Blog: how we triage with agents", priority: "medium", assignee: CONTENT, updated: "3h ago" },
        { id: "ACM-192", title: "Help center: workflow statuses", priority: "low", assignee: CONTENT, updated: "5h ago" },
      ],
    },
    edit: {
      total: 4,
      cards: [
        { id: "ACM-198", title: "Newsletter: May product roundup", priority: "medium", assignee: SAM, updated: "20m ago" },
        { id: "ACM-196", title: "Case study: Acme Robotics", priority: "medium", assignee: SAM, updated: "3h ago" },
        { id: "ACM-188", title: "Email: invite your team", priority: "low", assignee: SAM, updated: "4h ago" },
        { id: "ACM-186", title: "Docs: API rate limits", priority: "medium", assignee: SAM, updated: "5h ago" },
      ],
    },
    published: {
      total: 6,
      cards: [
        { id: "ACM-193", title: "Autopilots one-pager for sales", priority: "medium", assignee: SAM, updated: "1h ago" },
        { id: "ACM-190", title: "Q2 messaging framework", priority: "high", assignee: MIA, updated: "4h ago" },
        { id: "ACM-187", title: "Social cards for the v0.8 release", priority: "medium", assignee: SAM, updated: "1d ago" },
        { id: "ACM-182", title: "Changelog for v0.8", priority: "medium", assignee: CONTENT, updated: "2d ago" },
        { id: "ACM-181", title: "Customer story: Contoso Labs", priority: "medium", assignee: SAM, updated: "3d ago" },
        { id: "ACM-180", title: "Docs: inviting agents to a project", priority: "low", assignee: CONTENT, updated: "4d ago" },
      ],
    },
  },
  focus: {
    id: "ACM-212",
    title: "Launch post for Autopilots",
    priority: "high",
    description:
      "Announce Autopilots: what it is, who it is for, and three real examples. Blog post first, then social and newsletter copy.",
    label: "launch",
    due: "Jun 18",
  },
  timeline: [
    { kind: "created", by: "Sam Rivera", icon: <UserRound className="size-3.5" /> },
    { kind: "status", actor: SAM, from: "brief", to: "research", handoff: RESEARCH },
    {
      kind: "comment",
      actor: RESEARCH,
      body: (
        <>
          What customers ask for most.
          <BarList
            title="Use cases mentioned"
            aside="40 calls"
            bars={[
              { label: "Nightly triage", value: 18 },
              { label: "Weekly digests", value: 12 },
              { label: "Dependency bumps", value: 9 },
            ]}
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
          Draft is ready, with social posts and a newsletter blurb.
          <DocPreview title="autopilots-launch.md" meta="1,180 words" />
        </>
      ),
    },
    { kind: "status", actor: CONTENT, from: "draft", to: "edit", handoff: SAM },
    { kind: "comment", actor: SAM, body: "Tightened the intro. Scheduled for Tuesday 9:00." },
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

const FEEDBACK: Workflow = {
  id: "triage",
  project: "Customer feedback",
  statuses: [
    { key: "inbox", name: "Inbox", category: "unstarted", icon: "dotted", color: "#64748b", handler: TRIAGE },
    { key: "repro", name: "Reproduce", category: "started", icon: "half", color: "#f97316", handler: QA },
    { key: "fix", name: "Fix", category: "started", icon: "half", color: "#3b82f6", handler: BACKEND },
    { key: "reply", name: "Reply", category: "started", icon: "three_quarters", color: "#8b5cf6", handler: TRIAGE },
    { key: "resolved", name: "Resolved", category: "done", icon: "check", color: "#22c55e" },
  ],
  cards: {
    inbox: {
      total: 5,
      cards: [
        { id: "ACM-150", title: "Request: Slack alerts for mentions", priority: "low", assignee: TRIAGE, updated: "2h ago" },
        { id: "ACM-148", title: "Board columns are hard to scroll on mobile", priority: "medium", assignee: TRIAGE, updated: "4h ago" },
        { id: "ACM-149", title: "Request: dark mode for email digests", priority: "low", assignee: TRIAGE, updated: "5h ago" },
        { id: "ACM-147", title: "Search misses issues with emoji titles", priority: "medium", assignee: TRIAGE, updated: "6h ago" },
        { id: "ACM-145", title: "Request: export a board as PDF", priority: "low", assignee: TRIAGE, updated: "8h ago" },
      ],
    },
    repro: {
      total: 4,
      cards: [
        { id: "ACM-146", title: "Invite links expire after one hour", priority: "medium", assignee: QA, working: true, updated: "5m ago" },
        { id: "ACM-142", title: "Comment drafts lost when switching tabs", priority: "medium", assignee: QA, updated: "1h ago" },
        { id: "ACM-141", title: "Desktop badge count stuck at 1", priority: "low", assignee: QA, updated: "3h ago" },
        { id: "ACM-140", title: "Due dates shift by a day in UTC−8", priority: "high", assignee: QA, updated: "4h ago" },
      ],
    },
    fix: {
      total: 5,
      cards: [
        { id: "ACM-139", title: "Billing page shows the old plan after upgrade", priority: "high", assignee: FRONTEND, working: true, updated: "4m ago" },
        { id: "ACM-137", title: "Webhook retries show the wrong status", priority: "high", assignee: BACKEND, updated: "1h ago" },
        { id: "ACM-135", title: "Attachments over 25 MB fail silently", priority: "high", assignee: BACKEND, updated: "2h ago" },
        { id: "ACM-134", title: "Sidebar flickers when collapsing", priority: "low", assignee: FRONTEND, updated: "3h ago" },
        { id: "ACM-133", title: "Rate limit errors shown as a 500", priority: "medium", assignee: BACKEND, updated: "5h ago" },
      ],
    },
    reply: {
      total: 4,
      cards: [
        { id: "ACM-132", title: "Dark mode resets after a reload", priority: "low", assignee: TRIAGE, updated: "30m ago" },
        { id: "ACM-129", title: "Clarify how seats are billed", priority: "low", assignee: TRIAGE, updated: "1h ago" },
        { id: "ACM-127", title: "Confirm the SSO fix with the reporter", priority: "medium", assignee: TRIAGE, updated: "2h ago" },
        { id: "ACM-126", title: "Workaround for the Safari upload bug", priority: "medium", assignee: TRIAGE, updated: "4h ago" },
      ],
    },
    resolved: {
      total: 6,
      cards: [
        { id: "ACM-128", title: "Login loop on Safari 17", priority: "urgent", assignee: TRIAGE, updated: "2h ago" },
        { id: "ACM-124", title: "Duplicate notifications on mention", priority: "high", assignee: TRIAGE, updated: "5h ago" },
        { id: "ACM-121", title: "Broken link in the SSO guide", priority: "low", assignee: TRIAGE, updated: "8h ago" },
        { id: "ACM-122", title: "Avatar uploads rotate sideways", priority: "low", assignee: TRIAGE, updated: "1d ago" },
        { id: "ACM-120", title: "Search ignores the archived filter", priority: "medium", assignee: TRIAGE, updated: "1d ago" },
        { id: "ACM-119", title: "Shortcut conflicts on Windows", priority: "low", assignee: TRIAGE, updated: "2d ago" },
      ],
    },
  },
  focus: {
    id: "ACM-151",
    title: "CSV export times out on large workspaces",
    priority: "none",
    description: "Filed by the Feedback sweep autopilot from 3 reports: 2 in Intercom, 1 in Discord.",
    label: "bug",
    due: "Jun 10",
  },
  timeline: [
    { kind: "created", by: "Feedback sweep", icon: <Zap className="size-3.5" />, handoff: TRIAGE },
    {
      kind: "comment",
      actor: TRIAGE,
      body: (
        <>
          Not a duplicate. Set to Urgent.
          <ReportCluster
            reports={[
              { source: "Intercom", from: "Northwind", quote: "Export spins forever on 14k rows" },
              { source: "Intercom", from: "Globex", quote: "CSV download just fails" },
              { source: "Discord", from: "@dana", quote: "anyone else get export timeouts?" },
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
          Reproduced: the export runs inside the request.
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
          Exports now run as a background job.
          <PullRequest number={326} title="Stream large CSV exports" added={96} removed={31} checks={["Build", "148 tests", "Lint"]} />
        </>
      ),
    },
    { kind: "status", actor: BACKEND, from: "fix", to: "reply", handoff: TRIAGE },
    {
      kind: "comment",
      actor: TRIAGE,
      body: (
        <>
          Told everyone who reported it.
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

const WORKFLOWS = [
  { key: "ship", workflow: SHIP, icon: GitPullRequest },
  { key: "launch", workflow: LAUNCH, icon: Megaphone },
  { key: "triage", workflow: FEEDBACK, icon: Inbox },
] as const;

export function HeroDemo() {
  const [active, setActive] = useState(0);
  const [pinned, setPinned] = useState(false);
  const { key, workflow } = WORKFLOWS[active]!;
  const copy = useLocale().t.home.hero;

  return (
    <>
      <div className="flex justify-center">
        <div
          role="group"
          aria-label={copy.workflowGroup}
          className="inline-flex items-center gap-0.5 rounded-full border border-site-line bg-site-sunken p-0.5"
        >
          {WORKFLOWS.map(({ key: itemKey, workflow: item, icon: Icon }, index) => {
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
            if (!pinned) setActive((current) => (current + 1) % WORKFLOWS.length);
          }}
        />
      </div>
    </>
  );
}
