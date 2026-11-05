import { act, renderHook } from "@testing-library/react";
import { normalizeAgentActivitySession } from "@tutti-os/agent-activity-core";
import { describe, expect, it } from "vitest";
import { createTestRuntimeState } from "../../../shared/testing/createTestRuntimeState";
import { useModelValue } from "../../../shared/useModelValue";
import type { AgentConversationServices } from "../../../state/conversation-services";

describe("AgentGUI Session-family subscription", () => {
  it("does not render an unrelated AgentGUI when another Session changes", () => {
    const engine = createTestRuntimeState("workspace-1");
    engine.sessions.upsertSessions([
      normalizeAgentActivitySession(session("session-a")),
      normalizeAgentActivitySession(session("session-b"))
    ]);
    const selectA = (services: AgentConversationServices) =>
      services.sessions.family("session-a");
    const selectB = (services: AgentConversationServices) =>
      services.sessions.family("session-b");
    let rendersA = 0;
    let rendersB = 0;

    renderHook(() => {
      rendersA += 1;
      return useModelValue(() => selectA(engine), [engine, selectA]);
    });
    renderHook(() => {
      rendersB += 1;
      return useModelValue(() => selectB(engine), [engine, selectB]);
    });
    const initialRendersA = rendersA;
    const initialRendersB = rendersB;

    act(() => {
      engine.sessions.upsertSession(
        normalizeAgentActivitySession({
          ...session("session-a"),
          title: "renamed"
        })
      );
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
