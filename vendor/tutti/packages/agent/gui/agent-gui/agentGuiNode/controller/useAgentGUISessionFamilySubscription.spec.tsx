import { act, renderHook } from "@testing-library/react";
import {
  createAgentSessionFamilySnapshotSelector,
  normalizeAgentActivitySession
} from "@tutti-os/agent-activity-core";
import { describe, expect, it } from "vitest";
import { useEngineSelector } from "../../../shared/engine/useEngineSelector";
import { createTestAgentSessionEngine } from "../../../shared/testing/createTestAgentSessionEngine";

describe("AgentGUI Session-family subscription", () => {
  it("does not render an unrelated AgentGUI when another Session changes", () => {
    const engine = createTestAgentSessionEngine("workspace-1");
    engine.dispatch({
      sessions: [
        normalizeAgentActivitySession(session("session-a")),
        normalizeAgentActivitySession(session("session-b"))
      ],
      type: "session/snapshotReceived"
    });
    const selectA = createAgentSessionFamilySnapshotSelector("session-a");
    const selectB = createAgentSessionFamilySnapshotSelector("session-b");
    let rendersA = 0;
    let rendersB = 0;

    renderHook(() => {
      rendersA += 1;
      return useEngineSelector(engine, selectA);
    });
    renderHook(() => {
      rendersB += 1;
      return useEngineSelector(engine, selectB);
    });
    const initialRendersA = rendersA;
    const initialRendersB = rendersB;

    act(() => {
      engine.dispatch({
        session: normalizeAgentActivitySession({ ...session("session-a"), title: "renamed" }),
        type: "session/upserted"
      });
    });

    expect(rendersA).toBeGreaterThan(initialRendersA);
    expect(rendersB).toBe(initialRendersB);
  });
});

function session(agentSessionId: string) {
  return {
    activeTurnId: null,
    agentSessionId,
    cwd: "/workspace",
    latestTurnInteractions: [],
    pendingInteractions: [],
    provider: "codex",
    title: agentSessionId,
    workspaceId: "workspace-1"
  };
}
