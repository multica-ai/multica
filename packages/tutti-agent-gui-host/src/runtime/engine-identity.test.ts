// @vitest-environment node
import { describe, expect, it } from "vitest";
import { createAgentSessionEngine, normalizeAgentActivitySession, selectSessionMessages } from "@tutti-os/agent-activity-core";

const message = (id: string, version: number, content: string) => ({ workspaceId: "w", agentSessionId: "s", messageId: id, turnId: "t", role: "assistant", kind: "text", version, sequence: version, occurredAtUnixMs: version, payload: { content } });

describe("the engine's copy of a conversation's messages", () => {
  it("keeps the objects it was given, so what is built from them can be kept", () => {
    const engine = createAgentSessionEngine({ identity: { workspaceId: "w", origin: "multica" }, clock: { nowUnixMs: () => 1 }, scheduler: { schedule: () => ({ cancel: () => {} }) }, commandPort: {} } as never);
    const session = normalizeAgentActivitySession({ workspaceId: "w", agentSessionId: "s", provider: "claude", title: "c", cwd: "", activeTurnId: null, activeTurn: null, latestTurnInteractions: [], pendingInteractions: [] } as never);
    const commit = (messages: unknown[], historyRevision: number) => engine.dispatch({ type: "session/historyAuthoritativeSnapshotReceived", workspaceId: "w", agentSessionId: "s", childSessions: [], historyRevision, session, turns: [], messages } as never);
    const a = message("a", 1, "a"), b = message("b", 2, "b");
    commit([a, b], 1);
    const stored = selectSessionMessages(engine.getSnapshot(), "s");
    expect(stored[0]).toBe(a);
    expect(stored[1]).toBe(b);
    const longer = message("b", 3, "bb");
    commit([a, longer], 2);
    const next = selectSessionMessages(engine.getSnapshot(), "s");
    expect(next[0]).toBe(a);
    expect(next[1]).toBe(longer);
    expect(next[1]!.payload).toEqual({ content: "bb" });
  });
});
