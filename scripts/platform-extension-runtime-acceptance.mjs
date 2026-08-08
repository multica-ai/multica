#!/usr/bin/env node

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { homedir } from "node:os";
import { basename, dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import pg from "pg";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const repositoryRoot = resolve(scriptDir, "..");

function requiredString(value, label) {
  assert.equal(typeof value, "string", `${label} must be a string`);
  assert.notEqual(value.trim(), "", `${label} must not be empty`);
  return value;
}

function expectedContract(source) {
  const flowCommand = source.commands.find((command) => command.name.endsWith(".flow"));
  const runtimeCommand = source.commands.find((command) => !command.name.endsWith(".flow"));
  const member = source.agents.find((agent) => agent.key !== source.leader);
  assert.ok(flowCommand, "fixture must contain one flow command");
  assert.ok(runtimeCommand, "fixture must contain one runtime command");
  assert.ok(member, "fixture must contain one non-leader agent");
  return {
    extensionKey: source.extension.key,
    extensionVersion: source.extension.version,
    leaderKey: source.leader,
    memberKey: member.key,
    flowCommandName: flowCommand.name,
    flowCommandContent: flowCommand.content,
    runtimeCommandName: runtimeCommand.name,
    runtimeCommandContent: runtimeCommand.content,
    inputMarker: process.env.TASK11_INPUT_MARKER ?? "TASK11-LIVE-INPUT",
  };
}

export function assertImportMapping(mapping, expected) {
  assert.equal(mapping.release?.extension_key, expected.extensionKey, "release extension key");
  assert.equal(mapping.release?.version, expected.extensionVersion, "release version");
  assert.match(requiredString(mapping.release?.id, "release id"), /^[0-9a-f-]{36}$/i);
  assert.match(requiredString(mapping.release?.digest, "release digest"), /^sha256:[0-9a-f]{64}$/);
  assert.equal(mapping.runtime?.provider, "platform-agent-cli", "allocated runtime provider");
  requiredString(mapping.runtime?.id, "runtime id");
  requiredString(mapping.squad?.id, "squad id");
  assert.equal(mapping.agents?.length, 2, "import must create exactly 2 agents");
  assert.equal(mapping.skills?.length, 2, "import must create exactly 2 skills");

  const leaders = mapping.agents.filter((agent) => agent.leader === true);
  assert.equal(leaders.length, 1, "mapping must expose exactly one leader");
  assert.equal(leaders[0].source_key, expected.leaderKey, "mapped leader source key");
  assert.deepEqual(
    new Set(mapping.agents.map((agent) => agent.source_key)),
    new Set([expected.leaderKey, expected.memberKey]),
    "mapped source agents",
  );
  assert.equal(new Set(mapping.agents.map((agent) => agent.id)).size, 2, "native agent ids");
  assert.equal(new Set(mapping.skills.map((skill) => skill.id)).size, 2, "native skill ids");
}

export function assertImportDisposition(result, expectation = "either") {
  assert.ok([200, 201].includes(result.status), "import status must be 200 or 201");
  if (expectation === "fresh") {
    assert.equal(result.status, 201, "fresh acceptance import status");
    assert.equal(result.payload?.idempotent, false, "fresh acceptance import flag");
    return;
  }
  if (expectation === "idempotent") {
    assert.equal(result.status, 200, "idempotent acceptance import status");
    assert.equal(result.payload?.idempotent, true, "idempotent acceptance import flag");
    return;
  }
  assert.equal(expectation, "either", "import expectation");
}

export function resolveCLIPath(profile, explicitPath = "") {
  if (typeof explicitPath === "string" && explicitPath.trim() !== "") {
    return explicitPath;
  }
  const overrides = profile?.profile_command_overrides;
  if (!overrides || typeof overrides !== "object" || Array.isArray(overrides)) {
    return "resolved by Desktop daemon";
  }
  const candidates = Object.values(overrides).filter(
    (value) =>
      typeof value === "string" &&
      ["platform-agent-cli", "platform-agent-cli.exe"].includes(basename(value).toLowerCase()),
  );
  return candidates.length === 1 ? candidates[0] : "resolved by Desktop daemon";
}

export function assertNativeSnapshot(snapshot, mapping, expected) {
  assert.equal(snapshot.releaseCount, 1, "one immutable Release row");
  assert.equal(snapshot.runtime?.id, mapping.runtime.id, "snapshot runtime id");
  assert.equal(snapshot.runtime?.provider, "platform-agent-cli", "runtime provider");
  assert.equal(snapshot.runtime?.status, "online", "runtime status");
  if (snapshot.runtime.metadata) {
    assert.equal(snapshot.runtime.metadata.version, "platform-agent-cli 0.2.0", "runtime CLI version");
    assert.equal(snapshot.runtime.metadata.launched_by, "desktop", "Desktop-owned daemon registration");
  }

  assert.equal(snapshot.agents.length, 2, "native Agent rows");
  assert.equal(snapshot.skills.length, 2, "native Skill rows");
  assert.equal(snapshot.bindingCount, 4, "2 Agent x 2 Skill bindings");
  assert.equal(snapshot.skillFiles.length, 1, "one Skill support file in addition to two root SKILL.md files");
  for (const skill of snapshot.skills) {
    assert.ok(skill.content.includes("name:"), `root SKILL.md content for ${skill.id}`);
  }

  const mappedAgentIDs = new Set(mapping.agents.map((agent) => agent.id));
  const sourceKeys = new Set();
  for (const agent of snapshot.agents) {
    assert.ok(mappedAgentIDs.has(agent.id), `unexpected native Agent ${agent.id}`);
    assert.equal(agent.runtime_id, mapping.runtime.id, `Agent ${agent.id} fixed runtime`);
    const context = agent.runtime_config?.platform_agent;
    assert.equal(context?.schema_version, "platform-agent.runtime-context/v1", "sidecar schema source");
    assert.equal(context?.extension?.key, expected.extensionKey, "sidecar extension key source");
    assert.equal(context?.extension?.version, expected.extensionVersion, "sidecar extension version source");
    assert.equal(context?.extension?.release_id, mapping.release.id, "sidecar release source");
    assert.equal(context?.commands?.length, 1, "ordinary Command sidecar count");
    assert.equal(context.commands[0].name, expected.runtimeCommandName, "ordinary Command name");
    assert.equal(context.commands[0].content, expected.runtimeCommandContent, "ordinary Command content");
    assert.notEqual(context.commands[0].name, expected.flowCommandName, "flow Command excluded from sidecar");
    sourceKeys.add(context?.agent?.source_key);
    assert.ok(!agent.instructions.includes(expected.runtimeCommandContent), "ordinary Command excluded from Agent prompt");
  }
  assert.deepEqual(sourceKeys, new Set([expected.leaderKey, expected.memberKey]), "sidecar Agent identities");

  assert.equal(snapshot.squad.id, mapping.squad.id, "native Squad id");
  const leaderID = mapping.agents.find((agent) => agent.source_key === expected.leaderKey)?.id;
  assert.equal(snapshot.squad.leader_id, leaderID, "native Squad leader");
  assert.ok(snapshot.squad.instructions.includes(expected.flowCommandName), "flow Command name in Squad Instructions");
  assert.ok(snapshot.squad.instructions.includes(expected.flowCommandContent), "flow Command content in Squad Instructions");
  assert.ok(!snapshot.squad.instructions.includes(expected.runtimeCommandContent), "ordinary Command excluded from Squad Instructions");
  assert.equal(snapshot.members.length, 2, "native Squad membership count");
  assert.deepEqual(
    snapshot.members.map((member) => member.role).sort(),
    ["leader", "member"],
    "native Squad roles",
  );
  assert.ok(snapshot.members.every((member) => member.member_type === "agent"), "Squad members are Agents");
}

export function completedTaskOutput(tasks) {
  assert.ok(Array.isArray(tasks) && tasks.length > 0, "issue must have at least one real task");
  const task = [...tasks].sort((left, right) => left.created_at?.localeCompare(right.created_at ?? "") ?? 0).at(-1);
  if (task.status !== "completed") {
    throw new Error(`task ${task.id} reached terminal status ${task.status}: ${task.error ?? "no error"}`);
  }
  return requiredString(task.result?.output, "completed task result.output");
}

export function assertTaskOutput(output, expected, issueID) {
  const value = requiredString(output, "task output");
  for (const required of [
    `extension=${expected.extensionKey}@${expected.extensionVersion}`,
    `agent=${expected.leaderKey}`,
    "skills=2",
    "commands=1",
  ]) {
    assert.ok(value.includes(required), `task output must include ${required}`);
  }
  const inputPrefix = " input=";
  const inputOffset = value.indexOf(inputPrefix);
  assert.notEqual(inputOffset, -1, "task output must include input=");
  const runtimeInput = requiredString(value.slice(inputOffset + inputPrefix.length), "task runtime input");
  assert.ok(runtimeInput.includes(requiredString(issueID, "issue id")), "task runtime input must include dynamic Issue ID");
}

async function requestJSON(apiBase, profile, path, init = {}) {
  const headers = {
    Accept: "application/json",
    Authorization: `Bearer ${profile.token}`,
    "X-Workspace-ID": profile.workspace_id,
    ...(init.body === undefined ? {} : { "Content-Type": "application/json" }),
    ...(init.headers ?? {}),
  };
  const response = await fetch(`${apiBase}${path}`, { ...init, headers });
  const text = await response.text();
  let payload;
  try {
    payload = text === "" ? null : JSON.parse(text);
  } catch {
    throw new Error(`${init.method ?? "GET"} ${path} returned non-JSON ${response.status}: ${text}`);
  }
  if (!response.ok) {
    throw new Error(`${init.method ?? "GET"} ${path} returned ${response.status}: ${JSON.stringify(payload)}`);
  }
  return { status: response.status, payload };
}

async function loadNativeSnapshot(client, profile, mapping, expected) {
  const agentIDs = mapping.agents.map((agent) => agent.id);
  const skillIDs = mapping.skills.map((skill) => skill.id);
  // node-postgres clients execute one query at a time. Keep these evidence reads
  // explicitly ordered so a future node-postgres release cannot reject concurrent
  // query calls on the same connection.
  const release = await client.query(
    `SELECT count(*)::int AS count
     FROM platform_extension_release
     WHERE workspace_id = $1 AND extension_key = $2 AND version = $3`,
    [profile.workspace_id, expected.extensionKey, expected.extensionVersion],
  );
  const runtime = await client.query(
    `SELECT id::text, daemon_id::text, provider, status, name, last_seen_at, metadata
     FROM agent_runtime WHERE workspace_id = $1 AND id = $2`,
    [profile.workspace_id, mapping.runtime.id],
  );
  const agents = await client.query(
    `SELECT id::text, runtime_id::text, runtime_config, instructions, name
     FROM agent WHERE workspace_id = $1 AND id = ANY($2::uuid[]) ORDER BY id`,
    [profile.workspace_id, agentIDs],
  );
  const skills = await client.query(
    `SELECT id::text, name, content, config
     FROM skill WHERE workspace_id = $1 AND id = ANY($2::uuid[]) ORDER BY id`,
    [profile.workspace_id, skillIDs],
  );
  const bindings = await client.query(
    `SELECT count(*)::int AS count
     FROM agent_skill WHERE agent_id = ANY($1::uuid[]) AND skill_id = ANY($2::uuid[])`,
    [agentIDs, skillIDs],
  );
  const skillFiles = await client.query(
    `SELECT skill_id::text, path, content
     FROM skill_file WHERE skill_id = ANY($1::uuid[]) ORDER BY skill_id, path`,
    [skillIDs],
  );
  const squad = await client.query(
    `SELECT id::text, leader_id::text, name, instructions
     FROM squad WHERE workspace_id = $1 AND id = $2`,
    [profile.workspace_id, mapping.squad.id],
  );
  const members = await client.query(
    `SELECT member_id::text, member_type, role
     FROM squad_member WHERE squad_id = $1 ORDER BY role, member_id`,
    [mapping.squad.id],
  );
  return {
    releaseCount: release.rows[0]?.count,
    runtime: runtime.rows[0],
    agents: agents.rows,
    skills: skills.rows,
    bindingCount: bindings.rows[0]?.count,
    skillFiles: skillFiles.rows,
    squad: squad.rows[0],
    members: members.rows,
  };
}

async function waitForCompletedTask(apiBase, profile, issueID, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let lastTasks = [];
  while (Date.now() < deadline) {
    lastTasks = (await requestJSON(apiBase, profile, `/api/issues/${issueID}/task-runs`)).payload;
    const terminal = lastTasks.find((task) => ["completed", "failed", "cancelled"].includes(task.status));
    if (terminal) return lastTasks;
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 500));
  }
  throw new Error(`timed out after ${timeoutMs}ms waiting for daemon task; last=${JSON.stringify(lastTasks)}`);
}

async function main() {
  const fixturePath = resolve(
    process.env.TASK11_EXTENSION_FIXTURE ??
      resolve(repositoryRoot, "testdata/extensions/task11-live-squad.source.json"),
  );
  const profilePath = resolve(
    process.env.TASK11_PROFILE_CONFIG ??
      resolve(homedir(), ".multica/profiles/desktop-localhost-8080/config.json"),
  );
  const apiBase = (process.env.TASK11_API_BASE ?? "http://127.0.0.1:8080").replace(/\/$/, "");
  const databaseURL = requiredString(process.env.DATABASE_URL, "DATABASE_URL");
  const timeoutMs = Number.parseInt(process.env.TASK11_TIMEOUT_MS ?? "60000", 10);
  assert.ok(Number.isSafeInteger(timeoutMs) && timeoutMs > 0, "TASK11_TIMEOUT_MS must be positive");

  const [fixtureBytes, profileBytes] = await Promise.all([readFile(fixturePath), readFile(profilePath)]);
  const source = JSON.parse(fixtureBytes.toString("utf8"));
  const profile = JSON.parse(profileBytes.toString("utf8"));
  requiredString(profile.token, "Desktop profile token");
  requiredString(profile.workspace_id, "Desktop profile workspace_id");
  const expected = expectedContract(source);

  const runtimes = (await requestJSON(apiBase, profile, "/api/runtimes")).payload;
  const eligibleRuntime = runtimes.find(
    (runtime) => runtime.provider === "platform-agent-cli" && runtime.status === "online",
  );
  assert.ok(eligibleRuntime, "an online platform-agent-cli Runtime must exist before import");

  const importResult = await requestJSON(apiBase, profile, "/api/extensions/import", {
    method: "POST",
    body: fixtureBytes,
  });
  assert.notEqual(
    process.env.TASK11_EXPECT_FRESH === "1" && process.env.TASK11_EXPECT_IDEMPOTENT === "1",
    true,
    "fresh and idempotent import expectations are mutually exclusive",
  );
  const importExpectation =
    process.env.TASK11_EXPECT_FRESH === "1"
      ? "fresh"
      : process.env.TASK11_EXPECT_IDEMPOTENT === "1"
        ? "idempotent"
        : "either";
  assertImportDisposition(importResult, importExpectation);
  const mapping = importResult.payload;
  assertImportMapping(mapping, expected);
  assert.equal(mapping.runtime.id, eligibleRuntime.id, "allocator selected the observed idle Runtime");

  const detail = (await requestJSON(apiBase, profile, `/api/extensions/${mapping.release.id}`)).payload;
  assertImportMapping(detail, expected);
  assert.equal(detail.manifest?.schema_version, "multica.extension-bundle/v1", "persisted canonical Bundle");
  assert.equal(detail.manifest?.flow_commands?.length, 1, "one flow Command in Bundle");
  assert.equal(detail.manifest?.runtime_commands?.length, 1, "one runtime Command in Bundle");

  const client = new pg.Client({ connectionString: databaseURL });
  await client.connect();
  let snapshot;
  try {
    snapshot = await loadNativeSnapshot(client, profile, mapping, expected);
  } finally {
    await client.end();
  }
  assertNativeSnapshot(snapshot, mapping, expected);

  const input = `${expected.inputMarker}: execute the imported Leader with both skills and the ordinary command context.`;
  const issueResult = await requestJSON(apiBase, profile, "/api/issues", {
    method: "POST",
    body: JSON.stringify({
      title: input,
      description: "This is the dedicated real Platform Agent CLI daemon acceptance run.",
      status: "todo",
      priority: "high",
      assignee_type: "agent",
      assignee_id: mapping.agents.find((agent) => agent.leader === true).id,
      allow_duplicate: true,
    }),
  });
  assert.equal(issueResult.status, 201, "real Leader issue create status");
  const issue = issueResult.payload;

  const tasks = await waitForCompletedTask(apiBase, profile, issue.id, timeoutMs);
  const task = tasks.find((candidate) => candidate.status === "completed") ?? tasks.at(-1);
  const output = completedTaskOutput(tasks);
  assert.equal(task.agent_id, mapping.agents.find((agent) => agent.leader === true).id, "task Leader agent");
  assert.equal(task.runtime_id, mapping.runtime.id, "task fixed runtime");
  assertTaskOutput(output, expected, issue.id);

  const messages = (await requestJSON(apiBase, profile, `/api/tasks/${task.id}/messages`)).payload;
  assert.ok(
    messages.some((message) => message.content?.includes(" input=") && message.content.includes(issue.id)),
    "persisted task message must contain the dynamic runtime input Issue ID",
  );

  const evidence = {
    accepted_at: new Date().toISOString(),
    fixture: fixturePath,
    cli: {
      path: resolveCLIPath(profile, process.env.MULTICA_PLATFORM_AGENT_CLI_PATH),
      version: snapshot.runtime.metadata?.version,
    },
    daemon: {
      id: snapshot.runtime.daemon_id,
      launched_by: snapshot.runtime.metadata?.launched_by,
    },
    workspace_id: profile.workspace_id,
    release: mapping.release,
    runtime: mapping.runtime,
    squad: {
      ...mapping.squad,
      leader_id: snapshot.squad.leader_id,
      members: snapshot.members,
    },
    agents: mapping.agents,
    skills: mapping.skills,
    binding_count: snapshot.bindingCount,
    issue: { id: issue.id, identifier: issue.identifier, title: issue.title },
    task: {
      id: task.id,
      status: task.status,
      agent_id: task.agent_id,
      runtime_id: task.runtime_id,
      work_dir: task.work_dir,
      output,
      message_count: messages.length,
    },
  };
  process.stdout.write(`${JSON.stringify(evidence, null, 2)}\n`);
}

const invokedPath = process.argv[1] ? pathToFileURL(resolve(process.argv[1])).href : "";
if (import.meta.url === invokedPath) {
  main().catch((error) => {
    console.error(error instanceof Error ? error.stack : error);
    process.exitCode = 1;
  });
}
