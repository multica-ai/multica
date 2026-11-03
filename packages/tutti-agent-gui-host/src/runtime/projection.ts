import {
  normalizeAgentActivitySession,
  type AgentActivityComposerOptions,
  type AgentActivityDurableMessage,
  type AgentActivitySession,
  type AgentActivitySessionCapabilities,
  type AgentActivityTurn,
  type AgentActivityInteraction,
} from "@tutti-os/agent-activity-core";
import type { AgentTask, PendingChatTaskItem } from "@multica/core/types";
import type { AgentGuiSession, AgentGuiExecutionTarget, AgentGuiSessionDetail, AgentGuiTaskMessagePayload } from "../protocol";
import { localProjectSectionKey } from "../local-projects";
import { RuntimeLedger } from "./ledger";
import { chatAuthoredPrompt } from "@multica/core/chat/context-snapshot";
import { localMessageDisplayContent } from "@multica/core/local-conversation/protocol";
import { backgroundTaskPresentation } from "./background-task-presentation";
import type { V2Conversation } from "./v2-conversation";
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

export function projectDetail(
  detail: AgentGuiSessionDetail,
  agent: AgentGuiExecutionTarget | undefined,
  transcript: Map<string, AgentGuiTaskMessagePayload[]>,
  ledger: RuntimeLedger,
  locale: "en" | "zh-CN" = "en",
): { session: AgentActivitySession; turns: AgentActivityTurn[]; messages: AgentActivityDurableMessage[] } {
  const sessionId = ledger.guiId(detail.session.id);
  const local = detail.session.agent_id.startsWith("local:");
  const turns = new Map<string, AgentActivityTurn>();
  const taskById = new Map((detail.tasks ?? []).map((task) => [task.id, task]));
  const raw: Omit<AgentActivityDurableMessage, "version" | "sequence">[] = [];
  const finalTaskIds = new Set(detail.messages.filter((message) => message.role === "assistant").map((message) => message.task_id));

  const renderedInputs = new Set<string>();
  for (const message of detail.messages) {
    if (message.message_kind === "onboarding_kickoff") continue;
    const owner = message.task_id ? taskById.get(message.task_id) : undefined;
    if (message.role === "user" && owner?.chat_invocation_group_id) {
      if (renderedInputs.has(owner.chat_invocation_group_id)) continue;
      renderedInputs.add(owner.chat_invocation_group_id);
    }
    const createdAt = millis(message.created_at);
    const clientSubmitId = ledger.clientSubmitId(message.id);
    const accepted = clientSubmitId ? ledger.submission(clientSubmitId)?.response : undefined;
    const authored = message.role === "user" ? chatAuthoredPrompt(message.content) : message.content;
    const content = detail.session.agent_id.startsWith("local:")
      ? localMessageDisplayContent({ role: message.role, content: authored, attachmentIds: message.attachments?.filter((attachment) => attachment.content_type.startsWith("image/")).map((attachment) => attachment.id) })
      : authored;
    raw.push({
      workspaceId: detail.session.workspace_id, agentSessionId: sessionId,
      messageId: message.id, turnId: message.task_id ?? accepted?.task_id ?? null, role: message.role,
      kind: message.failure_reason ? "error" : "text",
      payload: {
        ...(message.role === "assistant" ? invocationIdentity(owner) : {}),
        text: content,
        content,
        ...(clientSubmitId ? { clientSubmitId } : {}),
        ...(message.attachments?.length ? {
          attachments: message.attachments.map((attachment) => ({
            attachmentId: attachment.id, name: attachment.filename,
            mimeType: attachment.content_type, url: detail.session.agent_id.startsWith("local:") && attachment.content_type.startsWith("image/") ? undefined : attachment.markdown_url || attachment.url,
            sizeBytes: attachment.size_bytes,
          })),
          content: [
            { type: "text", text: content },
            ...message.attachments.map((attachment) => ({
              type: attachment.content_type.startsWith("image/") ? "image" : "file",
              attachmentId: attachment.id, name: attachment.filename,
              mimeType: attachment.content_type,
              url: detail.session.agent_id.startsWith("local:") && attachment.content_type.startsWith("image/") ? undefined : attachment.markdown_url || attachment.url,
            })),
          ],
        } : {}),
      },
      semantics: message.role === "assistant" ? { userVisibleAssistantResponse: true, turnSettling: true } : undefined,
      occurredAtUnixMs: createdAt,
    });
    if (message.task_id && message.role === "assistant" && !taskById.has(message.task_id)) {
      turns.set(message.task_id, {
        agentSessionId: sessionId, turnId: message.task_id,
        origin: "legacy_unknown", phase: "settled",
        outcome: message.failure_reason ? "failed" : "completed",
        startedAtUnixMs: message.elapsed_ms == null ? createdAt : createdAt - message.elapsed_ms,
        updatedAtUnixMs: createdAt, settledAtUnixMs: createdAt,
        ...(message.failure_reason ? { error: {
          code: message.failure_reason, message: message.content,
          ...(local && message.content.trim() ? { detail: message.content } : {}),
        } } : {}),
      });
    }
  }
  for (const task of detail.tasks ?? []) turns.set(task.id, projectTask(task, sessionId, local));
  if (detail.pending.task_id && !taskById.has(detail.pending.task_id)) {
    turns.set(detail.pending.task_id, {
      agentSessionId: sessionId, turnId: detail.pending.task_id, origin: "legacy_unknown",
      phase: detail.pending.status === "running" ? "running" : detail.pending.status === "waiting_local_directory" ? "waiting" : "submitted",
      startedAtUnixMs: millis(detail.pending.created_at ?? detail.session.updated_at),
      updatedAtUnixMs: millis(detail.pending.created_at ?? detail.session.updated_at),
    });
  }
  for (const [taskId, events] of transcript) {
    const task = taskById.get(taskId);
    const fallbackTime = millis(task?.created_at ?? detail.pending.created_at ?? detail.session.updated_at);
    const toolMessages = new Map<string, Omit<AgentActivityDurableMessage, "version" | "sequence">>();
    let fragmentIndex: number | null = null;
    for (const event of events) {
      const time = event.created_at ? millis(event.created_at) : fallbackTime + event.seq;
      const base = {
        workspaceId: detail.session.workspace_id, agentSessionId: sessionId,
        turnId: taskId, role: "assistant", occurredAtUnixMs: time,
      };
      if (event.type === "status") {
        fragmentIndex = null;
        const retry = /^Reconnecting\.\.\. (\d+\/\d+)/.exec(event.content ?? "");
        if (retry && task && ["completed", "failed", "cancelled"].includes(task.status)) continue;
        const title = retry
          ? locale === "zh-CN" ? `连接中断，正在重连（${retry[1]}）` : `Connection interrupted. Reconnecting (${retry[1]})`
          : locale === "zh-CN" ? "运行状态" : "Execution status";
        raw.push({ ...base, messageId: `run:${taskId}:${event.seq}`, kind: "text",
          payload: { kind: "agent_system_notice", noticeKind: retry ? "transport_retry" : "system_notice", title, content: title,
            detail: event.content ?? "", ...(retry ? { severity: "warning", retryable: true } : {}) },
          semantics: { userVisibleAssistantResponse: false, turnSettling: false },
        });
      } else if (event.type === "tool_use" || event.type === "tool_result") {
        fragmentIndex = null;
        const identity = event.call_id ? `${taskId}:${event.call_id}` : `${taskId}:seq:${event.seq}`;
        const previous = toolMessages.get(identity);
        toolMessages.set(identity, {
          ...base, messageId: `tool:${identity}`, kind: "tool_call",
          occurredAtUnixMs: previous?.occurredAtUnixMs ?? time,
          status: event.type === "tool_result" ? "completed" : "running",
          payload: {
            ...previous?.payload, callId: identity, toolName: event.tool || previous?.payload.toolName || "Tool",
            ...(event.input ? { input: event.input } : {}),
            ...(event.output !== undefined ? { output: imageGenerationOutput(event.tool || previous?.payload.toolName, event.output) } : {}),
            ...(event.output_truncated !== undefined ? { outputTruncated: event.output_truncated } : {}),
          },
        });
      } else if (event.type !== "text" || !finalTaskIds.has(taskId)) {
        const kind = event.type === "thinking" ? "reasoning" : event.type === "error" ? "error" : "text";
        const fragment = fragmentIndex === null ? undefined : raw[fragmentIndex];
        if (fragment?.kind === kind && kind !== "error") {
          fragment.payload.content = `${fragment.payload.content ?? ""}${event.content ?? ""}`;
        } else {
          fragmentIndex = raw.length;
          raw.push({ ...base, messageId: `run:${taskId}:${event.seq}`, kind, payload: { content: event.content ?? "", ...(event.type === "text" ? invocationIdentity(task) : {}) } });
        }
      }
    }
    raw.push(...toolMessages.values());
  }
  for (const { turnId, task } of detail.backgroundTasks ?? []) {
    if (task.ambient) continue;
    const labels = locale === "zh-CN"
      ? { running: "后台任务运行中", completed: "后台任务已完成", failed: "后台任务失败", cancelled: "后台任务已停止" }
      : { running: "Background task running", completed: "Background task completed", failed: "Background task failed", cancelled: "Background task stopped" };
    const title = `${labels[task.status] ?? (locale === "zh-CN" ? "后台任务" : "Background task")} · ${task.description}`;
    const { progress, detail: taskDetail } = backgroundTaskPresentation(task, transcript.get(turnId) ?? [], locale);
    raw.push({ workspaceId: detail.session.workspace_id, agentSessionId: sessionId, turnId,
      messageId: `background:${turnId}:${task.id}`, role: "assistant", kind: "text", status: task.status,
      payload: { kind: "agent_system_notice", noticeKind: "background_task", title, content: progress || title, ...(taskDetail ? { detail: taskDetail } : {}) },
      semantics: { userVisibleAssistantResponse: false, turnSettling: false },
      occurredAtUnixMs: millis(task.createdAt ?? taskById.get(turnId)?.created_at ?? detail.session.created_at),
    });
  }
  const interactions: AgentActivityInteraction[] = (detail.questions ?? []).map(({ turnId, question }) => {
    const active = question.status === "pending" && detail.pending.task_id === turnId;
    const status = question.status === "answered" ? "answered" : active ? "pending" : "superseded";
    const input = { requestId: question.request_id, questions: question.questions };
    const output = question.answers ? { answersByQuestionId: question.answers, answers: Object.values(question.answers).map((value) => Array.isArray(value) ? value.join(", ") : value) } : undefined;
    raw.push({ workspaceId: detail.session.workspace_id, agentSessionId: sessionId, turnId,
      messageId: `question:${turnId}:${question.request_id}`, role: "assistant", kind: "tool_call",
      status: active ? "running" : question.status === "answered" ? "completed" : "failed",
      occurredAtUnixMs: millis(question.createdAt),
      payload: { callId: `question:${turnId}:${question.request_id}`, callType: "interactive", toolName: "AskUserQuestion", input, ...(output ? { output } : {}),
        metadata: { callType: "interactive", interactiveKind: "ask-user", toolName: "AskUserQuestion" } },
    });
    return { agentSessionId: sessionId, turnId, requestId: question.request_id, kind: "question", toolName: "AskUserQuestion", status,
      input, output, createdAtUnixMs: millis(question.createdAt), updatedAtUnixMs: millis(question.updatedAt) };
  });
  const messages = ledger.project(sessionId, raw, new Map([...turns.values()].map((turn) => [turn.turnId, turn.startedAtUnixMs])));
  const projectedTurns = [...turns.values()].map((turn) => ({ ...turn, ...(interactions.some((item) => item.turnId === turn.turnId && item.status === "pending") ? { phase: "waiting" as const } : {}), providerForkBindingAvailable: agent?.canFork === true }));
  const session = projectSession(detail.session, agent, ledger, projectedTurns, detail.pending.task_id ?? null);
  session.latestTurnInteractions = interactions.filter((item) => item.turnId === (session.activeTurnId ?? session.latestTurn?.turnId));
  session.pendingInteractions = session.latestTurnInteractions.filter((item) => item.status === "pending");
  if (detail.forkedFrom) session.forkedFrom = {
    sourceAgentSessionId: ledger.guiId(detail.forkedFrom.sessionId), sourceTurnId: detail.forkedFrom.turnId,
    targetTurnId: detail.forkedFrom.targetTurnId, operationId: detail.forkedFrom.operationId, forkedAtUnixMs: millis(detail.forkedFrom.createdAt),
  };
  return {
    session,
    turns: projectedTurns, messages,
  };
}

function imageGenerationOutput(tool: unknown, output: string): unknown {
  if (tool === "ImageGeneration") {
    try {
      const value: unknown = JSON.parse(output);
      if (value && typeof value === "object" && !Array.isArray(value)) return value;
    } catch { /* Keep diagnostics as ordinary text. */ }
  }
  return output;
}

function projectTask(task: AgentTask, sessionId: string, local: boolean): AgentActivityTurn {
  const terminal = ["completed", "failed", "cancelled"].includes(task.status);
  return {
    agentSessionId: sessionId, turnId: task.id, origin: "legacy_unknown",
    phase: terminal ? "settled" : task.status === "running" ? "running" : task.status === "waiting_local_directory" ? "waiting" : "submitted",
    outcome: task.status === "cancelled" ? "canceled" : task.status === "completed" || task.status === "failed" ? task.status : null,
    startedAtUnixMs: millis(task.started_at ?? task.created_at),
    updatedAtUnixMs: millis(task.completed_at ?? task.started_at ?? task.created_at),
    settledAtUnixMs: task.completed_at ? millis(task.completed_at) : null,
    ...(task.error ? { error: {
      message: task.error, ...(task.failure_reason ? { code: task.failure_reason } : {}),
      ...(local && task.error.trim() ? { detail: task.error } : {}),
    } } : {}),
  };
}

function millis(value: string): number {
  const result = Date.parse(value);
  return Number.isFinite(result) ? result : 0;
}

function invocationIdentity(task: AgentTask | undefined) {
  const name = task?.chat_invoked_agent_name;
  return name ? { invokedAgent: { id: task.agent_id, name } } : {};
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
  const messages = ledger.project(sessionId, view.messages, new Map(view.turns.map((turn) => [turn.turnId, turn.startedAtUnixMs])));
  const turns = view.turns.map((turn) => ({ ...turn, providerForkBindingAvailable: agent?.canFork === true }));
  const session = projectSession(detail.session, agent, ledger, turns, view.activeTurnId);
  session.latestTurnInteractions = view.interactions.filter((item) => item.turnId === (session.activeTurnId ?? session.latestTurn?.turnId));
  session.pendingInteractions = session.latestTurnInteractions.filter((item) => item.status === "pending");
  if (detail.forkedFrom) session.forkedFrom = {
    sourceAgentSessionId: ledger.guiId(detail.forkedFrom.sessionId), sourceTurnId: detail.forkedFrom.turnId,
    targetTurnId: detail.forkedFrom.targetTurnId, operationId: detail.forkedFrom.operationId, forkedAtUnixMs: millis(detail.forkedFrom.createdAt),
  };
  return { session, turns, messages };
}
