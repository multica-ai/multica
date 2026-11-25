#!/usr/bin/env node
// Seed realistic chat, Agent Builder and issue content into a managed dev
// environment, without a model and without a daemon:
//
//   make up C=api,web ARGS="--ephemeral"
//   make env-exec ARGS="-- node scripts/dev-fixtures.mjs all"
//   make env-exec ARGS="-- node scripts/dev-fixtures.mjs chat issue --json"
//   make env-exec ARGS="-- node scripts/dev-fixtures.mjs task --queued --json"
//   make env-exec ARGS="-- node scripts/dev-fixtures.mjs sidebar"
//   make env-exec ARGS="-- node scripts/dev-fixtures.mjs guest --json"
//   make env-exec ARGS="-- node scripts/dev-fixtures.mjs app-request --app posthog --member"
//
// Replies come from the daemon API, driven with the environment profile's user
// token: the script registers a fixture runtime, then claims, starts and
// completes each task it caused with canned Markdown, so content appears without
// running a model through `make up C=daemon`. The `task` fixture claims and
// starts its task but never completes it. Every run adds new sessions and
// issues and then exits; nothing keeps running unless `task --hold` was asked
// to heartbeat.

import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const KINDS = ["chat", "builder", "issue", "task", "sidebar", "guest", "app-request", "app-resume"];
// `sidebar` fills the whole Work panel, `guest` adds a workspace member, and the
// app fixtures need Composio, so
// `all` leaves them out.
const ALL_KINDS = ["chat", "builder", "issue", "task"];
// Kinds that need no fixture runtime.
const RUNTIMELESS_KINDS = ["issue", "guest"];
const FIXTURE_DAEMON_ID = "dev-fixtures";
const FIXTURE_AGENT_NAME = "Fixture Agent";
const FIXTURE_WORKER_NAME = "Fixture Worker";
// Matches the daemon's own heartbeat period.
const HEARTBEAT_INTERVAL_MS = 15_000;

const USAGE = `Usage: node scripts/dev-fixtures.mjs <all | chat | builder | issue | task | sidebar | guest | app-request | app-resume>...
         [--queued] [--hold] [--app SLUG] [--member] [--guest-email EMAIL] [--json]

  chat      a chat session: a user message and an assistant reply with a table, code and a long log line
  builder   an Agent Builder session with the same kind of reply and a filled-in draft
  issue     an issue with a wide description and a comment holding a diff
  task      an issue assigned to the Fixture Worker agent whose task stays running, with an execution log
  sidebar   a Work panel in use: 11 issues in 3 projects, read and unread notifications from 3 agents,
            pinned issues and a pinned project, and 2 saved views
  guest     guest@localhost as a guest of three shared projects (workspace-visible, private and
            archived), with an issue, a sub-issue, an assigned issue and unread notifications; a fourth
            project stays hidden from it. Prints the guest's token
  all       chat, builder, issue and task
  app-request  an issue assigned to the Fixture Agent whose run asks for an app account through
            apps_request_connection, then ends its turn; needs \`node scripts/fake-composio.mjs up\`
  app-resume   answer the run that resumed after the account was connected: it lists its apps,
            runs one read tool of each connected app, and completes
  --queued  task: also leave a second task queued behind the running one, on the same issue
  --hold    task: keep the fixture runtime heartbeating until Ctrl-C, so it stays online
  --app     app-request: the app slug to ask for (default googlecalendar)
  --member  app-request: start the run as Alice (alice@localhost), a plain member of the workspace;
            --json adds her member_token for signing a browser in as her
  --guest-email  guest: the guest's email (default guest@localhost); a new one gets a fresh user,
            clear of the per-recipient invitation limit
  --json    print the created ids and URLs as JSON instead of text; task adds task_id, auth_token and agent_id,
            guest adds guest_token and guest_user_id

A held task stays running for the server's reconnect grace (MULTICA_RUNTIME_RECONNECT_GRACE,
3 hours by default), but without --hold the runtime itself shows offline after about 150 seconds.

The app request goes to the person who started the run for an app the agent lacks or a personal
account, and to the workspace admins for a team account. Configure the Fixture Agent's apps in the
UI to choose which.

Run through \`make env-exec ARGS="-- node scripts/dev-fixtures.mjs all"\`.`;

function fail(message) {
  console.error(`[dev-fixtures] ${message}`);
  process.exit(1);
}

function parseCli(argv) {
  const kinds = new Set();
  let json = false;
  let queued = false;
  let hold = false;
  let app;
  let member = false;
  let guestEmail;
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "-h" || arg === "--help") {
      console.log(USAGE);
      process.exit(0);
    } else if (arg === "--json") json = true;
    else if (arg === "--queued") queued = true;
    else if (arg === "--hold") hold = true;
    else if (arg === "--member") member = true;
    else if (arg === "--app") {
      app = argv[++i];
      if (!app) fail(`--app needs an app slug.\n\n${USAGE}`);
    } else if (arg === "--guest-email") {
      guestEmail = argv[++i];
      if (!guestEmail?.includes("@")) fail(`--guest-email needs an email address.\n\n${USAGE}`);
    } else if (arg === "all") ALL_KINDS.forEach((kind) => kinds.add(kind));
    else if (KINDS.includes(arg)) kinds.add(arg);
    else fail(`unknown argument ${arg}\n\n${USAGE}`);
  }
  if (kinds.size === 0) fail(`name at least one fixture.\n\n${USAGE}`);
  if ((queued || hold) && !kinds.has("task")) fail(`--queued and --hold apply to the task fixture.\n\n${USAGE}`);
  if ((app || member) && !kinds.has("app-request")) fail(`--app and --member apply to the app-request fixture.\n\n${USAGE}`);
  if (guestEmail && !kinds.has("guest")) fail(`--guest-email applies to the guest fixture.\n\n${USAGE}`);
  return {
    kinds: KINDS.filter((kind) => kinds.has(kind)),
    json,
    queued,
    hold,
    app: app ?? "googlecalendar",
    member,
    guestEmail: guestEmail?.toLowerCase() ?? GUEST_EMAIL,
  };
}

function loadProfile(env) {
  if (!env.MULTICA_DEV_PROFILE) fail("MULTICA_DEV_PROFILE is not set; run this through `make env-exec`.");
  const home = env.MULTICA_DEV_PROFILES_HOME || join(homedir(), ".multica", "profiles");
  const dir = join(home, env.MULTICA_DEV_PROFILE);
  const file = join(dir, "config.json");
  let config;
  try {
    config = JSON.parse(readFileSync(file, "utf8"));
  } catch (error) {
    fail(`could not read ${file}: ${error.message}\nRun \`make up\` first; it signs the profile in.`);
  }
  if (!config.token || !config.workspace_id) fail(`${file} has no token or workspace; re-run \`make up\`.`);
  return {
    apiUrl: env.PORT ? `http://localhost:${env.PORT}` : config.server_url,
    appUrl: env.FRONTEND_PORT ? `http://localhost:${env.FRONTEND_PORT}` : config.app_url,
    token: config.token,
    workspaceId: config.workspace_id,
    dir,
  };
}

// Without a token the requests are anonymous, as the login endpoints need.
function client({ apiUrl, token, workspaceId }) {
  return async function request(method, path, body, headers = {}) {
    const response = await fetch(`${apiUrl}${path}`, {
      method,
      headers: {
        "Content-Type": "application/json",
        ...(token ? { Authorization: `Bearer ${token}`, "X-Workspace-ID": workspaceId } : {}),
        ...headers,
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await response.text();
    if (!response.ok) throw new Error(`${method} ${path} → ${response.status} ${text.slice(0, 300)}`);
    return text ? JSON.parse(text) : null;
  };
}

const sleep = (ms) => new Promise((done) => setTimeout(done, ms));

// --------------------------------------------------------------- content ---

const LONG_LOG_LINE =
  "2026-11-09T06:48:19.632113Z WARN  daemon/claim runtime=4f1c2a7e-0b8d-4c55-9a51-2f0f7a3c9e10 task=0193c2d4-5b6a-7e8f-9a0b-1c2d3e4f5a6b " +
  "agent=fixture-agent attempt=3 elapsed_ms=48213 queue_depth=17 lease_expires_at=2026-11-09T06:53:19Z " +
  'reason="prepare lease extended twice; workspace checkout still copying node_modules from cache /Users/dev/multica_workspaces/dev/4f1c2a7e/workdir"';

const WIDE_TABLE = `| Runtime | Provider | Region | p50 latency | p95 latency | Error rate | Tasks / day | Queue depth | Last deploy | Owner |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | --- | --- |
| build-mac-01 | Claude Code | us-west-2 | 1.8 s | 7.4 s | 0.4% | 1,284 | 3 | 2026-11-08 22:14 UTC | platform-infra |
| build-mac-02 | Codex | us-west-2 | 2.1 s | 9.0 s | 1.1% | 973 | 7 | 2026-11-08 22:14 UTC | platform-infra |
| cloud-pool-a | Claude Code | eu-central-1 | 1.2 s | 4.9 s | 0.2% | 4,612 | 0 | 2026-11-09 03:02 UTC | cloud-runtime |
| cloud-pool-b | Gemini CLI | ap-southeast-1 | 2.6 s | 11.3 s | 2.7% | 655 | 12 | 2026-11-07 17:40 UTC | cloud-runtime |
| laptop-dev | OpenCode | local | 0.9 s | 3.1 s | 0.0% | 88 | 0 | 2026-11-09 06:01 UTC | dev-tools |`;

const CODE_BLOCK = `\`\`\`go
// ClaimTaskForRuntime hands the oldest queued task to a runtime, unless the agent is already at its concurrency limit or the task's prepare lease belongs to another runtime.
func (s *TaskService) ClaimTaskForRuntime(ctx context.Context, runtimeID pgtype.UUID) (*db.AgentTaskQueue, error) {
	task, err := s.Queries.ClaimNextQueuedTask(ctx, db.ClaimNextQueuedTaskParams{RuntimeID: runtimeID, Now: pgtype.Timestamptz{Time: time.Now(), Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &task, err
}
\`\`\``;

const ASSISTANT_REPLY = `Here is how the five runtimes compared over the last 24 hours. **cloud-pool-b** is the outlier: its p95 is more than twice the fleet median and it holds the deepest queue.

${WIDE_TABLE}

The slow claims all come from the same path. The claim query itself is cheap; the time goes into the prepare lease while the checkout copies dependencies:

${CODE_BLOCK}

A representative log line from the slow runtime:

\`\`\`text
${LONG_LOG_LINE}
\`\`\`

Next steps:

1. Pre-warm the dependency cache on \`cloud-pool-b\` so the prepare lease is not extended.
2. Cap the queue at 8 per runtime and spill the rest to \`cloud-pool-a\`.
3. Re-check the p95 after the next deploy.`;

const CHAT_PROMPT =
  "Compare our runtimes over the last day: latency, error rate and queue depth. Show me a table, the code path behind slow claims, and one raw log line.";

const BUILDER_PROMPT =
  "I want an agent that watches our runtimes, flags the slow ones with a latency table, and proposes a fix with the relevant code.";

const BUILDER_DRAFT = {
  name: "Runtime Watch",
  description: "Flags slow or failing runtimes with a latency table and proposes a concrete fix.",
  instructions:
    "## Role\nYou watch the workspace's runtimes and report the ones that are slow or failing.\n\n## Workflow\n1. Compare p50, p95, error rate and queue depth across runtimes.\n2. Present the comparison as a table.\n3. Point at the code path or configuration behind the outlier.\n\n## Output\nA short summary, the table, and numbered next steps.",
  conversation_starters: [
    { label: "Daily runtime report", prompt: "Compare every runtime over the last 24 hours and flag the outliers." },
    { label: "Why is a runtime slow?", prompt: "cloud-pool-b has a high p95. Find the cause and propose a fix." },
  ],
  model: "",
  skill_ids: [],
  permission_scope: "private",
  member_ids: [],
};

const BUILDER_REPLY = `${ASSISTANT_REPLY}

I drafted **Runtime Watch** around that report. Should it post to an issue every morning, or only answer when asked?

<agent_draft>${JSON.stringify(BUILDER_DRAFT)}</agent_draft>`;

// Mirrors encodeBuilderInput in packages/core/agents/builder-protocol.ts, so the
// studio decodes the user turn the way it decodes one it sent itself.
function builderInput(runtime) {
  return `MULTICA_AGENT_BUILDER_INPUT\n${JSON.stringify(
    {
      user_request: BUILDER_PROMPT,
      current_draft: {
        name: "",
        description: "",
        instructions: "",
        conversation_starters: [],
        model: "",
        skill_ids: [],
        permission_scope: "private",
        member_ids: [],
      },
      selected_runtime: { id: runtime.id, name: runtime.name, provider: runtime.provider },
      available_runtime_models: null,
      available_workspace_skills: [],
      available_workspace_members: [],
    },
    null,
    2,
  )}`;
}

const ISSUE_DESCRIPTION = `Claims on \`cloud-pool-b\` regularly take longer than 45 seconds, and agents assigned to it start their runs late. The rest of the fleet is unaffected.

## Runtime comparison

${WIDE_TABLE}

## Evidence

\`\`\`text
${LONG_LOG_LINE}
${LONG_LOG_LINE.replace("attempt=3", "attempt=4").replace("48213", "51877")}
\`\`\`

## Where the time goes

${CODE_BLOCK}

## Acceptance

- p95 claim latency on \`cloud-pool-b\` is under 10 seconds for a full day.
- No prepare lease is extended more than once.`;

const ISSUE_COMMENT = `Found it: the prepare lease is extended before the cache copy starts, so a slow copy always needs a second extension. Moving the extension after the copy fixes the latency on my machine.

\`\`\`diff
--- a/server/internal/daemon/prepare.go
+++ b/server/internal/daemon/prepare.go
@@ -112,11 +112,11 @@ func (p *Preparer) Prepare(ctx context.Context, task *Task) error {
-	if err := p.client.ExtendPrepareLease(ctx, task.RuntimeID, task.ID); err != nil {
-		return fmt.Errorf("extend prepare lease for task %s on runtime %s: %w", task.ID, task.RuntimeID, err)
-	}
 	if err := p.cache.CopyInto(ctx, task.WorkDir, cache.Options{Include: []string{"node_modules", ".turbo", "server/bin"}, Concurrency: 8}); err != nil {
 		return fmt.Errorf("copy dependency cache into %s: %w", task.WorkDir, err)
 	}
+	if err := p.client.ExtendPrepareLease(ctx, task.RuntimeID, task.ID); err != nil {
+		return fmt.Errorf("extend prepare lease for task %s on runtime %s: %w", task.ID, task.RuntimeID, err)
+	}
\`\`\`

| Run | Before p95 | After p95 | Lease extensions |
| --- | ---: | ---: | ---: |
| 1 | 48.2 s | 6.1 s | 1 |
| 2 | 51.9 s | 5.8 s | 1 |
| 3 | 47.0 s | 6.4 s | 1 |`;

const TASK_ISSUE_DESCRIPTION = `Find out why the prepare lease on \`cloud-pool-b\` is extended twice per claim, and propose a fix.

The agent's task on this issue is held in \`running\` by \`scripts/dev-fixtures.mjs\`; it never finishes on its own.`;

const TASK_QUEUED_COMMENT = "When that is done, check whether `cloud-pool-a` shows the same pattern after the next deploy.";

const TASK_MESSAGES = [
  { type: "text", content: "Looking for where the daemon extends the prepare lease during a claim." },
  {
    type: "tool_use",
    call_id: "fixture-1",
    tool: "Bash",
    input: { command: "rg -n ExtendPrepareLease server/internal/daemon" },
  },
  {
    type: "tool_result",
    call_id: "fixture-1",
    tool: "Bash",
    output:
      "server/internal/daemon/prepare.go:112:\tif err := p.client.ExtendPrepareLease(ctx, task.RuntimeID, task.ID); err != nil {\nserver/internal/daemon/lease.go:41:func (c *Client) ExtendPrepareLease(ctx context.Context, runtimeID, taskID string) error {",
  },
  { type: "tool_use", call_id: "fixture-2", tool: "Read", input: { file_path: "server/internal/daemon/prepare.go" } },
  {
    type: "tool_result",
    call_id: "fixture-2",
    tool: "Read",
    output:
      "112\tif err := p.client.ExtendPrepareLease(ctx, task.RuntimeID, task.ID); err != nil {\n113\t\treturn fmt.Errorf(\"extend prepare lease: %w\", err)\n114\t}\n115\tif err := p.cache.CopyInto(ctx, task.WorkDir, cache.Options{Concurrency: 8}); err != nil {",
  },
  {
    type: "text",
    content: "The lease is extended before the dependency cache copy starts, so a slow copy always needs a second extension. Measuring the copy time on `cloud-pool-b` next.",
  },
];

// The sidebar fixture's cast. Each agent picks up one issue assigned to it and,
// with that task's token, acts on issues the dev user created; every inbox row
// comes from the server's own notification listeners.
const SIDEBAR_AGENTS = {
  atlas: { name: "Atlas", description: "Works through Desktop bugs." },
  nova: { name: "Nova", description: "Keeps the API reliable." },
  sage: { name: "Sage", description: "Writes product copy and onboarding research." },
};
for (const agent of Object.values(SIDEBAR_AGENTS)) {
  agent.instructions = "Fixture agent for local UI evidence. Its actions are scripted by scripts/dev-fixtures.mjs.";
}

// Statuses the sidebar's Projects section shows by default.
const SIDEBAR_PROJECTS = {
  desktop: { title: "Desktop 2.0", status: "in_progress", priority: "high" },
  onboarding: { title: "Onboarding revamp", status: "planned", priority: "medium" },
  reliability: { title: "API reliability", status: "in_progress", priority: "urgent" },
};

const SIDEBAR_VIEWS = [
  { name: "In review", query: { statusFilters: ["in_review"] } },
  { name: "Urgent and high", query: { priorityFilters: ["urgent", "high"] } },
];

// ageMinutes backdates the issue's notifications, so the inbox shows a spread
// of times rather than "just now"; read marks them read afterwards.
const SIDEBAR_ISSUES = {
  tabs: { title: "Tabs lose their scroll position after switching workspaces", project: "desktop", status: "todo", priority: "high", pinned: true, ageMinutes: 3 },
  menus: { title: "Native menu shortcuts ignore the focused window", project: "desktop", status: "todo", priority: "medium", ageMinutes: 26 },
  updater: { title: "Ship auto-update for the Windows build", project: "desktop", status: "todo", priority: "medium", ageMinutes: 26 * 60, read: true },
  checklist: { title: "Rewrite the first-run checklist copy", project: "onboarding", status: "todo", priority: "low", ageMinutes: 3 * 60 },
  invites: { title: "Invite step skips the role picker", project: "onboarding", status: "todo", priority: "medium", ageMinutes: 5 * 60, read: true },
  dropoff: { title: "Measure drop-off between sign-up and the first issue", project: "onboarding", status: "in_progress", priority: "medium", ageMinutes: 7 * 60, read: true },
  webhooks: { title: "Retry webhook deliveries with exponential backoff", project: "reliability", status: "todo", priority: "high", pinned: true, ageMinutes: 11 },
  ratelimit: { title: "Rate-limit the public search endpoint", project: "reliability", status: "todo", priority: "high", ageMinutes: 48 },
  claims: { title: "Slow claims on cloud-pool-b", project: "reliability", status: "in_progress", priority: "urgent", pinned: true, ageMinutes: 2 * 60 + 10 },
  flags: { title: "Clean up stale feature flags", status: "todo", priority: "low", ageMinutes: 2 * 24 * 60, read: true },
  roadmap: {
    title: "Q4 roadmap review notes",
    status: "todo",
    priority: "none",
    ageMinutes: 4 * 24 * 60,
    read: true,
    devComment: "Draft agenda is up. Please flag anything missing before Thursday.",
  },
};

const DROPOFF_TABLE = `| Step | Users | Drop-off |
| --- | ---: | ---: |
| Signed up | 1,204 | — |
| Created a workspace | 1,031 | 14% |
| Invited a teammate | 642 | 38% |
| Created the first issue | 518 | 19% |`;

const devMention = (dev) => `[@${dev.name}](mention://member/${dev.id})`;
const daysFromNow = (days) => new Date(Date.now() + days * 86_400_000).toISOString().slice(0, 10);

// One agent at a time holds a running task on the issue it owns; its actions
// use that task's token, so they are attributed to the agent. No action
// touches an issue assigned to another agent, which would wake that agent.
// Status changes are muted by default (MUL-9449), so they move the board but
// every issue also gets an action that notifies.
const SIDEBAR_SCRIPTS = [
  {
    agent: "atlas",
    owns: "tabs",
    report:
      "The scroll position is stored per tab but restored before the list has rendered, so it clamps to 0. Restoring it after the first paint fixes it; a PR is up.",
    actions: [
      { issue: "tabs", update: { status: "in_progress" } },
      {
        issue: "menus",
        comment: (dev) =>
          `${devMention(dev)} the accelerators are registered on the first window only. Should Cmd+W close the focused issue window or always the main one?`,
      },
      { issue: "claims", update: { status: "in_review" } },
      {
        issue: "claims",
        comment: () => "Moving the lease extension after the cache copy brings p95 back under 7 seconds. Over to review.",
      },
    ],
  },
  {
    agent: "nova",
    owns: "webhooks",
    report:
      "Deliveries now retry 5 times with backoff from 30 seconds to 1 hour, and a delivery that keeps failing is marked failed instead of retrying forever. Ready for review.",
    actions: [
      { issue: "webhooks", update: { status: "in_review" } },
      { issue: "ratelimit", update: (dev) => ({ assignee_type: "member", assignee_id: dev.id }) },
      {
        issue: "invites",
        comment: () =>
          "Reproduced: the role picker is skipped whenever the invite link carries a `role` parameter, even an empty one.",
      },
      { issue: "updater", update: () => ({ due_date: daysFromNow(10) }) },
    ],
  },
  {
    agent: "sage",
    owns: "checklist",
    report:
      "I drafted the new checklist copy, but the step order depends on whether invites move before the first issue. Blocked until that is decided.",
    actions: [
      { issue: "checklist", update: { status: "blocked" } },
      { issue: "flags", update: { priority: "urgent" } },
      { issue: "dropoff", comment: () => `Last 30 days, by step:\n\n${DROPOFF_TABLE}\n\nThe invite step loses the most people.` },
      { issue: "roadmap", react: "🎉" },
    ],
  },
];

// The guest fixture's cast. The guest signs in with the dev verification code
// like dev@localhost, and is a guest of the shared projects only: one visible
// to the workspace, one private and one archived.
const GUEST_EMAIL = "guest@localhost";
const GUEST_NAME = "Gina Guest";
const GUEST_CAPABILITIES = { "X-Client-Capabilities": "workspace-guests-v1" };

const GUEST_PROJECTS = {
  shared: {
    title: "Client portal",
    description: "Shared with the guest by scripts/dev-fixtures.mjs.",
    status: "in_progress",
    priority: "high",
    sharedWithGuest: true,
  },
  private: {
    title: "Client contracts",
    description: "A private project shared with the guest by scripts/dev-fixtures.mjs.",
    status: "in_progress",
    priority: "medium",
    visibility: "private",
    sharedWithGuest: true,
  },
  archived: {
    title: "Client portal v1",
    description: "An archived project shared with the guest by scripts/dev-fixtures.mjs.",
    status: "completed",
    priority: "low",
    sharedWithGuest: true,
    archived: true,
  },
  internal: {
    title: "Internal planning",
    description: "Not shared with the guest; the guest must not see it or its issues.",
    status: "planned",
    priority: "medium",
    sharedWithGuest: false,
  },
};

const GUEST_ISSUES = {
  review: {
    project: "shared",
    title: "Review the client portal sign-up flow",
    description: "Walk through sign-up as a new client and note every step that asks for something twice.",
    status: "in_progress",
    priority: "high",
  },
  form: {
    project: "shared",
    parent: "review",
    title: "Collect feedback on the sign-up form",
    status: "todo",
    priority: "medium",
  },
  export: {
    project: "shared",
    title: "Export invoices as CSV",
    description: "Clients want their invoices as CSV for their own bookkeeping.",
    status: "todo",
    priority: "medium",
  },
  contract: {
    project: "private",
    title: "Countersign the support contract",
    description: "The client signed the renewed support contract; it needs our countersignature.",
    status: "in_review",
    priority: "medium",
  },
  migration: {
    project: "archived",
    title: "Move v1 clients to the new portal",
    description: "Every v1 client now signs in through the new portal.",
    status: "done",
    priority: "low",
  },
  pricing: {
    project: "internal",
    title: "Decide renewal pricing for the client",
    description: "Internal only: margins and the discount we can offer at renewal.",
    status: "todo",
    priority: "high",
  },
};

const guestMention = (guest) => `[@${guest.name}](mention://member/${guest.id})`;
// `make up` writes the code into the env file, which `make env-exec` exports.
const devCode = () => process.env.MULTICA_DEV_VERIFICATION_CODE || "888888";

// -------------------------------------------------------------- fixtures ---

// The version a daemon built from this checkout reports, as the Makefile
// derives it, so UI that parses the CLI version sees a realistic value.
function cliVersion() {
  try {
    return execFileSync("git", ["describe", "--tags", "--match", "v[0-9]*", "--always", "--dirty"], {
      cwd: dirname(fileURLToPath(import.meta.url)),
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
  } catch {
    return "dev";
  }
}

async function ensureRuntime(request, workspaceId) {
  const registered = await request("POST", "/api/daemon/register", {
    workspace_id: workspaceId,
    daemon_id: FIXTURE_DAEMON_ID,
    device_name: "Dev fixtures",
    cli_version: cliVersion(),
    runtimes: [{ name: "Fixture runtime", type: "claude", version: "fixture", status: "online" }],
  });
  const runtime = registered?.runtimes?.[0];
  if (!runtime?.id) throw new Error(`runtime registration returned no runtime: ${JSON.stringify(registered).slice(0, 300)}`);
  return runtime;
}

async function ensureAgent(request, runtime, { name, ...fields }) {
  const agents = (await request("GET", "/api/agents")) ?? [];
  const existing = (Array.isArray(agents) ? agents : agents.agents ?? []).find(
    (agent) => agent.name === name && agent.runtime_id === runtime.id,
  );
  if (existing) return existing;
  return request("POST", "/api/agents", { name, ...fields, runtime_id: runtime.id, visibility: "workspace" });
}

// The Fixture Agent's tasks finish at once, but the Fixture Worker's stay
// running for hours, so they get separate agents: held tasks must not use up
// the concurrency the chat fixtures need.
const FIXTURE_AGENT = {
  name: FIXTURE_AGENT_NAME,
  description: "Answers with canned Markdown from scripts/dev-fixtures.mjs.",
  instructions: "Fixture agent for local UI evidence. Its replies are written by scripts/dev-fixtures.mjs.",
};
const FIXTURE_WORKER = {
  name: FIXTURE_WORKER_NAME,
  description: "Holds tasks in running or queued for scripts/dev-fixtures.mjs.",
  instructions: "Fixture agent for local UI evidence. Its tasks are started by scripts/dev-fixtures.mjs and never finish.",
  max_concurrent_tasks: 50,
};

// A send or an assignment enqueues the task; the fixture runtime is the only
// claimer, so the expected task arrives within a few polls. Anything else it
// can claim is left over from an interrupted run and is answered, so it cannot
// block ours. Held tasks are never claimable again: they are running already,
// or queued behind a running task on the same issue.
async function claimAndStart(request, runtime, taskId, leftoverOutput) {
  const deadline = Date.now() + 30_000;
  for (;;) {
    const claimed = await request("POST", `/api/daemon/runtimes/${runtime.id}/tasks/claim`, {});
    const task = claimed?.task;
    if (task) {
      await request("POST", `/api/daemon/tasks/${task.id}/start`);
      if (task.id === taskId) return task;
      await request("POST", `/api/daemon/tasks/${task.id}/complete`, { output: leftoverOutput });
      continue;
    }
    if (Date.now() > deadline) throw new Error(`task ${taskId} was never offered to the fixture runtime`);
    await sleep(500);
  }
}

async function answer(request, runtime, taskId, output) {
  await claimAndStart(request, runtime, taskId, output);
  await request("POST", `/api/daemon/tasks/${taskId}/complete`, { output });
}

// An assignment or a comment enqueues server-side, so the new task is found
// in the issue's task runs rather than in the response.
async function waitForQueuedTask(request, issueId, agentId, ignore = new Set()) {
  const deadline = Date.now() + 10_000;
  for (;;) {
    const runs = (await request("GET", `/api/issues/${issueId}/task-runs`)) ?? [];
    const task = (Array.isArray(runs) ? runs : runs.tasks ?? []).find(
      (run) => run.agent_id === agentId && run.status === "queued" && !ignore.has(run.id),
    );
    if (task) return task;
    if (Date.now() > deadline) throw new Error(`no queued task for agent ${agentId} appeared on issue ${issueId}`);
    await sleep(300);
  }
}

async function seedChat({ request, runtime, agent, appUrl, slug }) {
  const session = await request("POST", "/api/chat/sessions", {
    agent_id: agent.id,
    title: "Fixture: runtime latency report",
  });
  const sent = await request("POST", `/api/chat/sessions/${session.id}/messages`, { content: CHAT_PROMPT });
  await answer(request, runtime, sent.task_id, ASSISTANT_REPLY);
  return { session_id: session.id, url: `${appUrl}/${slug}/chat?session=${encodeURIComponent(session.id)}` };
}

async function seedBuilder({ request, runtime, appUrl, slug }) {
  const created = await request("POST", "/api/agent-builder/sessions", { runtime_id: runtime.id });
  const sent = await request("POST", `/api/chat/sessions/${created.session_id}/messages`, {
    content: builderInput(runtime),
  });
  await answer(request, runtime, sent.task_id, BUILDER_REPLY);
  return {
    session_id: created.session_id,
    url: `${appUrl}/${slug}/agents/new/ai/${encodeURIComponent(created.session_id)}`,
  };
}

async function seedIssue({ request, appUrl, slug }) {
  const issue = await request("POST", "/api/issues", {
    title: "Fixture: slow claims on cloud-pool-b",
    description: ISSUE_DESCRIPTION,
    status: "in_progress",
    priority: "high",
    allow_duplicate: true,
  });
  await request("POST", `/api/issues/${issue.id}/comments`, { content: ISSUE_COMMENT });
  return { issue_id: issue.id, identifier: issue.identifier, url: `${appUrl}/${slug}/issues/${issue.identifier}` };
}

async function seedTask({ request, profile, runtime, worker, appUrl, slug, queued }) {
  const issue = await request("POST", "/api/issues", {
    title: "Fixture: agent task in progress",
    description: TASK_ISSUE_DESCRIPTION,
    status: "todo",
    priority: "medium",
    assignee_type: "agent",
    assignee_id: worker.id,
    allow_duplicate: true,
  });
  const pending = await waitForQueuedTask(request, issue.id, worker.id);
  const task = await claimAndStart(request, runtime, pending.id, ASSISTANT_REPLY);
  await request("POST", `/api/daemon/tasks/${task.id}/messages`, {
    messages: TASK_MESSAGES.map((message, index) => ({ seq: index + 1, ...message })),
  });
  // A real run moves its issue with its own task token, so the activity is the agent's.
  const asAgent = client({ ...profile, token: task.auth_token });
  await asAgent("PUT", `/api/issues/${issue.id}`, { status: "in_progress" });

  const result = {
    issue_id: issue.id,
    identifier: issue.identifier,
    url: `${appUrl}/${slug}/issues/${issue.identifier}`,
    task_id: task.id,
    auth_token: task.auth_token,
    agent_id: worker.id,
    runtime_id: runtime.id,
  };
  if (queued) {
    await request("POST", `/api/issues/${issue.id}/comments`, { content: TASK_QUEUED_COMMENT });
    result.queued_task_id = (await waitForQueuedTask(request, issue.id, worker.id, new Set([task.id]))).id;
  }
  return result;
}

const MEMBER_EMAIL = "alice@localhost";

// Signs Alice in with the environment's fixed verification code and makes her
// a plain member of the workspace, so runs she starts route team-account
// requests to the admins and she sees "Ask an admin" in the UI.
async function ensureMember(request, profile) {
  const post = async (path, body) => {
    const response = await fetch(`${profile.apiUrl}${path}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    const text = await response.text();
    if (!response.ok) throw new Error(`POST ${path} → ${response.status} ${text.slice(0, 300)}`);
    return text ? JSON.parse(text) : null;
  };
  await post("/auth/send-code", { email: MEMBER_EMAIL });
  const { token } = await post("/auth/verify-code", {
    email: MEMBER_EMAIL,
    code: process.env.MULTICA_DEV_VERIFICATION_CODE || "888888",
  });
  const asMember = client({ ...profile, token });
  await asMember("PATCH", "/api/me", { name: "Alice" });
  await asMember("POST", "/api/me/onboarding/complete", {}).catch(() => {});

  const members = listOf(await request("GET", `/api/workspaces/${profile.workspaceId}/members`), "members");
  if (!members.some((m) => m.email === MEMBER_EMAIL || m.user?.email === MEMBER_EMAIL)) {
    await request("POST", `/api/workspaces/${profile.workspaceId}/members`, { email: MEMBER_EMAIL, role: "member" });
    for (const invitation of listOf(await asMember("GET", "/api/invitations"), "invitations")) {
      if (invitation.workspace_id === profile.workspaceId) await asMember("POST", `/api/invitations/${invitation.id}/accept`, {});
    }
  }
  return { email: MEMBER_EMAIL, token };
}

// Calls the apps gateway the claim handed this run, the way an agent's MCP
// client does. Null when the claim has none: Composio is off.
function appsGateway(task) {
  const server = task.agent?.mcp_config?.mcpServers?.multica_apps ?? task.mcp_config?.mcpServers?.multica_apps;
  if (!server?.url) return null;
  let id = 0;
  return async function call(name, args = {}) {
    const response = await fetch(server.url, {
      method: "POST",
      headers: { "Content-Type": "application/json", ...server.headers },
      body: JSON.stringify({ jsonrpc: "2.0", id: ++id, method: "tools/call", params: { name, arguments: args } }),
    });
    const body = await response.json().catch(() => null);
    if (!response.ok || !body?.result) {
      throw new Error(`apps gateway ${name} → ${response.status} ${JSON.stringify(body).slice(0, 300)}`);
    }
    return { text: body.result.content?.[0]?.text ?? "", isError: Boolean(body.result.isError) };
  };
}

const NO_GATEWAY =
  "the run was handed no apps gateway. Start the fake Composio first: `node scripts/fake-composio.mjs up`.";

async function seedAppRequest({ request, profile, runtime, agent, appUrl, slug, app, member }) {
  const starter = member ? await ensureMember(request, profile) : null;
  const asStarter = starter ? client({ ...profile, token: starter.token }) : request;
  const issue = await asStarter("POST", "/api/issues", {
    title: `Fixture: a run that needs ${app}`,
    description: `The Fixture Agent asks for ${app} through apps_request_connection and ends its turn. Connect the account and run \`dev-fixtures.mjs app-resume\` to finish it.`,
    status: "todo",
    priority: "medium",
    assignee_type: "agent",
    assignee_id: agent.id,
    allow_duplicate: true,
  });
  const pending = await waitForQueuedTask(request, issue.id, agent.id);
  const task = await claimAndStart(request, runtime, pending.id, ASSISTANT_REPLY);
  const call = appsGateway(task);
  const requested = call
    ? await call("apps_request_connection", { app, summary: `Needs ${app} to finish ${issue.identifier}.` })
    : { text: NO_GATEWAY, isError: true };
  if (requested.isError) {
    await request("POST", `/api/daemon/tasks/${task.id}/fail`, { error: requested.text }).catch(() => {});
    throw new Error(`apps_request_connection(${app}) failed: ${requested.text}`);
  }
  await request("POST", `/api/daemon/tasks/${task.id}/complete`, {
    output: `I need ${app} for this and asked for it. I'll continue once it is connected.`,
  });
  return {
    issue_id: issue.id,
    identifier: issue.identifier,
    url: `${appUrl}/${slug}/issues/${issue.identifier}`,
    task_id: task.id,
    agent_id: agent.id,
    request: requested.text,
    ...(starter ? { member_email: starter.email, member_token: starter.token } : {}),
  };
}

// A connected account resumes the waiting run as a new task for the same
// agent. This claims it, uses every connected app once through the gateway,
// so the app pages show the call in Recent activity, and completes it.
async function seedAppResume({ request, runtime, appUrl, slug }) {
  const deadline = Date.now() + 30_000;
  let task;
  while (!(task = (await request("POST", `/api/daemon/runtimes/${runtime.id}/tasks/claim`, {}))?.task)) {
    if (Date.now() > deadline) {
      throw new Error("no run is waiting to resume. Connect the account the app-request run asked for, then run app-resume again.");
    }
    await sleep(500);
  }
  await request("POST", `/api/daemon/tasks/${task.id}/start`);
  const call = appsGateway(task);
  if (!call) {
    await request("POST", `/api/daemon/tasks/${task.id}/complete`, { output: ASSISTANT_REPLY });
    throw new Error(`claimed task ${task.id}, but ${NO_GATEWAY}`);
  }
  const { apps = [] } = JSON.parse((await call("apps_list")).text || "{}");
  const used = [];
  for (const app of apps.filter((a) => a.status === "connected" && !a.server)) {
    const { tools = [] } = JSON.parse((await call("apps_search_tools", { app: app.app })).text || "{}");
    const tool = tools.find((t) => t.class === "read" && t.policy === "allow");
    if (!tool) continue;
    const result = await call("apps_run_tool", { tool: tool.tool, arguments: {}, summary: "Fixture: read once after resuming" });
    used.push(`${tool.tool}${result.isError ? ` (failed: ${result.text})` : ""}`);
  }
  const summary = apps.map((a) => `${a.name}: ${a.status}`).join(", ") || "no apps";
  await request("POST", `/api/daemon/tasks/${task.id}/complete`, {
    output: `Resumed after the connection. Apps: ${summary}.${used.length ? ` Ran ${used.join(", ")}.` : ""}`,
  });
  const issue = task.issue_id ? await request("GET", `/api/issues/${task.issue_id}`) : null;
  return {
    task_id: task.id,
    issue_id: task.issue_id,
    url: issue ? `${appUrl}/${slug}/issues/${issue.identifier}` : appUrl,
    apps,
    ran: used,
  };
}

const listOf = (response, key) => (Array.isArray(response) ? response : response?.[key] ?? []);

async function ensureProject(request, existing, project) {
  return existing.find((p) => p.title === project.title && !p.archived_at) ?? request("POST", "/api/projects", project);
}

async function ensureView(request, existing, view) {
  return (
    existing.find((v) => v.name === view.name) ??
    request("POST", "/api/issue-views", { ...view, scope_type: "workspace", display: {} })
  );
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// The API stamps every notification with the time it was delivered, so the
// fixture's rows would all read "just now". Shifting them back is the one
// direct database write; without psql the rows keep their real times.
function backdateInbox({ workspaceId, recipientId, issues }) {
  const skip = (reason) => console.error(`[dev-fixtures] sidebar: notifications keep their real times; ${reason}`);
  const databaseUrl = process.env.DATABASE_URL;
  if (!databaseUrl) return skip("DATABASE_URL is not set.");
  const ids = [workspaceId, recipientId, ...issues.map((issue) => issue.id)];
  if (!ids.every((id) => UUID.test(id))) return skip("an id is not a UUID.");
  const values = issues.map((issue) => `('${issue.id}'::uuid, ${Math.trunc(issue.ageMinutes)})`).join(", ");
  const sql = `UPDATE inbox_item AS i SET created_at = i.created_at - make_interval(mins => v.minutes)
    FROM (VALUES ${values}) AS v(issue_id, minutes)
    WHERE i.issue_id = v.issue_id AND i.workspace_id = '${workspaceId}'
      AND i.recipient_type = 'member' AND i.recipient_id = '${recipientId}'`;
  try {
    execFileSync("psql", [databaseUrl, "-v", "ON_ERROR_STOP=1", "-qAt", "-c", sql], { stdio: ["ignore", "ignore", "pipe"] });
  } catch (error) {
    if (error.code === "ENOENT") skip("psql is not installed.");
    else skip(`psql failed: ${String(error.stderr ?? "").trim().split("\n")[0]}`);
  }
}

async function seedSidebar({ request, profile, runtime, appUrl, slug }) {
  const dev = await request("GET", "/api/me");

  const agents = {};
  for (const [key, agent] of Object.entries(SIDEBAR_AGENTS)) agents[key] = await ensureAgent(request, runtime, agent);

  const existingProjects = listOf(await request("GET", "/api/projects"), "projects");
  const projects = {};
  for (const [key, project] of Object.entries(SIDEBAR_PROJECTS)) {
    projects[key] = await ensureProject(request, existingProjects, project);
  }

  const existingViews = listOf(await request("GET", "/api/issue-views?scope_type=all"), "views");
  const views = [];
  for (const view of SIDEBAR_VIEWS) views.push(await ensureView(request, existingViews, view));

  const owner = Object.fromEntries(SIDEBAR_SCRIPTS.map((script) => [script.owns, script.agent]));
  const issues = {};
  for (const [key, spec] of Object.entries(SIDEBAR_ISSUES)) {
    if (owner[key]) continue;
    issues[key] = await createSidebarIssue(request, spec, projects);
    if (spec.devComment) {
      issues[key].devComment = await request("POST", `/api/issues/${issues[key].id}/comments`, { content: spec.devComment });
    }
  }

  for (const script of SIDEBAR_SCRIPTS) {
    const agent = agents[script.agent];
    const own = await createSidebarIssue(request, SIDEBAR_ISSUES[script.owns], projects, agent);
    issues[script.owns] = own;
    const pending = await waitForQueuedTask(request, own.id, agent.id);
    const task = await claimAndStart(request, runtime, pending.id, script.report);
    const asAgent = client({ ...profile, token: task.auth_token });
    for (const action of script.actions) {
      const issue = issues[action.issue];
      if (action.update) {
        const body = typeof action.update === "function" ? action.update(dev) : action.update;
        await asAgent("PUT", `/api/issues/${issue.id}`, body);
      }
      if (action.comment) await asAgent("POST", `/api/issues/${issue.id}/comments`, { content: action.comment(dev) });
      if (action.react) await asAgent("POST", `/api/comments/${issue.devComment.id}/reactions`, { emoji: action.react });
    }
    await request("POST", `/api/daemon/tasks/${task.id}/complete`, { output: script.report });
  }

  for (const [key, spec] of Object.entries(SIDEBAR_ISSUES)) {
    if (spec.read) await request("POST", `/api/inbox/issues/${issues[key].id}/read`);
  }
  backdateInbox({
    workspaceId: profile.workspaceId,
    recipientId: dev.id,
    issues: Object.entries(SIDEBAR_ISSUES).map(([key, spec]) => ({ id: issues[key].id, ageMinutes: spec.ageMinutes })),
  });

  // Pins persist across runs, so only the issues of this run are added.
  const pinned = new Set((listOf(await request("GET", "/api/pins"), "pins")).map((pin) => `${pin.item_type}:${pin.item_id}`));
  const toPin = [
    ...Object.entries(SIDEBAR_ISSUES)
      .filter(([, spec]) => spec.pinned)
      .map(([key]) => ({ item_type: "issue", item_id: issues[key].id })),
    { item_type: "project", item_id: projects.desktop.id },
  ];
  for (const pin of toPin) {
    if (!pinned.has(`${pin.item_type}:${pin.item_id}`)) await request("POST", "/api/pins", pin);
  }

  const inbox = listOf(await request("GET", "/api/inbox"), "items");
  const notified = new Set(inbox.map((item) => item.issue_id));
  const unread = new Set(inbox.filter((item) => !item.read).map((item) => item.issue_id));
  return {
    url: `${appUrl}/${slug}/issues`,
    inbox_issues: notified.size,
    inbox_unread_issues: unread.size,
    issues: Object.fromEntries(Object.entries(issues).map(([key, issue]) => [key, issue.identifier])),
    project_ids: Object.values(projects).map((project) => project.id),
    view_ids: views.map((view) => view.id),
    agent_ids: Object.values(agents).map((agent) => agent.id),
  };
}

function createSidebarIssue(request, spec, projects, agent) {
  return request("POST", "/api/issues", {
    title: spec.title,
    status: spec.status,
    priority: spec.priority,
    project_id: spec.project ? projects[spec.project].id : undefined,
    ...(agent ? { assignee_type: "agent", assignee_id: agent.id } : {}),
    allow_duplicate: true,
  });
}

// send-code is rate-limited to 5 a minute, so the guest's token is kept in the
// environment's profile directory and a rerun signs in again only when that
// token no longer works. `make destroy` removes it with the profile.
async function guestSession(profile, email) {
  const file = join(profile.dir, "guest.json");
  try {
    const { token } = JSON.parse(readFileSync(file, "utf8"));
    const me = await client({ ...profile, token })("GET", "/api/me");
    if (me?.email === email) return { token, user: me };
  } catch {
    // No saved token, or it stopped working: sign in below.
  }

  const anonymous = client({ ...profile, token: "" });
  await anonymous("POST", "/auth/send-code", { email });
  const verified = await anonymous("POST", "/auth/verify-code", { email, code: devCode() });
  const created = await client({ ...profile, token: verified.token })("POST", "/api/tokens", {
    name: "dev-fixtures guest",
    expires_in_days: 365,
  });
  const asGuest = client({ ...profile, token: created.token });
  const user = await asGuest("PATCH", "/api/me", { name: GUEST_NAME });
  writeFileSync(file, `${JSON.stringify({ email, token: created.token }, null, 2)}\n`, { mode: 0o600 });
  return { token: created.token, user };
}

// An archived project takes no new issues, so one an earlier run archived is
// restored here and archived again once this run's issues are in.
async function ensureGuestProject(request, existing, { sharedWithGuest: _shared, archived, ...project }) {
  const found = existing.find((p) => p.title === project.title && (archived || !p.archived_at));
  if (!found) return request("POST", "/api/projects", project);
  if (found.archived_at) await request("POST", `/api/projects/${found.id}/restore`);
  return found;
}

// Invites the guest to the shared projects and accepts as the guest, the way
// the web app does. On a rerun the guest is already a member and keeps its
// other projects, but always has the shared ones and never the internal one.
async function ensureGuestMember({ request, asGuest, workspaceId, guest, email, projects }) {
  const sharedIds = Object.keys(GUEST_PROJECTS)
    .filter((key) => GUEST_PROJECTS[key].sharedWithGuest)
    .map((key) => projects[key].id);
  const workspaces = (await asGuest("GET", "/api/workspaces")) ?? [];
  if (!workspaces.some((ws) => ws.id === workspaceId)) {
    try {
      await request("POST", `/api/workspaces/${workspaceId}/members`, {
        email,
        role: "guest",
        project_ids: sharedIds,
      });
    } catch (error) {
      // An interrupted run can leave its invitation pending; accept that one.
      if (!error.message.includes("invitation already pending")) throw error;
    }
    const invitations = listOf(await asGuest("GET", "/api/invitations"), "invitations");
    const invitation = invitations.find((inv) => inv.workspace_id === workspaceId);
    if (!invitation) throw new Error(`no pending invitation for ${email} in workspace ${workspaceId}`);
    await asGuest("POST", `/api/invitations/${invitation.id}/accept`);
  }

  const members = listOf(
    await request("GET", `/api/workspaces/${workspaceId}/members`, undefined, GUEST_CAPABILITIES),
    "members",
  );
  const member = members.find((m) => m.user_id === guest.id);
  if (member?.role !== "guest") {
    throw new Error(`${email} is a ${member?.role ?? "missing"} member of this workspace, not a guest`);
  }
  const current = member.project_ids ?? [];
  const fixtureIds = new Set(Object.values(projects).map((project) => project.id));
  const wanted = [...current.filter((id) => !fixtureIds.has(id)), ...sharedIds];
  if (current.length !== wanted.length || !wanted.every((id) => current.includes(id))) {
    await request("PUT", `/api/workspaces/${workspaceId}/members/${member.id}/projects`, { project_ids: wanted });
  }
  return member;
}

async function seedGuest({ request, profile, appUrl, slug, guestEmail }) {
  const existingProjects = listOf(await request("GET", "/api/projects?include_archived=true"), "projects");
  const projects = {};
  for (const [key, project] of Object.entries(GUEST_PROJECTS)) {
    projects[key] = await ensureGuestProject(request, existingProjects, project);
  }

  const session = await guestSession(profile, guestEmail);
  const guest = session.user;
  const asGuest = client({ ...profile, token: session.token });
  await ensureGuestMember({ request, asGuest, workspaceId: profile.workspaceId, guest, email: guestEmail, projects });

  const issues = {};
  for (const [key, spec] of Object.entries(GUEST_ISSUES)) {
    issues[key] = await request("POST", "/api/issues", {
      title: spec.title,
      description: spec.description,
      status: spec.status,
      priority: spec.priority,
      project_id: projects[spec.project].id,
      parent_issue_id: spec.parent ? issues[spec.parent].id : undefined,
      allow_duplicate: true,
    });
  }

  // Each notifies the guest on a different issue: a mention, a reply in a
  // thread the guest started, and an assignment.
  await request("POST", `/api/issues/${issues.review.id}/comments`, {
    content: `${guestMention(guest)} the new sign-up steps are live on staging. Could you go through them before Friday?`,
  });
  const question = await asGuest("POST", `/api/issues/${issues.form.id}/comments`, {
    content: "The form asks for the company name twice, once on each step. Is that on purpose?",
  });
  await request("POST", `/api/issues/${issues.form.id}/comments`, {
    content: "Not on purpose. The second field goes away in the next build.",
    parent_id: question.id,
  });
  await request("PUT", `/api/issues/${issues.export.id}`, { assignee_type: "member", assignee_id: guest.id });

  for (const [key, spec] of Object.entries(GUEST_PROJECTS)) {
    if (spec.archived) await request("POST", `/api/projects/${projects[key].id}/archive`);
  }

  const inbox = listOf(await asGuest("GET", "/api/inbox"), "items");
  const issueUrl = (issue) => `${appUrl}/${slug}/issues/${issue.identifier}`;
  return {
    url: `${appUrl}/${slug}/inbox`,
    guest_email: guestEmail,
    guest_code: devCode(),
    guest_user_id: guest.id,
    guest_token: session.token,
    inbox_issues: new Set(inbox.map((item) => item.issue_id)).size,
    inbox_unread_issues: new Set(inbox.filter((item) => !item.read).map((item) => item.issue_id)).size,
    projects: Object.fromEntries(
      Object.entries(projects).map(([key, project]) => [
        key,
        {
          id: project.id,
          title: project.title,
          url: `${appUrl}/${slug}/projects/${project.id}`,
          visibility: GUEST_PROJECTS[key].visibility ?? "workspace",
          archived: !!GUEST_PROJECTS[key].archived,
          visible_to_guest: GUEST_PROJECTS[key].sharedWithGuest,
        },
      ]),
    ),
    issues: Object.fromEntries(
      Object.entries(issues).map(([key, issue]) => [
        key,
        {
          id: issue.id,
          identifier: issue.identifier,
          title: issue.title,
          url: issueUrl(issue),
          visible_to_guest: GUEST_PROJECTS[GUEST_ISSUES[key].project].sharedWithGuest,
        },
      ]),
    ),
  };
}

// Keeps the runtime online, and its held tasks with it, the way a daemon does.
// A failed beat (an API restart) is reported and retried on the next tick.
async function holdRuntime(request, runtime) {
  console.error(`[dev-fixtures] holding: ${runtime.name} heartbeats every ${HEARTBEAT_INTERVAL_MS / 1000}s; Ctrl-C to stop.`);
  for (const signal of ["SIGINT", "SIGTERM"]) process.on(signal, () => process.exit(0));
  for (;;) {
    await sleep(HEARTBEAT_INTERVAL_MS);
    try {
      await request("POST", "/api/daemon/heartbeat", { runtime_id: runtime.id });
    } catch (error) {
      console.error(`[dev-fixtures] heartbeat failed, retrying: ${error.message}`);
    }
  }
}

async function main() {
  const { kinds, json, queued, hold, app, member, guestEmail } = parseCli(process.argv.slice(2));
  const profile = loadProfile(process.env);
  const request = client(profile);

  const workspaces = (await request("GET", "/api/workspaces")) ?? [];
  const workspace = workspaces.find((ws) => ws.id === profile.workspaceId);
  if (!workspace) fail(`the profile's workspace ${profile.workspaceId} is not visible to its token.`);
  const context = { request, profile, appUrl: profile.appUrl, slug: workspace.slug, queued, app, member, guestEmail };

  if (kinds.some((kind) => !RUNTIMELESS_KINDS.includes(kind))) {
    context.runtime = await ensureRuntime(request, profile.workspaceId);
  }
  if (kinds.includes("chat") || kinds.includes("app-request")) {
    context.agent = await ensureAgent(request, context.runtime, FIXTURE_AGENT);
  }
  if (kinds.includes("task")) context.worker = await ensureAgent(request, context.runtime, FIXTURE_WORKER);

  const seeders = {
    chat: seedChat,
    builder: seedBuilder,
    issue: seedIssue,
    task: seedTask,
    sidebar: seedSidebar,
    guest: seedGuest,
    "app-request": seedAppRequest,
    "app-resume": seedAppResume,
  };
  const result = {};
  for (const kind of kinds) {
    result[kind] = await seeders[kind](context);
    if (!json) console.log(`[dev-fixtures] ${kind.padEnd(12)}${result[kind].url}`);
    if (!json && result[kind].request) console.log(`[dev-fixtures] ${"".padEnd(12)}${result[kind].request}`);
  }
  if (result.guest && !json) {
    const { guest_email, guest_code, guest_token, inbox_unread_issues } = result.guest;
    console.log(`[dev-fixtures] ${"".padEnd(12)}sign in as ${guest_email} with code ${guest_code}, or set localStorage multica_token:`);
    console.log(`[dev-fixtures] ${"".padEnd(12)}${guest_token}`);
    console.log(`[dev-fixtures] ${"".padEnd(12)}${inbox_unread_issues} unread issues in the guest's Inbox`);
  }
  if (json) console.log(JSON.stringify(result, null, 2));
  if (hold) await holdRuntime(request, context.runtime);
}

main().catch((error) => fail(error?.stack || String(error)));
