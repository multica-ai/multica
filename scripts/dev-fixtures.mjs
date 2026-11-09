#!/usr/bin/env node
// Seed realistic chat, Agent Builder and issue content into a managed dev
// environment, without a model and without a daemon:
//
//   make up C=api,web ARGS="--ephemeral"
//   make env-exec ARGS="-- node scripts/dev-fixtures.mjs all"
//   make env-exec ARGS="-- node scripts/dev-fixtures.mjs chat issue --json"
//
// Replies come from the daemon API, driven with the environment profile's user
// token: the script registers a fixture runtime, then claims, starts and
// completes each task it caused with canned Markdown. That path works inside a
// daemon-managed task, where `make up C=daemon` is refused. Every run adds new
// sessions and issues and then exits; nothing keeps running.

import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

const KINDS = ["chat", "builder", "issue"];
const FIXTURE_DAEMON_ID = "dev-fixtures";
const FIXTURE_AGENT_NAME = "Fixture Agent";

const USAGE = `Usage: node scripts/dev-fixtures.mjs <all | chat | builder | issue>... [--json]

  chat      a chat session: a user message and an assistant reply with a table, code and a long log line
  builder   an Agent Builder session with the same kind of reply and a filled-in draft
  issue     an issue with a wide description and a comment holding a diff
  all       all three
  --json    print the created ids and URLs as JSON instead of text

Run through \`make env-exec ARGS="-- node scripts/dev-fixtures.mjs all"\`.`;

function fail(message) {
  console.error(`[dev-fixtures] ${message}`);
  process.exit(1);
}

function parseCli(argv) {
  const kinds = new Set();
  let json = false;
  for (const arg of argv) {
    if (arg === "-h" || arg === "--help") {
      console.log(USAGE);
      process.exit(0);
    } else if (arg === "--json") json = true;
    else if (arg === "all") KINDS.forEach((kind) => kinds.add(kind));
    else if (KINDS.includes(arg)) kinds.add(arg);
    else fail(`unknown argument ${arg}\n\n${USAGE}`);
  }
  if (kinds.size === 0) fail(`name at least one fixture.\n\n${USAGE}`);
  return { kinds: KINDS.filter((kind) => kinds.has(kind)), json };
}

function loadProfile(env) {
  if (!env.MULTICA_DEV_PROFILE) fail("MULTICA_DEV_PROFILE is not set; run this through `make env-exec`.");
  const home = env.MULTICA_DEV_PROFILES_HOME || join(homedir(), ".multica", "profiles");
  const file = join(home, env.MULTICA_DEV_PROFILE, "config.json");
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
  };
}

function client({ apiUrl, token, workspaceId }) {
  return async function request(method, path, body) {
    const response = await fetch(`${apiUrl}${path}`, {
      method,
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
        "X-Workspace-ID": workspaceId,
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

// -------------------------------------------------------------- fixtures ---

async function ensureRuntime(request, workspaceId) {
  const registered = await request("POST", "/api/daemon/register", {
    workspace_id: workspaceId,
    daemon_id: FIXTURE_DAEMON_ID,
    device_name: "Dev fixtures",
    cli_version: "dev-fixtures",
    runtimes: [{ name: "Fixture runtime", type: "claude", version: "fixture", status: "online" }],
  });
  const runtime = registered?.runtimes?.[0];
  if (!runtime?.id) throw new Error(`runtime registration returned no runtime: ${JSON.stringify(registered).slice(0, 300)}`);
  return runtime;
}

async function ensureAgent(request, runtime) {
  const agents = (await request("GET", "/api/agents")) ?? [];
  const existing = (Array.isArray(agents) ? agents : agents.agents ?? []).find(
    (agent) => agent.name === FIXTURE_AGENT_NAME && agent.runtime_id === runtime.id,
  );
  if (existing) return existing;
  return request("POST", "/api/agents", {
    name: FIXTURE_AGENT_NAME,
    description: "Answers with canned Markdown from scripts/dev-fixtures.mjs.",
    instructions: "Fixture agent for local UI evidence. Its replies are written by scripts/dev-fixtures.mjs.",
    runtime_id: runtime.id,
    visibility: "workspace",
  });
}

// A send enqueues the task; the fixture runtime is the only claimer, so the
// expected task arrives within a few polls. Anything else it holds is left
// over from an interrupted run and is answered too, so it cannot block ours.
async function answer(request, runtime, taskId, output) {
  const deadline = Date.now() + 30_000;
  for (;;) {
    const claimed = await request("POST", `/api/daemon/runtimes/${runtime.id}/tasks/claim`, {});
    const task = claimed?.task;
    if (task) {
      await request("POST", `/api/daemon/tasks/${task.id}/start`);
      await request("POST", `/api/daemon/tasks/${task.id}/complete`, { output });
      if (task.id === taskId) return;
      continue;
    }
    if (Date.now() > deadline) throw new Error(`task ${taskId} was never offered to the fixture runtime`);
    await sleep(500);
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
  });
  await request("POST", `/api/issues/${issue.id}/comments`, { content: ISSUE_COMMENT });
  return { issue_id: issue.id, identifier: issue.identifier, url: `${appUrl}/${slug}/issues/${issue.identifier}` };
}

async function main() {
  const { kinds, json } = parseCli(process.argv.slice(2));
  const profile = loadProfile(process.env);
  const request = client(profile);

  const workspaces = (await request("GET", "/api/workspaces")) ?? [];
  const workspace = workspaces.find((ws) => ws.id === profile.workspaceId);
  if (!workspace) fail(`the profile's workspace ${profile.workspaceId} is not visible to its token.`);
  const context = { request, appUrl: profile.appUrl, slug: workspace.slug };

  if (kinds.includes("chat") || kinds.includes("builder")) {
    context.runtime = await ensureRuntime(request, profile.workspaceId);
  }
  if (kinds.includes("chat")) context.agent = await ensureAgent(request, context.runtime);

  const seeders = { chat: seedChat, builder: seedBuilder, issue: seedIssue };
  const result = {};
  for (const kind of kinds) {
    result[kind] = await seeders[kind](context);
    if (!json) console.log(`[dev-fixtures] ${kind.padEnd(8)}${result[kind].url}`);
  }
  if (json) console.log(JSON.stringify(result, null, 2));
}

main().catch((error) => fail(error?.stack || String(error)));
