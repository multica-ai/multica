// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AgentTask, ChatSession } from "@multica/core/types";
import { localV2SnapshotSchema } from "@multica/core/local-conversation/v2";
import type { AgentActivityTurn } from "@tutti-os/agent-activity-core";
import { enrichProjectedTurnsWithCanonicalErrors } from "../../../../vendor/tutti/packages/agent/gui/shared/workspaceAgentTurnErrorProjection";
import type { WorkspaceAgentSessionDetailTurn } from "../../../../vendor/tutti/packages/agent/gui/shared/workspaceAgentSessionDetailViewModel";
import type { AgentGuiSessionDetail } from "../protocol";
import { RuntimeLedger } from "./ledger";
import { projectDetail, projectV2Detail } from "./projection";
import { V2Conversation } from "./v2-conversation";

const at = "2026-10-29T12:00:00Z";
const reason = "Authentication failed: please sign in again.";
const code = "process_exited";
const session = {
  id: "conversation", workspace_id: "workspace", agent_id: "local:claude",
  creator_id: "member", title: "Example", status: "active", created_at: at, updated_at: at,
} as ChatSession;

function legacyDetail(source: "task" | "message", message: string): AgentGuiSessionDetail {
  return {
    session, pending: {},
    messages: [{ id: "failure", chat_session_id: session.id, task_id: "turn", role: "assistant", content: message, failure_reason: code, created_at: at }],
    tasks: source === "task" ? [{ id: "turn", status: "failed", error: message, failure_reason: code, created_at: at, completed_at: at } as AgentTask] : [],
  };
}

function visibleError(turns: AgentActivityTurn[]) {
  const projected = new Map<string, WorkspaceAgentSessionDetailTurn>();
  enrichProjectedTurnsWithCanonicalErrors({ turns: projected, latestTurnId: "turn", sessionTurns: turns, provider: "claude", agentSessionId: session.id });
  return projected.get("turn")?.agentMessages[0]?.visibleError;
}

describe("local failure detail disclosure", () => {
  it.each(["task", "message"] as const)("makes the %s failure reason available to the error card after hydration", (source) => {
    const detail = legacyDetail(source, reason);
    for (let reload = 0; reload < 2; reload++) {
      const view = projectDetail(detail, undefined, new Map(), new RuntimeLedger());
      expect(view.turns[0]?.error).toMatchObject({ message: reason, code, detail: reason });
      expect(visibleError(view.turns)).toMatchObject({ detail: reason, detailAvailable: true });
    }
    expect(detail.messages[0]?.content).toBe(reason);
  });

  it.each(["task", "message"] as const)("preserves the cloud %s disclosure boundary", (source) => {
    const detail = legacyDetail(source, reason);
    detail.session = { ...session, agent_id: "cloud-agent" };
    const view = projectDetail(detail, undefined, new Map(), new RuntimeLedger());
    expect(view.turns[0]?.error).toEqual({ message: reason, code });
    expect(visibleError(view.turns)).not.toHaveProperty("detailAvailable");
  });

  it.each(["task", "message"] as const)("keeps long %s diagnostics intact in expandable details", (source) => {
    const message = `  ${"Long diagnostic line\n".repeat(500)}  `;
    const view = projectDetail(legacyDetail(source, message), undefined, new Map(), new RuntimeLedger());
    expect(visibleError(view.turns)).toMatchObject({ detail: message, detailAvailable: true });
  });

  it.each(["task", "message"] as const)("does not disclose blank %s diagnostics", (source) => {
    for (const message of ["", " \n\t "]) {
      const view = projectDetail(legacyDetail(source, message), undefined, new Map(), new RuntimeLedger());
      expect(view.turns[0]?.error?.detail).toBeUndefined();
      expect(visibleError(view.turns)).toBeUndefined();
    }
  });

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
