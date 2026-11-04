// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { ChatSession } from "@multica/core/types";
import { localV2SnapshotSchema } from "@multica/core/local-conversation/v2";
import type { AgentActivityTurn } from "@tutti-os/agent-activity-core";
import { enrichProjectedTurnsWithCanonicalErrors } from "../../../../vendor/tutti/packages/agent/gui/shared/workspaceAgentTurnErrorProjection";
import type { WorkspaceAgentSessionDetailTurn } from "../../../../vendor/tutti/packages/agent/gui/shared/workspaceAgentSessionDetailViewModel";
import type { AgentGuiSessionDetail } from "../protocol";
import { RuntimeLedger } from "./ledger";
import { projectLocalMetadata, projectV2Detail } from "./projection";
import { V2Conversation } from "@multica/core/local-conversation/conversation";

const at = "2026-10-29T12:00:00Z";
const reason = "Authentication failed: please sign in again.";
const code = "process_exited";
const session = {
  id: "conversation", workspace_id: "workspace", agent_id: "local:claude",
  creator_id: "member", title: "Example", status: "active", created_at: at, updated_at: at,
} as ChatSession;

function visibleError(turns: AgentActivityTurn[]) {
  const projected = new Map<string, WorkspaceAgentSessionDetailTurn>();
  enrichProjectedTurnsWithCanonicalErrors({ turns: projected, latestTurnId: "turn", sessionTurns: turns, provider: "claude", agentSessionId: session.id });
  return projected.get("turn")?.agentMessages[0]?.visibleError;
}

describe("local failure detail disclosure", () => {
  it.each(["events", "snapshot"])("makes v2 failures expandable from %s", (source) => {
    const snapshot = localV2SnapshotSchema.parse({
      version: 2, store_generation: "generation", snapshot_seq: source === "events" ? 0 : 2,
      conversation: { conversation_id: session.id, summary_seq: 1, provider: "claude", title: "", status: "idle", pinned: false, archived: false, updated_at: at },
      turns: source === "events" ? [] : [{ id: "turn", turn_index: 0, request_id: "request", status: "failed", started_at: at, completed_at: at, error: { kind: code, message: reason }, messages: [], events: [] }],
    });
    const conversation = V2Conversation.fromSnapshot(snapshot);
    if (source === "events") {
      expect(conversation.apply([
        { seq: 1, turn_id: "turn", turn_seq: 1, item_id: null, type: "turn.started", final: false, created_at: at, payload: { request_id: "request" } },
        { seq: 2, turn_id: "turn", turn_seq: 2, item_id: null, type: "turn.completed", final: true, created_at: at, payload: { status: "failed", error: { kind: code, message: reason } } },
      ])).toBe("applied");
    }
    const detail: AgentGuiSessionDetail = { session, messages: [], pending: {} };
    const project = () => projectV2Detail(detail, conversation, undefined, new RuntimeLedger());
    expect(project().turns[0]?.error).toEqual({ message: reason, code, detail: reason });
    expect(visibleError(project().turns)).toMatchObject({ detail: reason, detailAvailable: true });

    const longReason = `  ${"Diagnostic\n".repeat(500)}  `;
    conversation.turns.get("turn")!.error!.message = longReason;
    expect(visibleError(project().turns)).toMatchObject({ detail: longReason, detailAvailable: true });
    for (const message of ["", " \n\t "]) {
      conversation.turns.get("turn")!.error!.message = message;
      expect(project().turns[0]?.error?.detail).toBeUndefined();
      expect(visibleError(project().turns)).toBeUndefined();
    }
  });
});

describe("a conversation the source no longer holds", () => {
  it("keeps its metadata and fork lineage without inventing turns", () => {
    const detail: AgentGuiSessionDetail = { session, messages: [], pending: {}, forkedFrom: { sessionId: "source", turnId: "turn", targetTurnId: "copy", operationId: "fork", createdAt: at } };
    const view = projectLocalMetadata(detail, undefined, new RuntimeLedger());
    expect(view).toMatchObject({ turns: [], messages: [], session: { agentSessionId: session.id, activeTurnId: null, forkedFrom: { sourceAgentSessionId: "source", sourceTurnId: "turn", targetTurnId: "copy" } } });
  });
});
