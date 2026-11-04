import {
  normalizeAgentActivitySession,
  type AgentActivityComposerOptions,
  type AgentActivityDurableMessage,
  type AgentActivitySession,
  type AgentActivitySessionCapabilities,
  type AgentActivityTurn,
} from "@tutti-os/agent-activity-core";
import type { PendingChatTaskItem } from "@multica/core/types";
import type { AgentGuiSession, AgentGuiExecutionTarget, AgentGuiSessionDetail } from "../protocol";
import { localProjectSectionKey } from "../local-projects";
import { RuntimeLedger } from "./ledger";
import type { V2Conversation } from "@multica/core/local-conversation/conversation";
import { projectV2Conversation } from "./v2-projection";

export const MULTICA_GUI_CAPABILITIES: AgentActivitySessionCapabilities = {
  imageInput: true, modelImageInputRequired: false, modelPlanBinding: false,
  skills: false, compact: false, tokenUsage: false, rateLimits: false,
  planMode: false, interrupt: true, modelSwitch: true, activeTurnGuidance: false,
  browserUse: false, computerUse: false, goalPause: false, planImplementation: false,
  permissionModeChangeDuringTurn: false, permissionModeChangeDeferred: false,
  review: false, resumeRunningTurn: false,
};

export function composerOptions(agent: AgentGuiExecutionTarget): AgentActivityComposerOptions {
  return {
    provider: agent.provider,
    capabilities: { ...MULTICA_GUI_CAPABILITIES, interrupt: agent.canInvoke },
    models: agent.model ? [{ value: agent.model, label: agent.model, requested: true }] : [],
    reasoningEfforts: [], speeds: [], skills: [], commands: [], capabilityCatalog: [],
    modelConfigurable: false, reasoningConfigurable: false, speedConfigurable: false,
    effectiveModel: agent.model || null,
    effectiveSettings: { model: agent.model || null },
    permissionConfig: { configurable: false, modes: [] },
    behavior: {
      collapseModelOptionsToLatest: false, modelOptionsAuthoritative: true,
      refreshModelOptionsAfterSettings: false, prewarmDraftSession: false,
      planModeExclusiveWithPermissionMode: false,
    },
    slashCommandPolicy: { fallbackCommands: [], commandCatalogAuthoritative: true, commandEffects: [] },
    loadedAtUnixMs: Date.now(),
  };
}

export function projectSession(
  session: AgentGuiSession,
  agent: AgentGuiExecutionTarget | undefined,
  ledger: RuntimeLedger,
  turns: AgentActivityTurn[] = [],
  activeTurnId: string | null = null,
): AgentActivitySession {
  const guiId = ledger.guiId(session.id);
  const latestTurn = [...turns].sort((a, b) => b.updatedAtUnixMs - a.updatedAtUnixMs)[0] ?? null;
  const projectPath = session.localDirectory?.projectPath ?? null;
  return normalizeAgentActivitySession({
    workspaceId: session.workspace_id, agentSessionId: guiId,
    provider: agent?.provider ?? "unknown", agentTargetId: session.agent_id,
    railSectionKey: projectPath ? localProjectSectionKey(projectPath) : "conversations",
    providerSessionId: null, userId: session.creator_id, title: session.title,
    cwd: projectPath ?? "", noProject: !projectPath, activeTurnId,
    activeTurn: turns.find((turn) => turn.turnId === activeTurnId) ?? null,
    latestTurn, latestTurnInteractions: [], pendingInteractions: [],
    settings: { model: agent?.model || null },
    capabilities: { ...MULTICA_GUI_CAPABILITIES, interrupt: Boolean(agent?.canInvoke) },
    lifecycleCapabilities: { fork: false, forkThroughTurn: agent?.canFork === true },
    lifecycleCapabilitiesProjected: true,
    resumable: session.status === "active", visible: true,
    messageVersion: ledger.latestVersion(guiId),
    createdAtUnixMs: millis(session.created_at), updatedAtUnixMs: millis(session.updated_at),
    // The server only exposes the pinned flag and modification time.
    pinnedAtUnixMs: session.pinned ? millis(session.updated_at) : null,
  });
}

/** The aggregate identifies a live run, but does not expose its start time. */
export function projectPendingSession(
  session: AgentGuiSession,
  agent: AgentGuiExecutionTarget | undefined,
  ledger: RuntimeLedger,
  pending: PendingChatTaskItem | undefined,
): AgentActivitySession {
  if (!pending) return projectSession(session, agent, ledger);
  const turn: AgentActivityTurn = {
    agentSessionId: ledger.guiId(session.id), turnId: pending.task_id,
    origin: "legacy_unknown", outcome: null,
    phase: pending.status === "running" ? "running" : pending.status === "waiting_local_directory" ? "waiting" : "submitted",
    // Zero is the source contract's unknown time; never use session metadata
    // as if it were a real task start. Detail hydration supplies actual times.
    startedAtUnixMs: 0, updatedAtUnixMs: 0,
  };
  return projectSession(session, agent, ledger, [turn], pending.task_id);
}

/** A local conversation the source no longer holds: its metadata, without turns or messages. */
export function projectLocalMetadata(
  detail: AgentGuiSessionDetail,
  agent: AgentGuiExecutionTarget | undefined,
  ledger: RuntimeLedger,
): { session: AgentActivitySession; turns: AgentActivityTurn[]; messages: AgentActivityDurableMessage[] } {
  const session = projectSession(detail.session, agent, ledger);
  if (detail.forkedFrom) session.forkedFrom = forkedFrom(detail.forkedFrom, ledger);
  return { session, turns: [], messages: [] };
}

function forkedFrom(fork: NonNullable<AgentGuiSessionDetail["forkedFrom"]>, ledger: RuntimeLedger): NonNullable<AgentActivitySession["forkedFrom"]> {
  return {
    sourceAgentSessionId: ledger.guiId(fork.sessionId), sourceTurnId: fork.turnId,
    targetTurnId: fork.targetTurnId, operationId: fork.operationId, forkedAtUnixMs: millis(fork.createdAt),
  };
}

function millis(value: string): number {
  const result = Date.parse(value);
  return Number.isFinite(result) ? result : 0;
}

/**
 * A local conversation's view from its v2 state: turns, messages and
 * question interactions come from the conversation's events; the detail
 * supplies only metadata (session, queue, settings, fork lineage).
 */
export function projectV2Detail(
  detail: AgentGuiSessionDetail,
  conversation: V2Conversation,
  agent: AgentGuiExecutionTarget | undefined,
  ledger: RuntimeLedger,
  locale: "en" | "zh-CN" = "en",
): { session: AgentActivitySession; turns: AgentActivityTurn[]; messages: AgentActivityDurableMessage[] } {
  const sessionId = ledger.guiId(detail.session.id);
  const view = projectV2Conversation(conversation, { workspaceId: detail.session.workspace_id, agentSessionId: sessionId, locale });
  // The composer's submit id lets the engine replace its optimistic message.
  for (const message of view.messages) {
    const clientSubmitId = message.role === "user" ? ledger.clientSubmitId(message.messageId) : undefined;
    if (clientSubmitId) message.payload = { ...message.payload, clientSubmitId };
  }
  const messages = ledger.project(sessionId, view.messages, new Map(view.turns.map((turn) => [turn.turnId, turn.startedAtUnixMs])), true);
  const turns = view.turns.map((turn) => ({ ...turn, providerForkBindingAvailable: agent?.canFork === true }));
  const session = projectSession(detail.session, agent, ledger, turns, view.activeTurnId);
  session.latestTurnInteractions = view.interactions.filter((item) => item.turnId === (session.activeTurnId ?? session.latestTurn?.turnId));
  session.pendingInteractions = session.latestTurnInteractions.filter((item) => item.status === "pending");
  if (detail.forkedFrom) session.forkedFrom = forkedFrom(detail.forkedFrom, ledger);
  return { session, turns, messages };
}
