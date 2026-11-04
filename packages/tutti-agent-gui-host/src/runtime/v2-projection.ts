import type { AgentActivityInteraction, AgentActivityTurn } from "@tutti-os/agent-activity-core";
import type { ConversationModel, TurnModel } from "@multica/conversation-client/models";

export interface V2Projection {
  turns: AgentActivityTurn[];
  interactions: AgentActivityInteraction[];
  activeTurnId: string | null;
}

/** Puts a conversation's own derived state into the GUI's turns and interactions. */
export function projectV2Conversation(
  conversation: ConversationModel,
  context: { agentSessionId: string },
): V2Projection {
  const { agentSessionId } = context;
  const interactions: AgentActivityInteraction[] = [];
  for (const interaction of conversation.interactions.values()) {
    if (interaction.kind !== "question") continue;
    interactions.push({
      agentSessionId, turnId: interaction.turnId, requestId: interaction.id, kind: "question", toolName: "AskUserQuestion",
      status: interaction.status === "pending" ? "pending" : interaction.status === "answered" ? "answered" : "superseded",
      input: interaction.input, output: interaction.output, createdAtUnixMs: millis(interaction.createdAt), updatedAtUnixMs: millis(interaction.updatedAt),
    });
  }

  const waiting = new Set(interactions.filter((item) => item.status === "pending").map((item) => item.turnId));
  const turns = [...conversation.turns.values()].map((turn) => projectTurn(turn, agentSessionId, waiting.has(turn.id)));
  const active = [...conversation.turns.values()].filter((turn) => turn.status === "running").sort((a, b) => millis(b.startedAt) - millis(a.startedAt))[0];
  return { turns, interactions, activeTurnId: active?.id ?? null };
}

function projectTurn(turn: TurnModel, agentSessionId: string, waiting: boolean): AgentActivityTurn {
  const settled = turn.status !== "running";
  const updated = millis(turn.completedAt ?? turn.startedAt);
  return {
    agentSessionId, turnId: turn.id, origin: "legacy_unknown",
    phase: settled ? "settled" : waiting ? "waiting" : "running",
    outcome: turn.status === "running" ? null : turn.status === "cancelled" ? "canceled" : turn.status,
    startedAtUnixMs: millis(turn.startedAt), updatedAtUnixMs: updated,
    settledAtUnixMs: settled ? updated : null,
    ...(turn.error ? { error: {
      message: turn.error.message, code: turn.error.kind,
      ...(turn.error.message.trim() ? { detail: turn.error.message } : {}),
    } } : {}),
  };
}

function millis(value: string): number {
  const result = Date.parse(value);
  return Number.isFinite(result) ? result : 0;
}
