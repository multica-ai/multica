import {
  normalizeAgentActivitySession,
  type AgentActivitySessionCapabilities,
  type AgentActivityTurn
} from "@tutti-os/agent-activity-core";
import { describe, expect, it } from "vitest";
import { createTestAgentSessionEngine } from "../testing/createTestAgentSessionEngine";
import {
  consumerAwaitingPlanImplementation,
  selectRootAgentSessionIdsAwaitingPlanImplementation
} from "./planImplementationAwaiting";

describe("planImplementationAwaiting", () => {
  it("matches a settled plan turn that still needs implementation confirmation", () => {
    expect(
      consumerAwaitingPlanImplementation({
        capabilities: planCapabilities(),
        latestTurn: settledPlanTurn("turn-plan"),
        messages: [
          {
            turnId: "turn-plan",
            occurredAtUnixMs: 20,
            payload: { messageKind: "plan" }
          }
        ]
      })
    ).toBe(true);
  });

  it("fails closed for dismissals, missing plan messages, or explicit capability denial", () => {
    const messages = [
      {
        turnId: "turn-plan",
        occurredAtUnixMs: 20,
        payload: { messageKind: "plan" }
      }
    ];
    expect(
      consumerAwaitingPlanImplementation({
        capabilities: planCapabilities({ planImplementation: false }),
        latestTurn: settledPlanTurn("turn-plan"),
        messages
      })
    ).toBe(false);
    expect(
      consumerAwaitingPlanImplementation({
        capabilities: planCapabilities({ planMode: false }),
        latestTurn: settledPlanTurn("turn-plan"),
        messages
      })
    ).toBe(false);
    expect(
      consumerAwaitingPlanImplementation({
        capabilities: planCapabilities(),
        latestTurn: settledPlanTurn("turn-plan"),
        messages: [
          {
            turnId: "turn-plan",
            occurredAtUnixMs: 20,
            payload: { messageKind: "text" }
          }
        ]
      })
    ).toBe(false);
  });

  it("keeps waiting when session capabilities are not projected yet", () => {
    expect(
      consumerAwaitingPlanImplementation({
        capabilities: null,
        latestTurn: settledPlanTurn("turn-plan"),
        messages: [
          {
            turnId: "turn-plan",
            occurredAtUnixMs: 20,
            payload: { messageKind: "plan" }
          }
        ]
      })
    ).toBe(true);
  });

  it("selects every root session awaiting plan implementation from engine state", () => {
    const engine = createTestAgentSessionEngine("workspace-1");
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [
        normalizeAgentActivitySession({
          activeTurnId: null,
          agentSessionId: "plan-session",
          capabilities: planCapabilities(),
          cwd: "/workspace",
          latestTurn: settledPlanTurn("turn-plan", "plan-session"),
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "codex",
          title: "Plan session",
          workspaceId: "workspace-1"
        }),
        normalizeAgentActivitySession({
          activeTurnId: null,
          agentSessionId: "idle-session",
          capabilities: planCapabilities(),
          cwd: "/workspace",
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "codex",
          title: "Idle session",
          workspaceId: "workspace-1"
        })
      ]
    });
    engine.dispatch({
      type: "message/snapshotReceived",
      messages: [
        {
          agentSessionId: "plan-session",
          kind: "message",
          messageId: "plan-message",
          occurredAtUnixMs: 20,
          payload: { messageKind: "plan" },
          role: "agent",
          turnId: "turn-plan",
          version: 1,
          workspaceId: "workspace-1"
        }
      ]
    });

    expect(
      selectRootAgentSessionIdsAwaitingPlanImplementation(engine.getSnapshot())
    ).toEqual(["plan-session"]);
  });
});

function settledPlanTurn(
  turnId: string,
  agentSessionId = "session-1"
): AgentActivityTurn {
  return {
    turnId,
    agentSessionId,
    origin: "user_prompt",
    phase: "settled",
    outcome: "completed",
    startedAtUnixMs: 10,
    settledAtUnixMs: 20,
    updatedAtUnixMs: 20
  };
}

function planCapabilities(
  overrides: Partial<AgentActivitySessionCapabilities> = {}
): AgentActivitySessionCapabilities {
  return {
    imageInput: false,
    modelImageInputRequired: false,
    modelPlanBinding: false,
    modelSwitch: false,
    skills: false,
    compact: false,
    tokenUsage: false,
    rateLimits: false,
    planMode: true,
    interrupt: false,
    activeTurnGuidance: false,
    browserUse: false,
    computerUse: false,
    goalPause: false,
    planImplementation: true,
    permissionModeChangeDuringTurn: false,
    permissionModeChangeDeferred: false,
    review: false,
    resumeRunningTurn: false,
    ...overrides
  };
}
