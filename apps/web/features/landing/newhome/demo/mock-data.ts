// Mock data for the interactive product demo embedded in the landing hero.
// All ids/shapes follow the real backend contracts so the real product
// components render unchanged. Issues are a MUTABLE module array so demo
// interactions (drag to change status) persist across refetches.

import type { Agent, AgentTask } from "@multica/core/types/agent";
import type { TaskMessagePayload } from "@multica/core/types/events";
import type { TimelineEntry } from "@multica/core/types/activity";
import type { GitHubPullRequest } from "@multica/core/types/github";
import type { Issue, IssueStatus, IssuePriority } from "@multica/core/types/issue";
import type { MemberWithUser, Workspace } from "@multica/core/types/workspace";

const NOW = "2026-06-01T09:00:00Z";

export const WORKSPACE = {
  id: "ws-demo",
  name: "Acme",
  slug: "demo",
  created_at: NOW,
  updated_at: NOW,
} as unknown as Workspace;

export const MEMBERS: MemberWithUser[] = [
  {
    id: "m-alex",
    workspace_id: "ws-demo",
    user_id: "u-alex",
    role: "admin",
    created_at: NOW,
    name: "Alex Rivera",
    email: "alex@acme.dev",
    avatar_url: null,
  },
  {
    id: "m-sam",
    workspace_id: "ws-demo",
    user_id: "u-sam",
    role: "member",
    created_at: NOW,
    name: "Sam Chen",
    email: "sam@acme.dev",
    avatar_url: null,
  },
] as unknown as MemberWithUser[];

// Each agent carries its provider mark as a data-URI avatar so the real
// ActorAvatar renders the right icon on cards / detail / the working chip.
const svgUri = (svg: string) => `data:image/svg+xml,${encodeURIComponent(svg)}`;

const CLAUDE_SVG = `<svg xmlns='http://www.w3.org/2000/svg' viewBox='-1 -1 18 18'><rect x='-1' y='-1' width='18' height='18' fill='#F4EBE5'/><path fill='#D97757' d='m3.127 10.604 3.135-1.76.053-.153-.053-.085H6.11l-.525-.032-1.791-.048-1.554-.065-1.505-.08-.38-.081L0 7.832l.036-.234.32-.214.455.04 1.009.069 1.513.105 1.097.064 1.626.17h.259l.036-.105-.089-.065-.068-.064-1.566-1.062-1.695-1.121-.887-.646-.48-.327-.243-.306-.104-.67.435-.48.585.04.15.04.593.456 1.267.981 1.654 1.218.242.202.097-.068.012-.049-.109-.181-.9-1.626-.96-1.655-.428-.686-.113-.411a2 2 0 0 1-.068-.484l.496-.674L4.446 0l.662.089.279.242.411.94.666 1.48 1.033 2.014.302.597.162.553.06.17h.105v-.097l.085-1.134.157-1.392.154-1.792.052-.504.25-.605.497-.327.387.186.319.456-.045.294-.19 1.23-.37 1.93-.243 1.29h.142l.161-.16.654-.868 1.097-1.372.484-.545.565-.601.363-.287h.686l.505.751-.226.775-.707.895-.585.759-.839 1.13-.524.904.048.072.125-.012 1.897-.403 1.024-.186 1.223-.21.553.258.06.263-.218.536-1.307.323-1.533.307-2.284.54-.028.02.032.04 1.029.098.44.024h1.077l2.005.15.525.346.315.424-.053.323-.807.411-3.631-.863-.872-.218h-.12v.073l.726.71 1.331 1.202 1.667 1.55.084.383-.214.302-.226-.032-1.464-1.101-.565-.497-1.28-1.077h-.084v.113l.295.432 1.557 2.34.08.718-.112.234-.404.141-.444-.08-.911-1.28-.94-1.44-.759-1.291-.093.053-.448 4.821-.21.246-.484.186-.403-.307-.214-.496.214-.98.258-1.28.21-1.016.19-1.263.112-.42-.008-.028-.092.012-.953 1.307-1.448 1.957-1.146 1.227-.274.109-.477-.247.045-.44.266-.39 1.586-2.018.956-1.25.617-.723-.004-.105h-.036l-4.212 2.736-.75.096-.324-.302.04-.496.154-.162 1.267-.871z'/></svg>`;

const CODEX_SVG = `<svg xmlns='http://www.w3.org/2000/svg' viewBox='-1 -1 18 18'><rect x='-1' y='-1' width='18' height='18' fill='#111827'/><path fill='#ffffff' d='M14.949 6.547a3.94 3.94 0 0 0-.348-3.273 4.11 4.11 0 0 0-4.4-1.934A4.1 4.1 0 0 0 8.423.2 4.15 4.15 0 0 0 6.305.086a4.1 4.1 0 0 0-1.891.948 4.04 4.04 0 0 0-1.158 1.753 4.1 4.1 0 0 0-1.563.679A4 4 0 0 0 .554 4.72a3.99 3.99 0 0 0 .502 4.731 3.94 3.94 0 0 0 .346 3.274 4.11 4.11 0 0 0 4.402 1.933c.382.425.852.764 1.377.995.526.231 1.095.35 1.67.346 1.78.002 3.358-1.132 3.901-2.804a4.1 4.1 0 0 0 1.563-.68 4 4 0 0 0 1.14-1.253 3.99 3.99 0 0 0-.506-4.716m-6.097 8.406a3.05 3.05 0 0 1-1.945-.694l.096-.054 3.23-1.838a.53.53 0 0 0 .265-.455v-4.49l1.366.778q.02.011.025.035v3.722c-.003 1.653-1.361 2.992-3.037 2.996m-6.53-2.75a2.95 2.95 0 0 1-.36-2.01l.095.057L5.29 12.09a.53.53 0 0 0 .527 0l3.949-2.246v1.555a.05.05 0 0 1-.022.041L6.473 13.3c-1.454.826-3.311.335-4.15-1.098m-.85-6.94A3.02 3.02 0 0 1 3.07 3.949v3.785a.51.51 0 0 0 .262.451l3.93 2.237-1.366.779a.05.05 0 0 1-.048 0l-4.83-2.786A4.504 4.504 0 0 1 2.34 7.872v.024Zm11.216 2.571L8.747 5.576l1.362-.776a.05.05 0 0 1 .048 0l3.265 1.86a3 3 0 0 1 1.173 1.207 2.96 2.96 0 0 1-.27 3.2 3.05 3.05 0 0 1-1.36.997V8.279a.52.52 0 0 0-.276-.445m1.36-2.015-.097-.057-3.226-1.855a.53.53 0 0 0-.53 0L6.249 6.153V4.598a.04.04 0 0 1 .019-.04L9.533 2.7a3.07 3.07 0 0 1 3.257.139c.474.325.843.778 1.066 1.303.223.526.289 1.103.191 1.664zM5.503 8.575 4.139 7.8a.05.05 0 0 1-.026-.037V4.049c0-.57.166-1.127.476-1.607s.752-.864 1.275-1.105a3.08 3.08 0 0 1 3.234.41l-.096.054-3.23 1.838a.53.53 0 0 0-.265.455zm.742-1.577 1.758-1 1.762 1v2l-1.755 1-1.762-1z'/></svg>`;

const GEMINI_SVG = `<svg xmlns='http://www.w3.org/2000/svg' viewBox='-3 -3 30 30'><rect x='-3' y='-3' width='30' height='30' fill='#EFEBF6'/><path fill='#8E75B2' d='M11.04 19.32Q12 21.51 12 24q0-2.49.93-4.68.96-2.19 2.58-3.81t3.81-2.55Q21.51 12 24 12q-2.49 0-4.68-.93a12.3 12.3 0 0 1-3.81-2.58 12.3 12.3 0 0 1-2.58-3.81Q12 2.49 12 0q0 2.49-.96 4.68-.93 2.19-2.55 3.81a12.3 12.3 0 0 1-3.81 2.58Q2.49 12 0 12q2.49 0 4.68.96 2.19.93 3.81 2.55t2.55 3.81'/></svg>`;

const KIMI_SVG = `<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'><rect width='24' height='24' fill='#1F1147'/><path fill='#ffffff' d='M7.2 6h2.4v5.1l4.3-5.1h2.9l-4.4 5.1L17 18h-2.9l-3.2-5.2-1.3 1.5V18H7.2V6z'/></svg>`;

export const AGENTS: Agent[] = [
  { id: "a-claude", name: "Claude Code", avatar_url: svgUri(CLAUDE_SVG) },
  { id: "a-codex", name: "Codex", avatar_url: svgUri(CODEX_SVG) },
  { id: "a-gemini", name: "Gemini CLI", avatar_url: svgUri(GEMINI_SVG) },
  { id: "a-kimi", name: "Kimi", avatar_url: svgUri(KIMI_SVG) },
].map(
  (a) =>
    ({
      ...a,
      workspace_id: "ws-demo",
      description: "Autonomous coding agent — assign it an issue and it runs.",
      instructions: "",
      // Fields read by the agent hover-card (AgentProfileCard). Without these
      // (esp. `skills`) hovering an agent throws.
      skills: [],
      runtime_id: "rt-demo",
      runtime_mode: "cloud",
      runtime_config: {},
      custom_args: [],
      owner_id: "u-alex",
      owner_type: "member",
      visibility: "workspace",
      archived_at: null,
      created_at: NOW,
      updated_at: NOW,
    }) as unknown as Agent,
);

type Seed = {
  n: number;
  title: string;
  status: IssueStatus;
  priority: IssuePriority;
  at: "member" | "agent";
  aid: string;
  due?: string;
};

const SEEDS: Seed[] = [
  { n: 160, title: "Add 2FA / TOTP support", status: "backlog", priority: "medium", at: "member", aid: "u-sam" },
  { n: 162, title: "Investigate p95 latency on /search", status: "backlog", priority: "high", at: "agent", aid: "a-codex" },
  { n: 165, title: "Spike: vector search over issues", status: "backlog", priority: "low", at: "agent", aid: "a-gemini" },
  { n: 168, title: "Audit npm dependencies for CVEs", status: "backlog", priority: "medium", at: "member", aid: "u-alex" },
  { n: 142, title: "Design pricing page v2", status: "todo", priority: "high", at: "member", aid: "u-alex" },
  { n: 151, title: "Add SSO (SAML) to enterprise plan", status: "todo", priority: "low", at: "member", aid: "u-sam" },
  { n: 156, title: "Refactor billing webhooks handler", status: "todo", priority: "medium", at: "agent", aid: "a-kimi" },
  { n: 129, title: "Implement OAuth login flow", status: "in_progress", priority: "high", at: "agent", aid: "a-claude", due: "2026-06-08" },
  { n: 133, title: "Migrate analytics events to new schema", status: "in_progress", priority: "medium", at: "agent", aid: "a-gemini" },
  { n: 138, title: "Fix flaky checkout E2E test", status: "in_progress", priority: "medium", at: "agent", aid: "a-codex" },
  { n: 147, title: "Polish onboarding empty states", status: "in_progress", priority: "medium", at: "member", aid: "u-alex" },
  { n: 124, title: "Weekly dependency upgrade sweep", status: "in_review", priority: "low", at: "agent", aid: "a-claude" },
  { n: 119, title: "Write API docs for webhooks", status: "in_review", priority: "medium", at: "member", aid: "u-sam" },
  { n: 112, title: "Triage inbound bug reports", status: "done", priority: "low", at: "agent", aid: "a-codex" },
  { n: 108, title: "Ship dark-mode polish", status: "done", priority: "medium", at: "member", aid: "u-alex" },
  { n: 103, title: "Nightly DB backup health check", status: "done", priority: "low", at: "agent", aid: "a-gemini" },
  { n: 170, title: "Enable SSO on the staging environment", status: "blocked", priority: "high", at: "member", aid: "u-sam" },
  { n: 172, title: "Migrate CI to the new build runners", status: "blocked", priority: "medium", at: "agent", aid: "a-gemini" },
];

function makeIssue(seed: Seed, index: number): Issue {
  return {
    id: `issue-${seed.n}`,
    workspace_id: "ws-demo",
    number: seed.n,
    identifier: `MUL-${seed.n}`,
    title: seed.title,
    description:
      seed.at === "agent"
        ? `Assigned to an agent. ${seed.title}. The agent picks this up, runs it, and reports back here.`
        : `${seed.title}.`,
    status: seed.status,
    priority: seed.priority,
    assignee_type: seed.at,
    assignee_id: seed.aid,
    creator_type: "member",
    creator_id: "u-alex",
    parent_issue_id: null,
    project_id: null,
    position: index,
    start_date: null,
    due_date: seed.due ?? null,
    metadata: {},
    created_at: NOW,
    updated_at: NOW,
  };
}

// Mutable so updateIssue (drag) persists across refetches.
export const ISSUES: Issue[] = SEEDS.map(makeIssue);

// Agents currently working — drives the "N working" header chip + avatar
// stack and the agents-working filter. Each points at an in-progress,
// agent-assigned issue above.
const WORKING: { agent: string; issue: string }[] = [
  { agent: "a-claude", issue: "issue-129" },
  { agent: "a-gemini", issue: "issue-133" },
  { agent: "a-codex", issue: "issue-138" },
];

// A few minutes ago, so the "agent is working" timers read naturally (e.g.
// "6m") and tick up live, instead of an absurd elapsed value from a fixed date.
const startedAt = (minsAgo: number) =>
  new Date(Date.now() - minsAgo * 60_000).toISOString();

export const RUNNING_TASKS: AgentTask[] = WORKING.map(
  ({ agent, issue }, i) =>
    ({
      id: `task-${i}`,
      agent_id: agent,
      runtime_id: "rt-demo",
      issue_id: issue,
      status: "running",
      priority: 0,
      dispatched_at: startedAt(4 + i * 3 + 1),
      started_at: startedAt(4 + i * 3),
      completed_at: null,
      result: null,
      error: null,
      created_at: NOW,
      updated_at: NOW,
    }) as unknown as AgentTask,
);

// Create-issue flow: build a fresh issue from the dialog input, drop it at the
// top of its column, and return it so the board shows the new card.
let nextNumber = 200;
export function createMockIssue(
  input: Partial<Issue> & { title: string },
): Issue {
  const n = nextNumber++;
  const now = new Date().toISOString();
  const issue: Issue = {
    id: `issue-new-${n}`,
    workspace_id: "ws-demo",
    number: n,
    identifier: `MUL-${n}`,
    title: input.title || "Untitled issue",
    description: input.description ?? null,
    status: input.status ?? "todo",
    priority: input.priority ?? "none",
    assignee_type: input.assignee_type ?? null,
    assignee_id: input.assignee_id ?? null,
    creator_type: "member",
    creator_id: "u-alex",
    parent_issue_id: input.parent_issue_id ?? null,
    project_id: input.project_id ?? null,
    position: -1,
    start_date: input.start_date ?? null,
    due_date: input.due_date ?? null,
    metadata: {},
    created_at: now,
    updated_at: now,
  };
  ISSUES.unshift(issue);
  return issue;
}

export function patchIssue(id: string, patch: Partial<Issue>): Issue | undefined {
  const i = ISSUES.findIndex((x) => x.id === id);
  if (i === -1) return undefined;
  ISSUES[i] = { ...ISSUES[i]!, ...patch, updated_at: NOW };
  return ISSUES[i];
}

// ---------------------------------------------------------------------------
// Richer issue detail: comments/discussion, linked PRs, execution history.
// ---------------------------------------------------------------------------

const mins = (m: number) => new Date(Date.now() - m * 60_000).toISOString();

function comment(
  id: string,
  actorType: "member" | "agent",
  actorId: string,
  content: string,
  minsAgo: number,
  parentId: string | null = null,
): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: actorType,
    actor_id: actorId,
    content,
    comment_type: "comment",
    parent_id: parentId,
    reactions: [],
    attachments: [],
    created_at: mins(minsAgo),
    updated_at: mins(minsAgo),
    resolved_at: null,
  } as unknown as TimelineEntry;
}

// Comment / activity threads, keyed by issue id. Issues without an entry just
// render an empty Activity feed (the real component handles that).
export const TIMELINE: Record<string, TimelineEntry[]> = {
  // One conversation thread (root + nested replies) reads cleaner than four
  // separate top-level comments each with its own reply box.
  "issue-129": [
    comment("c-129-1", "member", "u-alex", "Let's use the existing session store for the token refresh — no new tables.", 38),
    comment("c-129-2", "agent", "a-claude", "Done. Implemented the redirect flow and token refresh against the session store, and opened a PR (linked on the right). Working through the edge cases now.", 22, "c-129-1"),
    comment("c-129-3", "member", "u-alex", "Make sure we validate the state param to prevent CSRF.", 12, "c-129-1"),
    comment("c-129-4", "agent", "a-claude", "Good catch — added state validation + a regression test. Re-running CI.", 5, "c-129-1"),
  ],
  "issue-133": [
    comment("c-133-1", "member", "u-sam", "Which events are in scope for v1 of the migration?", 90),
    comment("c-133-2", "agent", "a-gemini", "Starting with page_view, signup, and checkout; the long tail follows once the new schema is verified in staging.", 64, "c-133-1"),
  ],
  "issue-138": [
    comment("c-138-1", "agent", "a-codex", "Reproduced the flake — it's a race on the cart fixture between the checkout poll and the seed step. Adding an explicit wait + idempotent seed.", 30),
    comment("c-138-2", "member", "u-alex", "Nice, that's been haunting CI for weeks.", 18, "c-138-1"),
  ],
  "issue-124": [
    comment("c-124-1", "agent", "a-claude", "Bumped 14 dependencies, 2 majors held back behind a follow-up. Lockfile + changelog in the PR.", 140),
  ],
};

// Real pull requests from github.com/multica-ai/multica, linked to issues.
function pr(
  id: string,
  number: number,
  title: string,
  state: "open" | "merged",
  authorLogin: string,
  opts: { mergedMinsAgo?: number; checks?: "passed" | "pending" | "failed"; add?: number; del?: number; files?: number } = {},
): GitHubPullRequest {
  return {
    id,
    workspace_id: "ws-demo",
    repo_owner: "multica-ai",
    repo_name: "multica",
    number,
    title,
    state,
    html_url: `https://github.com/multica-ai/multica/pull/${number}`,
    branch: null,
    author_login: authorLogin,
    author_avatar_url: `https://github.com/${authorLogin}.png`,
    merged_at: state === "merged" ? mins(opts.mergedMinsAgo ?? 120) : null,
    closed_at: null,
    pr_created_at: mins(600),
    pr_updated_at: mins(opts.mergedMinsAgo ?? 60),
    mergeable_state: state === "open" ? "clean" : null,
    checks_conclusion: opts.checks ?? (state === "merged" ? "passed" : "pending"),
    checks_passed: opts.checks === "failed" ? 11 : 12,
    checks_failed: opts.checks === "failed" ? 1 : 0,
    checks_pending: opts.checks === "pending" ? 2 : 0,
    additions: opts.add ?? 180,
    deletions: opts.del ?? 40,
    changed_files: opts.files ?? 7,
  } as unknown as GitHubPullRequest;
}

export const PULL_REQUESTS: Record<string, GitHubPullRequest[]> = {
  "issue-129": [
    pr("pr-1", 3717, "refactor(server/lark): collapse HTTP_ENABLED + WS_ENABLED into the SECRET_KEY gate", "merged", "Bohan-J", { mergedMinsAgo: 80, add: 96, del: 120, files: 5 }),
  ],
  "issue-138": [
    pr("pr-2", 3712, "test(migrate): concurrent migration race test using real Postgres (MUL-2956)", "open", "ldnvnbl", { checks: "pending", add: 210, del: 8, files: 3 }),
  ],
  "issue-133": [
    pr("pr-3", 3716, "fix(execenv): refresh skills in place on reuse instead of accumulating duplicate dirs", "merged", "Bohan-J", { mergedMinsAgo: 200, add: 64, del: 31, files: 4 }),
  ],
  "issue-124": [
    pr("pr-4", 3718, "fix(lark): use named import for react-qr-code to survive electron-vite interop", "open", "Bohan-J", { checks: "passed", add: 12, del: 6, files: 1 }),
  ],
};

// Execution-log history per issue (api.listTasksByIssue) — running task(s)
// plus a couple of completed past runs, so the panel isn't empty.
function task(
  id: string,
  agentId: string,
  issueId: string,
  status: AgentTask["status"],
  summary: string,
  startMinsAgo: number,
  endMinsAgo: number | null,
): AgentTask {
  return {
    id,
    agent_id: agentId,
    runtime_id: "rt-demo",
    issue_id: issueId,
    status,
    priority: 0,
    dispatched_at: mins(startMinsAgo + 1),
    started_at: mins(startMinsAgo),
    completed_at: endMinsAgo == null ? null : mins(endMinsAgo),
    result: null,
    error: null,
    trigger_summary: summary,
    created_at: mins(startMinsAgo + 1),
    updated_at: mins(endMinsAgo ?? 0),
  } as unknown as AgentTask;
}

export const EXEC_LOG: Record<string, AgentTask[]> = {
  "issue-129": [
    task("t-129-run", "a-claude", "issue-129", "running", "Implement OAuth login flow", 4, null),
    task("t-129-1", "a-claude", "issue-129", "completed", "Scaffold OAuth routes", 95, 78),
    task("t-129-0", "a-claude", "issue-129", "failed", "Initial run", 180, 150),
  ],
  "issue-133": [
    task("t-133-run", "a-gemini", "issue-133", "running", "Migrate analytics events to new schema", 7, null),
    task("t-133-1", "a-gemini", "issue-133", "completed", "Draft migration plan", 120, 96),
  ],
  "issue-138": [
    task("t-138-run", "a-codex", "issue-138", "running", "Fix flaky checkout E2E test", 10, null),
    task("t-138-1", "a-codex", "issue-138", "completed", "Reproduce the flake", 60, 44),
  ],
  "issue-124": [
    task("t-124-1", "a-claude", "issue-124", "completed", "Weekly dependency upgrade sweep", 160, 138),
  ],
  "issue-112": [
    task("t-112-1", "a-codex", "issue-112", "completed", "Triage inbound bug reports", 1400, 1380),
  ],
};

// ---------------------------------------------------------------------------
// Agent transcripts (the "running" agent's live log, opened via the transcript
// button). Keyed by issue; listTaskMessages resolves a task id to its issue.
// ---------------------------------------------------------------------------
type Msg = Omit<TaskMessagePayload, "task_id" | "issue_id" | "seq">;
const think = (content: string): Msg => ({ type: "thinking", content });
const say = (content: string): Msg => ({ type: "text", content });
const tool = (t: string, input: Record<string, unknown>): Msg => ({ type: "tool_use", tool: t, input });
const result = (t: string, output: string): Msg => ({ type: "tool_result", tool: t, output });

export const TRANSCRIPT_BY_ISSUE: Record<string, Msg[]> = {
  "issue-129": [
    think("Let me see how auth is wired today — the session store and the existing login route — so I can reuse them instead of adding new tables."),
    tool("Read", { file_path: "server/internal/auth/session.go" }),
    result("Read", "// session.go — NewSession, Refresh, store-backed cookie sessions (138 lines)"),
    say("Got it. I'll add an OAuth redirect handler and store the token in the existing session store. Writing it now."),
    tool("Edit", { file_path: "server/internal/auth/oauth.go", summary: "add Authorize + Callback handlers, token exchange" }),
    result("Edit", "Created server/internal/auth/oauth.go (+96 −0)"),
    think("Need to validate the `state` param on the callback to prevent CSRF."),
    tool("Edit", { file_path: "server/internal/auth/oauth.go", summary: "validate state param against the signed cookie" }),
    result("Edit", "Updated server/internal/auth/oauth.go (+14 −1)"),
    tool("Bash", { command: "go test ./internal/auth/..." }),
    result("Bash", "ok  \tmultica/internal/auth\t1.82s"),
    say("Tests pass. Opened a PR with the redirect flow + token refresh + the state-validation test. Working through the last edge cases (expired token re-auth)."),
  ],
  "issue-133": [
    think("Mapping the old analytics events to the new schema. Let me read the current event definitions first."),
    tool("Read", { file_path: "packages/analytics/events.ts" }),
    result("Read", "// 41 event types; page_view / signup / checkout are the high-volume ones"),
    say("I'll migrate page_view, signup, and checkout first, then backfill the long tail once staging looks clean."),
    tool("Bash", { command: "pnpm migrate:analytics --events page_view,signup,checkout --dry-run" }),
    result("Bash", "dry-run: would migrate 3 event types · 1,243,902 rows · est. 4m12s"),
    say("Dry-run looks right. Running it against staging now and will diff the row counts before touching prod."),
  ],
  "issue-138": [
    think("Reproducing the flake first — running the checkout E2E in a tight loop to catch it."),
    tool("Bash", { command: "pnpm exec playwright test checkout --repeat-each=20" }),
    result("Bash", "18 passed, 2 failed — timeout waiting for [data-testid=cart-total]"),
    say("It's a race between the checkout poll and the cart-seed step. Adding an explicit wait + making the seed idempotent."),
    tool("Edit", { file_path: "e2e/tests/checkout.spec.ts", summary: "await cart-ready, dedupe seed" }),
    result("Edit", "Updated e2e/tests/checkout.spec.ts (+9 −3)"),
    tool("Bash", { command: "pnpm exec playwright test checkout --repeat-each=30" }),
    result("Bash", "30 passed (0 flaky)"),
    say("30/30 green now. Pushing the fix and linking the PR."),
  ],
};

// Mock skills for the "Skills" tab — reusable workflows agents can run.
export const SKILLS: { name: string; description: string }[] = [
  { name: "PR Review", description: "Read a diff, flag bugs & style issues, leave inline review comments." },
  { name: "Bug Repro", description: "Turn a bug report into a minimal reproduction and a failing test." },
  { name: "Release Notes", description: "Summarize merged PRs since the last tag into a changelog." },
  { name: "Dependency Sweep", description: "Bump dependencies, run the suite, open a PR with the lockfile diff." },
  { name: "Issue Triage", description: "Label, prioritize, and route inbound issues to the right owner." },
  { name: "Docs Sync", description: "Keep API docs in lockstep with code changes on every merge." },
];
