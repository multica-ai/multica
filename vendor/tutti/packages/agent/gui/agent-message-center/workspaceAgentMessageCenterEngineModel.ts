import type {
  AgentActivityInteraction,
  AgentActivityMessage,
  AgentActivityNeedsAttentionItem,
  AgentSessionEngineState,
  WorkspaceAgentConsumerSession
} from "@tutti-os/agent-activity-core";
import { normalizeAgentApprovalPurpose } from "../shared/agentConversation/agentApprovalPurpose";
import {
  selectWorkspaceAgentRootConversationSessions
} from "@tutti-os/agent-activity-core";
import type { AgentConversationPromptVM } from "../shared/agentConversation/contracts/agentConversationVM";
import { normalizeAskUserQuestions } from "../shared/agentConversation/askUserQuestions";
import {
  extractExitPlanKeepPlanningOptionId,
  extractExitPlanModeOptions,
  isExitPlanSwitchModeInput
} from "../shared/agentConversation/exitPlanOptions";
import {
  consumerAwaitingPlanImplementation,
  selectRootAgentSessionIdsAwaitingPlanImplementation
} from "../shared/agentConversation/planImplementationAwaiting";
import { planImplementationPromptFromPlanTurn } from "../shared/agentConversation/planImplementationPresentation";
import {
  buildWorkspaceAgentMessageCenterItem,
  buildWorkspaceAgentMessageCenterModelFromItems,
  type BuildWorkspaceAgentMessageCenterOptions,
  type WorkspaceAgentMessageCenterModel,
  type WorkspaceAgentMessageCenterTurnOutcome
} from "./workspaceAgentMessageCenterModel";
import { workspaceAgentMessageCenterItemEqual } from "./workspaceAgentMessageCenterModelStability";

/**
 * Canonical Message Center entrypoint. Session/turn/interaction truth comes
 * directly from the engine. Durable messages are presentation input only:
 * title and digest text may use them, but they cannot recreate lifecycle or
 * pending-interaction state.
 */
export function buildWorkspaceAgentMessageCenterModelFromEngine(
  presentation: WorkspaceAgentMessageCenterPresentation,
  snapshot: WorkspaceAgentMessageCenterMessageSnapshot,
  options: BuildWorkspaceAgentMessageCenterOptions = {}
): WorkspaceAgentMessageCenterModel {
  const includeHiddenSessionIds = new Set(
    (options.includeHiddenSessionIds ?? [])
      .map((sessionId) => sessionId.trim())
      .filter((sessionId) => sessionId.length > 0)
  );
  const items = presentation.consumers
    .filter(
      (consumer) =>
        consumer.session.visible !== false ||
        includeHiddenSessionIds.has(consumer.session.agentSessionId.trim())
    )
    .map((consumer) => {
      const interaction = latestPendingInteraction(consumer);
      const planTurn =
        !interaction &&
        consumer.latestTurn?.turnId ===
          presentation.awaitingPlanImplementationTurnIdBySessionId?.[
            consumer.session.agentSessionId
          ]
          ? consumer.latestTurn
          : null;
      const planPrompt = planTurn
        ? planImplementationPromptFromPlanTurn(
            planTurn.turnId,
            consumer.session.title
          )
        : null;
      const needsAttention = interaction
        ? needsAttentionFromInteraction(consumer, interaction)
        : planTurn && planPrompt
          ? needsAttentionFromPlan(consumer, planTurn, planPrompt.title)
          : null;
      return buildWorkspaceAgentMessageCenterItem({
        session: consumer.session,
        latestTurn: consumer.latestTurn,
        messages: sessionMessages(snapshot.sessionMessagesById, consumer),
        status: planTurn ? "waiting" : consumer.displayStatus,
        needsAttention,
        pendingInteractionTarget: interaction
          ? interactionTarget(interaction)
          : null,
        pendingPrompt: interaction
          ? promptFromInteraction(interaction)
          : planPrompt,
        latestTurnOutcome: planTurn ? null : turnOutcome(consumer),
        options
      });
    });
  return buildWorkspaceAgentMessageCenterModelFromItems(
    items,
    options.itemCutoffUnixMs
  );
}

interface WorkspaceAgentMessageCenterMessageSnapshot {
  readonly sessionMessagesById: Readonly<
    Record<string, readonly AgentActivityMessage[]>
  >;
  readonly workspaceId: string;
}

export interface WorkspaceAgentMessageCenterPresentation {
  consumers: readonly WorkspaceAgentConsumerSession[];
  /**
   * Derived from the canonical Engine message window. Optional for hosts that
   * construct the older presentation shape; absence fails closed.
   */
  awaitingPlanImplementationTurnIdBySessionId?: Readonly<
    Record<string, string>
  >;
  promptStatusByKey: Readonly<
    Record<string, WorkspaceAgentMessageCenterPromptStatus>
  >;
}

export function selectWorkspaceAgentMessageCenterPresentation(
  state: AgentSessionEngineState,
  interactionResponse?: (
    agentSessionId: string,
    turnId: string,
    requestId: string
  ) => { status: WorkspaceAgentMessageCenterPromptStatus } | null
): WorkspaceAgentMessageCenterPresentation {
  const consumers = selectWorkspaceAgentRootConversationSessions(state);
  const promptStatusByKey: Record<
    string,
    WorkspaceAgentMessageCenterPromptStatus
  > = {};
  const awaitingPlanImplementationTurnIdBySessionId: Record<string, string> =
    {};
  const awaitingPlanImplementationSessionIds = new Set(
    selectRootAgentSessionIdsAwaitingPlanImplementation(state)
  );
  for (const consumer of consumers) {
    const sessionId = consumer.session.agentSessionId;
    if (
      awaitingPlanImplementationSessionIds.has(sessionId) &&
      consumer.latestTurn
    ) {
      awaitingPlanImplementationTurnIdBySessionId[sessionId] =
        consumer.latestTurn.turnId;
    }
    for (const interaction of consumer.pendingInteractions) {
      const response = interactionResponse?.(
        interaction.agentSessionId,
        interaction.turnId,
        interaction.requestId
      );
      if (response) {
        promptStatusByKey[
          promptStatusKey(
            interaction.agentSessionId,
            interaction.turnId,
            interaction.requestId
          )
        ] = response.status;
      }
    }
  }
  return {
    consumers,
    awaitingPlanImplementationTurnIdBySessionId,
    promptStatusByKey
  };
}

export function workspaceAgentMessageCenterPresentationEqual(
  left: WorkspaceAgentMessageCenterPresentation,
  right: WorkspaceAgentMessageCenterPresentation
): boolean {
  return (
    stringMapsEqual(
      left.awaitingPlanImplementationTurnIdBySessionId ?? {},
      right.awaitingPlanImplementationTurnIdBySessionId ?? {}
    ) &&
    promptStatusMapsEqual(left.promptStatusByKey, right.promptStatusByKey) &&
    left.consumers.length === right.consumers.length &&
    left.consumers.every((item, index) => {
      const candidate = right.consumers[index];
      return (
        candidate !== undefined &&
        item.session === candidate.session &&
        item.activeTurn === candidate.activeTurn &&
        item.latestTurn === candidate.latestTurn &&
        item.displayStatus === candidate.displayStatus &&
        item.pendingInteractions.length ===
          candidate.pendingInteractions.length &&
        item.pendingInteractions.every(
          (interaction, interactionIndex) =>
            interaction === candidate.pendingInteractions[interactionIndex]
        )
      );
    })
  );
}

export type WorkspaceAgentAttentionTarget =
  | {
      kind: "interaction";
      workspaceId: string;
      agentSessionId: string;
      turnId: string;
      requestId: string;
    }
  | {
      kind: "plan-implementation";
      workspaceId: string;
      agentSessionId: string;
      turnId: string;
      requestId: string;
    };

export interface WorkspaceAgentAttentionItem {
  item: import("./workspaceAgentMessageCenterModel").WorkspaceAgentMessageCenterItem;
  status: WorkspaceAgentMessageCenterPromptStatus;
  target: WorkspaceAgentAttentionTarget;
}

/**
 * Projects every canonical actionable target instead of collapsing a root
 * conversation to its latest interaction. The display item keeps the root
 * conversation identity for navigation while target keeps the exact child
 * session/turn/request identity used by the command.
 */
export function selectWorkspaceAgentAttentionItems(
  presentation: WorkspaceAgentMessageCenterPresentation,
  snapshot: WorkspaceAgentMessageCenterMessageSnapshot,
  options: BuildWorkspaceAgentMessageCenterOptions = {}
): WorkspaceAgentAttentionItem[] {
  const result: WorkspaceAgentAttentionItem[] = [];
  for (const consumer of presentation.consumers) {
    if (
      consumer.session.visible === false ||
      consumer.session.workspaceId !== snapshot.workspaceId
    ) {
      continue;
    }
    const messages = sessionMessages(snapshot.sessionMessagesById, consumer);
    for (const interaction of consumer.pendingInteractions) {
      const prompt = promptFromInteraction(interaction);
      if (!prompt) continue;
      const target: WorkspaceAgentAttentionTarget = {
        kind: "interaction",
        workspaceId: consumer.session.workspaceId,
        agentSessionId: interaction.agentSessionId,
        turnId: interaction.turnId,
        requestId: interaction.requestId
      };
      const item = buildWorkspaceAgentMessageCenterItem({
        session: consumer.session,
        latestTurn: consumer.latestTurn,
        messages,
        status: "waiting",
        needsAttention: needsAttentionFromInteraction(consumer, interaction),
        pendingInteractionTarget: interactionTarget(interaction),
        pendingPrompt: prompt,
        latestTurnOutcome: null,
        options
      });
      item.id = workspaceAgentAttentionKey(target);
      item.sortTimeUnixMs = interaction.createdAtUnixMs;
      result.push({
        item,
        status:
          presentation.promptStatusByKey[
            promptStatusKey(
              interaction.agentSessionId,
              interaction.turnId,
              interaction.requestId
            )
          ] ?? "idle",
        target
      });
    }

    const latestTurn = consumer.latestTurn;
    const statusKey = promptStatusKey(
      consumer.session.agentSessionId,
      latestTurn?.turnId ?? "",
      latestTurn?.turnId ?? ""
    );
    if (!latestTurn) {
      continue;
    }
    const presentationAwaitsPlan =
      presentation.awaitingPlanImplementationTurnIdBySessionId?.[
        consumer.session.agentSessionId
      ] === latestTurn.turnId;
    if (
      !presentationAwaitsPlan &&
      !consumerAwaitingPlanImplementation({
        capabilities: consumer.session.capabilities,
        latestTurn,
        messages
      })
    ) {
      continue;
    }
    const target: WorkspaceAgentAttentionTarget = {
      kind: "plan-implementation",
      workspaceId: consumer.session.workspaceId,
      agentSessionId: consumer.session.agentSessionId,
      turnId: latestTurn.turnId,
      requestId: latestTurn.turnId
    };
    const prompt = planImplementationPromptFromPlanTurn(
      latestTurn.turnId,
      consumer.session.title
    );
    const needsAttention = needsAttentionFromPlan(
      consumer,
      latestTurn,
      prompt.title
    );
    const item = buildWorkspaceAgentMessageCenterItem({
      session: consumer.session,
      latestTurn,
      messages,
      status: "waiting",
      needsAttention,
      pendingInteractionTarget: null,
      pendingPrompt: prompt,
      latestTurnOutcome: null,
      options
    });
    item.id = workspaceAgentAttentionKey(target);
    item.sortTimeUnixMs = needsAttention.occurredAtUnixMs;
    result.push({
      item,
      status: presentation.promptStatusByKey[statusKey] ?? "idle",
      target
    });
  }
  return result;
}

export function workspaceAgentAttentionItemsEqual(
  left: readonly WorkspaceAgentAttentionItem[],
  right: readonly WorkspaceAgentAttentionItem[]
): boolean {
  return (
    left.length === right.length &&
    left.every((entry, index) => {
      const candidate = right[index];
      return (
        candidate !== undefined &&
        entry.status === candidate.status &&
        workspaceAgentAttentionTargetEqual(entry.target, candidate.target) &&
        workspaceAgentMessageCenterItemEqual(entry.item, candidate.item)
      );
    })
  );
}

function workspaceAgentAttentionKey(target: WorkspaceAgentAttentionTarget) {
  return [
    target.workspaceId,
    target.agentSessionId,
    target.turnId,
    target.requestId
  ].join("\n");
}

function workspaceAgentAttentionTargetEqual(
  left: WorkspaceAgentAttentionTarget,
  right: WorkspaceAgentAttentionTarget
): boolean {
  return (
    left.kind === right.kind &&
    left.workspaceId === right.workspaceId &&
    left.agentSessionId === right.agentSessionId &&
    left.turnId === right.turnId &&
    left.requestId === right.requestId
  );
}

export type WorkspaceAgentMessageCenterPromptStatus =
  | "idle"
  | "responding"
  | "unknown"
  | "failed";

export function workspaceAgentMessageCenterPromptStatus(
  presentation: WorkspaceAgentMessageCenterPresentation,
  item: Pick<
    import("./workspaceAgentMessageCenterModel").WorkspaceAgentMessageCenterItem,
    "agentSessionId" | "pendingInteractionTarget" | "pendingPrompt"
  >
): WorkspaceAgentMessageCenterPromptStatus {
  const prompt = item.pendingPrompt;
  if (!prompt) return "idle";
  const target = item.pendingInteractionTarget;
  return (
    presentation.promptStatusByKey[
      target
        ? promptStatusKey(
            target.agentSessionId,
            target.turnId,
            target.requestId
          )
        : promptStatusKey(
            item.agentSessionId,
            prompt.requestId,
            prompt.requestId
          )
    ] ?? "idle"
  );
}

function promptStatusKey(
  agentSessionId: string,
  turnId: string,
  requestId: string
): string {
  return `${agentSessionId}\0${turnId}\0${requestId}`;
}

function promptStatusMapsEqual(
  left: Readonly<Record<string, WorkspaceAgentMessageCenterPromptStatus>>,
  right: Readonly<Record<string, WorkspaceAgentMessageCenterPromptStatus>>
): boolean {
  const keys = Object.keys(left);
  return (
    keys.length === Object.keys(right).length &&
    keys.every((key) => left[key] === right[key])
  );
}

function booleanMapsEqual(
  left: Readonly<Record<string, true>>,
  right: Readonly<Record<string, true>>
): boolean {
  const keys = Object.keys(left);
  return (
    keys.length === Object.keys(right).length &&
    keys.every((key) => right[key] === true)
  );
}

function stringMapsEqual(
  left: Readonly<Record<string, string>>,
  right: Readonly<Record<string, string>>
): boolean {
  const keys = Object.keys(left);
  return (
    keys.length === Object.keys(right).length &&
    keys.every((key) => left[key] === right[key])
  );
}

function sessionMessages(
  sessionMessagesById: Readonly<
    Record<string, readonly AgentActivityMessage[]>
  >,
  consumer: WorkspaceAgentConsumerSession
): readonly AgentActivityMessage[] {
  for (const id of [
    consumer.session.agentSessionId,
    consumer.session.providerSessionId
  ]) {
    const normalized = id?.trim() ?? "";
    if (normalized && sessionMessagesById[normalized]) {
      return sessionMessagesById[normalized];
    }
  }
  return [];
}

function latestPendingInteraction(
  consumer: WorkspaceAgentConsumerSession
): AgentActivityInteraction | null {
  return consumer.pendingInteractions.at(-1) ?? null;
}

function interactionTarget(
  interaction: AgentActivityInteraction
): import("./workspaceAgentMessageCenterModel").WorkspaceAgentMessageCenterInteractionTarget {
  return {
    agentSessionId: interaction.agentSessionId,
    requestId: interaction.requestId,
    turnId: interaction.turnId
  };
}

function needsAttentionFromInteraction(
  consumer: WorkspaceAgentConsumerSession,
  interaction: AgentActivityInteraction
): AgentActivityNeedsAttentionItem {
  const summary = interactionSummary(interaction);
  return {
    id: `interaction:${interaction.requestId}`,
    workspaceId: consumer.session.workspaceId,
    agentSessionId: consumer.session.agentSessionId,
    provider: consumer.session.provider,
    title: summary,
    cwd: consumer.session.cwd,
    kind:
      interaction.kind === "approval"
        ? "permission"
        : interaction.kind === "question"
          ? "question"
          : "constraint",
    summary,
    occurredAtUnixMs: interaction.createdAtUnixMs
  };
}

function needsAttentionFromPlan(
  consumer: WorkspaceAgentConsumerSession,
  turn: NonNullable<WorkspaceAgentConsumerSession["latestTurn"]>,
  title: string
): AgentActivityNeedsAttentionItem {
  return {
    id: `plan-implementation:${turn.turnId}`,
    workspaceId: consumer.session.workspaceId,
    agentSessionId: consumer.session.agentSessionId,
    provider: consumer.session.provider,
    title,
    cwd: consumer.session.cwd,
    kind: "constraint",
    summary: title,
    occurredAtUnixMs: turn.settledAtUnixMs ?? turn.updatedAtUnixMs
  };
}

function promptFromInteraction(
  interaction: AgentActivityInteraction
): AgentConversationPromptVM | null {
  const input = interaction.input ?? {};
  const normalizedToolName = (interaction.toolName ?? "")
    .replace(/[_\s-]+/g, "")
    .trim()
    .toLowerCase();
  if (
    interaction.kind === "plan" ||
    normalizedToolName === "exitplanmode" ||
    isExitPlanSwitchModeInput(input)
  ) {
    const keepPlanningOptionId = extractExitPlanKeepPlanningOptionId(input);
    return {
      kind: "exit-plan",
      requestId: interaction.requestId,
      title: interactionSummary(interaction),
      options: extractExitPlanModeOptions(input),
      ...(keepPlanningOptionId ? { keepPlanningOptionId } : {})
    };
  }
  if (interaction.kind === "question") {
    const questions = normalizeAskUserQuestions(input.questions);
    return {
      agentSessionId: interaction.agentSessionId,
      kind: "ask-user",
      requestId: interaction.requestId,
      title: interactionSummary(interaction),
      turnId: interaction.turnId,
      questions:
        questions.length > 0
          ? questions
          : [
              {
                id: "response",
                header: "",
                question: interactionSummary(interaction),
                options: [],
                multiSelect: false,
                answer: null
              }
            ]
    };
  }
  if (interaction.kind !== "approval") {
    return null;
  }
  const options = arrayValue(input.options).flatMap((value) => {
    const option = recordValue(value);
    const id = textValue(option.optionId) ?? textValue(option.id);
    return id
      ? [
          {
            id,
            label: textValue(option.label) ?? textValue(option.name) ?? id,
            kind: textValue(option.kind) ?? id,
            ...(textValue(option.description)
              ? { description: textValue(option.description) as string }
              : {})
          }
        ]
      : [];
  });
  const approvalPurpose = normalizeAgentApprovalPurpose(
    interaction.metadata?.approvalPurpose
  );
  return {
    kind: "approval",
    id: `approval:${interaction.requestId}`,
    turnId: interaction.turnId,
    requestId: interaction.requestId,
    callId: textValue(input.callId) ?? interaction.requestId,
    ...(approvalPurpose ? { approvalPurpose } : {}),
    title: interactionSummary(interaction),
    toolName: interaction.toolName ?? null,
    status: interaction.status,
    input,
    options,
    output: interaction.output ?? null,
    occurredAtUnixMs: interaction.createdAtUnixMs
  };
}

function turnOutcome(
  consumer: WorkspaceAgentConsumerSession
): WorkspaceAgentMessageCenterTurnOutcome | null {
  if (
    consumer.displayStatus !== "completed" &&
    consumer.displayStatus !== "failed"
  ) {
    return null;
  }
  const turn = consumer.latestTurn;
  if (!turn || turn.phase !== "settled") return null;
  const status =
    turn.outcome === "completed"
      ? "completed"
      : turn.outcome === "failed"
        ? "failed"
        : null;
  return status
    ? {
        notificationKey: `${consumer.session.agentSessionId}:turn:${turn.turnId}:${status}`,
        status,
        turnId: turn.turnId
      }
    : null;
}

function interactionSummary(interaction: AgentActivityInteraction): string {
  const input = interaction.input ?? {};
  return (
    textValue(input.question) ??
    textValue(input.title) ??
    textValue(input.summary) ??
    interaction.toolName?.trim() ??
    interaction.kind
  );
}

function textValue(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function recordValue(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}
