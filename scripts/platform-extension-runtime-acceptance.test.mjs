import assert from "node:assert/strict";
import test from "node:test";

import {
  assertImportDisposition,
  assertImportMapping,
  assertNativeSnapshot,
  assertTaskOutput,
  completedTaskOutput,
  resolveCLIPath,
} from "./platform-extension-runtime-acceptance.mjs";

const expected = {
  extensionKey: "task11-live-squad",
  extensionVersion: "1.0.0",
  leaderKey: "acceptance-leader",
  memberKey: "acceptance-reviewer",
  flowCommandName: "delegate.flow",
  flowCommandContent: "Delegate independent verification to the reviewer when useful.",
  runtimeCommandName: "summarize",
  runtimeCommandContent: "Return the final verified acceptance summary.",
  inputMarker: "TASK11-LIVE-INPUT",
};
const ids = {
  release: "11111111-1111-4111-8111-111111111111",
  runtime: "22222222-2222-4222-8222-222222222222",
  squad: "33333333-3333-4333-8333-333333333333",
  leader: "44444444-4444-4444-8444-444444444444",
  member: "55555555-5555-4555-8555-555555555555",
  skillOne: "66666666-6666-4666-8666-666666666666",
  skillTwo: "77777777-7777-4777-8777-777777777777",
};
const release = {
  id: ids.release,
  extension_key: expected.extensionKey,
  version: expected.extensionVersion,
  digest: `sha256:${"a".repeat(64)}`,
};

test("acceptance assertions distinguish a fresh import from the required idempotent rerun", () => {
  assertImportDisposition({ status: 201, payload: { idempotent: false } }, "fresh");
  assertImportDisposition({ status: 200, payload: { idempotent: true } }, "idempotent");
  assert.throws(
    () => assertImportDisposition({ status: 201, payload: { idempotent: false } }, "idempotent"),
    /idempotent acceptance import status/,
  );
});

test("CLI evidence path is portable across Desktop profile command keys", () => {
  const profile = {
    profile_command_overrides: {
      "another-install-specific-id": "/opt/multica/platform-agent-cli",
    },
  };
  assert.equal(resolveCLIPath(profile, "/explicit/platform-agent-cli"), "/explicit/platform-agent-cli");
  assert.equal(resolveCLIPath(profile), "/opt/multica/platform-agent-cli");
  assert.equal(
    resolveCLIPath({
      profile_command_overrides: {
        one: "/opt/one/platform-agent-cli",
        two: "/opt/two/platform-agent-cli",
      },
    }),
    "resolved by Desktop daemon",
  );
});

test("acceptance assertions reject a mapping that is not the dedicated 2x2 import", () => {
  assert.throws(
    () =>
      assertImportMapping(
        {
          release,
          runtime: { id: ids.runtime, provider: "platform-agent-cli" },
          squad: { id: ids.squad },
          agents: [{ source_key: expected.leaderKey, id: ids.leader, leader: true }],
          skills: [{ source_key: "evidence-check", id: ids.skillOne }],
        },
        expected,
      ),
    /exactly 2 agents/,
  );
});

test("acceptance assertions validate native all-to-all, leader, instruction, command, and runtime semantics", () => {
  const mapping = {
    release,
    runtime: { id: ids.runtime, provider: "platform-agent-cli" },
    squad: { id: ids.squad },
    agents: [
      { source_key: expected.leaderKey, id: ids.leader, leader: true },
      { source_key: expected.memberKey, id: ids.member, leader: false },
    ],
    skills: [
      { source_key: "evidence-check", id: ids.skillOne },
      { source_key: "result-review", id: ids.skillTwo },
    ],
  };
  assertImportMapping(mapping, expected);
  assertNativeSnapshot(
    {
      releaseCount: 1,
      runtime: { id: ids.runtime, provider: "platform-agent-cli", status: "online" },
      agents: [
        {
          id: ids.leader,
          runtime_id: ids.runtime,
          instructions: "Lead the acceptance task.",
          runtime_config: {
            platform_agent: {
              schema_version: "platform-agent.runtime-context/v1",
              extension: { key: expected.extensionKey, version: expected.extensionVersion, release_id: ids.release },
              agent: { source_key: expected.leaderKey },
              commands: [
                {
                  name: expected.runtimeCommandName,
                  content: expected.runtimeCommandContent,
                  metadata: {},
                },
              ],
            },
          },
        },
        {
          id: ids.member,
          runtime_id: ids.runtime,
          instructions: "Review the acceptance evidence.",
          runtime_config: {
            platform_agent: {
              schema_version: "platform-agent.runtime-context/v1",
              extension: { key: expected.extensionKey, version: expected.extensionVersion, release_id: ids.release },
              agent: { source_key: expected.memberKey },
              commands: [
                {
                  name: expected.runtimeCommandName,
                  content: expected.runtimeCommandContent,
                  metadata: {},
                },
              ],
            },
          },
        },
      ],
      skills: [{ id: ids.skillOne, content: "---\nname: evidence-check\n---" }, { id: ids.skillTwo, content: "---\nname: result-review\n---" }],
      skillFiles: [{ skill_id: ids.skillOne, path: "references/checklist.md" }],
      bindingCount: 4,
      squad: {
        id: ids.squad,
        leader_id: ids.leader,
        instructions: `${expected.flowCommandName}\n${expected.flowCommandContent}`,
      },
      members: [
        { member_id: ids.leader, member_type: "agent", role: "leader" },
        { member_id: ids.member, member_type: "agent", role: "member" },
      ],
    },
    mapping,
    expected,
  );
});

test("completedTaskOutput requires a completed task with persisted output", () => {
  assert.equal(
    completedTaskOutput([
      {
        id: "task-1",
        status: "completed",
        result: { output: `extension=${expected.extensionKey}@${expected.extensionVersion}` },
      },
    ]),
    `extension=${expected.extensionKey}@${expected.extensionVersion}`,
  );
  assert.throws(
    () => completedTaskOutput([{ id: "task-1", status: "failed", error: "boom" }]),
    /terminal status failed/,
  );
});

test("task output proves the dynamic Multica runtime input reached the CLI", () => {
  const issueID = "88888888-8888-4888-8888-888888888888";
  assertTaskOutput(
    `extension=${expected.extensionKey}@${expected.extensionVersion} agent=${expected.leaderKey} skills=2 commands=1 input=Your assigned issue ID is: ${issueID}`,
    expected,
    issueID,
  );
  assert.throws(
    () =>
      assertTaskOutput(
        `extension=${expected.extensionKey}@${expected.extensionVersion} agent=${expected.leaderKey} skills=2 commands=1 input=another task`,
        expected,
        issueID,
      ),
    /dynamic Issue ID/,
  );
});
