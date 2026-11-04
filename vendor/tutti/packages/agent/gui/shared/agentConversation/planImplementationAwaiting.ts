import {
  selectSessionMessages,
  selectWorkspaceAgentConsumerSessions,
  type AgentActivitySession,
  type AgentActivityTurn
} from "@tutti-os/agent-activity-core";
import { latestPlanTurnId } from "./planImplementationPresentation";

type AgentSessionEngineSnapshot = Parameters<
  typeof selectWorkspaceAgentConsumerSessions
>[0];

/**
 * Shared predicate for the synthesized plan-implementation wait. Durable
 * approval / question / exit-plan waits already ride pendingInteractions;
 * this predicate is the matching cross-surface fact for plan confirmation so
 * rail, conversation list, and attention do not each invent a private rule.
 *
 * Capability rules match the detail composer: an explicit `false` fails closed,
 * but a missing capabilities record stays open so a settled plan turn can still
 * surface waiting while composer options are the only advertised source.
 */
export function consumerAwaitingPlanImplementation(input: {
  capabilities: AgentActivitySession["capabilities"] | null | undefined;
  latestTurn: AgentActivityTurn | null | undefined;
  messages: readonly {
    turnId?: string | null;
    occurredAtUnixMs?: number | null;
    createdAtUnixMs?: number | null;
    seq?: number | null;
    payload?: Record<string, unknown> | null;
  }[];
}): boolean {
  const latestTurn = input.latestTurn;
  if (
    !latestTurn ||
    latestTurn.phase !== "settled" ||
    latestTurn.outcome !== "completed" ||
    !sessionAllowsPlanImplementation(input.capabilities)
  ) {
    return false;
  }
  return latestPlanTurnId(input.messages) === latestTurn.turnId;
}

function sessionAllowsPlanImplementation(
  capabilities: AgentActivitySession["capabilities"] | null | undefined
): boolean {
  if (!capabilities) {
    return true;
  }
  return (
    capabilities.planImplementation === true && capabilities.planMode === true
  );
}

export function selectRootAgentSessionIdsAwaitingPlanImplementation(
  state: AgentSessionEngineSnapshot
): readonly string[] {
  const sessionIds: string[] = [];
  for (const consumer of selectWorkspaceAgentConsumerSessions(state)) {
    const sessionId = consumer.session.agentSessionId;
    if (
      !consumerAwaitingPlanImplementation({
        capabilities: consumer.session.capabilities,
        latestTurn: consumer.latestTurn,
        messages: sessionMessagesForPlanDetection(state, consumer.session)
      })
    ) {
      continue;
    }
    sessionIds.push(sessionId);
  }
  return sessionIds;
}

function sessionMessagesForPlanDetection(
  state: AgentSessionEngineSnapshot,
  session: Pick<AgentActivitySession, "agentSessionId" | "providerSessionId">
) {
  for (const id of [session.agentSessionId, session.providerSessionId]) {
    const messages = selectSessionMessages(state, id);
    if (messages.length > 0) {
      return messages;
    }
  }
  return [];
}
