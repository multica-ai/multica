import assert from "node:assert/strict";
import test from "node:test";
import { normalizeAgentActivitySession } from "./sessionNormalization.ts";

test("normalizes a transport session into the complete canonical contract", () => {
  const session = normalizeAgentActivitySession({
    ...{
      activeTurnId: null,
      latestTurnInteractions: [],
      pendingInteractions: []
    },
    agentSessionId: "session-1",
    cwd: "/workspace",
    provider: "codex",
    title: "Session",
    updatedAtUnixMs: 42,
    workspaceId: "workspace-1"
  });

  assert.deepEqual(session, {
    activeTurn: null,
    activeTurnId: null,
    agentSessionId: "session-1",
    agentTargetId: null,
    capabilities: null,
    createdAtUnixMs: 0,
    cwd: "/workspace",
    endedAtUnixMs: null,
    forkedFrom: null,
    imported: false,
    isolation: null,
    kind: "root",
    lastEventUnixMs: 42,
    latestTurn: null,
    latestTurnInteractions: [],
    lifecycleCapabilities: {
      fork: false,
      forkThroughTurn: false
    },
    messageVersion: 0,
    pendingInteractions: [],
    parentAgentSessionId: null,
    parentToolCallId: null,
    parentTurnId: null,
    permissionConfig: { configurable: false, modes: [] },
    pinnedAtUnixMs: null,
    provider: "codex",
    providerSessionId: null,
    resumable: false,
    rootAgentSessionId: null,
    rootTurnId: null,
    settings: {},
    startedAtUnixMs: 0,
    title: "Session",
    updatedAtUnixMs: 42,
    usage: null,
    visible: true,
    workspaceId: "workspace-1"
  });
});

test("preserves an independent managed Worktree identity", () => {
  const session = normalizeAgentActivitySession({
    activeTurnId: null,
    agentSessionId: "session-worktree",
    cwd: "/state/worktrees/worktree-1",
    isolation: {
      baseCommit: "base-1",
      branch: "tutti/worktree/worktree-1",
      mode: "worktree",
      worktreeId: " worktree-1 ",
      worktreePath: "/state/worktrees/worktree-1"
    },
    latestTurnInteractions: [],
    pendingInteractions: [],
    provider: "codex",
    title: "Worktree session",
    workspaceId: "workspace-1"
  });

  assert.equal(session.isolation?.worktreeId, "worktree-1");
});
