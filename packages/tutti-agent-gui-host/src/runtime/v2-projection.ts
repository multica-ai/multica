import type { AgentActivityDurableMessage, AgentActivityInteraction, AgentActivityTurn } from "@tutti-os/agent-activity-core";
import type { LocalBackgroundTask } from "@multica/core/local-conversation/protocol";
import { localMessageDisplayContent } from "@multica/core/local-conversation/protocol";
import { chatAuthoredPrompt } from "@multica/core/chat/context-snapshot";
import { backgroundTaskPresentation } from "./background-task-presentation";
import type { V2Conversation, V2Item, V2Turn } from "./v2-conversation";

/** The daemon's context MCP server; its question tool is shown as an interaction instead. */
const CONTEXT_SERVER = "multica_desktop_context";

type RawMessage = Omit<AgentActivityDurableMessage, "version" | "sequence">;

export interface V2Projection {
  turns: AgentActivityTurn[];
  messages: RawMessage[];
  interactions: AgentActivityInteraction[];
  activeTurnId: string | null;
}

/** Projects v2 conversation state into the GUI's turns, messages and interactions. */
export function projectV2Conversation(
  conversation: V2Conversation,
  context: { workspaceId: string; agentSessionId: string; locale: "en" | "zh-CN" },
): V2Projection {
  const { workspaceId, agentSessionId, locale } = context;
  const messages: RawMessage[] = [];
  const base = { workspaceId, agentSessionId };

  for (const message of conversation.messages.values()) {
    const content = localMessageDisplayContent({ role: "user", content: chatAuthoredPrompt(message.content), attachmentIds: message.attachmentIds });
    messages.push({
      ...base, messageId: message.id, turnId: message.turnId, role: "user", kind: "text",
      occurredAtUnixMs: millis(message.createdAt),
      payload: {
        text: content, content,
        ...(message.requestId ? { clientSubmitId: message.requestId } : {}),
        ...(message.attachmentIds.length ? {
          attachments: message.attachmentIds.map((attachmentId) => ({ attachmentId })),
          content: [{ type: "text", text: content }, ...message.attachmentIds.map((attachmentId) => ({ type: "image", attachmentId }))],
        } : {}),
      },
    });
  }

  for (const turn of conversation.turns.values()) {
    const allItems = conversation.turnItems(turn.id);
    const items = allItems.filter((item) => !text(item.state.parent_tool_use_id));
    const lastText = items.findLast((item) => item.kind === "assistant_text")?.id;
    for (const item of items) {
      const message = item.kind === "background_task"
        ? backgroundTask(item, locale, allItems)
        : projectItem(item, turn, item.id === lastText, locale);
      if (message) messages.push({ ...base, turnId: turn.id, role: "assistant", occurredAtUnixMs: millis(item.startedAt), ...message });
    }
  }

  const interactions: AgentActivityInteraction[] = [];
  for (const interaction of conversation.interactions.values()) {
    if (interaction.kind !== "question") continue;
    const input = { requestId: text(interaction.request.request_id), questions: interaction.request.questions ?? [] };
    const answers = interaction.answer;
    const output = answers ? {
      answersByQuestionId: answers,
      answers: Object.values(answers).map((value) => Array.isArray(value) ? value.join(", ") : value),
    } : undefined;
    const status = interaction.status === "pending" ? "pending" : interaction.status === "answered" ? "answered" : "superseded";
    messages.push({
      ...base, turnId: interaction.turnId, messageId: interaction.id, role: "assistant", kind: "tool_call",
      status: status === "pending" ? "running" : status === "answered" ? "completed" : "failed",
      occurredAtUnixMs: millis(interaction.createdAt),
      payload: {
        callId: interaction.id, callType: "interactive", toolName: "AskUserQuestion", input, ...(output ? { output } : {}),
        metadata: { callType: "interactive", interactiveKind: "ask-user", toolName: "AskUserQuestion" },
      },
    });
    interactions.push({
      agentSessionId, turnId: interaction.turnId, requestId: interaction.id, kind: "question", toolName: "AskUserQuestion",
      status, input, output, createdAtUnixMs: millis(interaction.createdAt), updatedAtUnixMs: millis(interaction.updatedAt),
    });
  }

  const waiting = new Set(interactions.filter((item) => item.status === "pending").map((item) => item.turnId));
  const turns = [...conversation.turns.values()].map((turn) => projectTurn(turn, agentSessionId, waiting.has(turn.id)));
  const active = [...conversation.turns.values()].filter((turn) => turn.status === "running").sort((a, b) => millis(b.startedAt) - millis(a.startedAt))[0];
  return { turns, messages, interactions, activeTurnId: active?.id ?? null };
}

function projectTurn(turn: V2Turn, agentSessionId: string, waiting: boolean): AgentActivityTurn {
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

type ItemMessage = Pick<RawMessage, "messageId" | "kind" | "payload"> & Partial<Pick<RawMessage, "status" | "semantics">>;

function projectItem(item: V2Item, turn: V2Turn, finalText: boolean, locale: "en" | "zh-CN"): ItemMessage | null {
  const state = item.state;
  const status = itemStatus(item.status);
  switch (item.kind) {
    case "assistant_text":
      return {
        messageId: item.id, kind: "text", payload: { content: text(state.text) },
        // The turn's last answer settles it once the turn has ended.
        semantics: { userVisibleAssistantResponse: true, turnSettling: finalText && turn.status !== "running" },
      };
    case "reasoning": {
      const running = item.status === "in_progress" && turn.status === "running";
      // The transcript drops a reasoning row without text, and summaries can be off entirely.
      const pending = running ? (locale === "zh-CN" ? "思考中" : "Thinking") : "";
      return { messageId: item.id, kind: "reasoning", ...(running ? { status } : {}), payload: { content: text(state.text) || pending } };
    }
    case "error":
      return { messageId: item.id, kind: "error", payload: { content: errorMessage(state.error) } };
    case "compaction": {
      const commandStatus = item.status === "in_progress" ? "running" : item.status === "completed" ? "completed" : item.status === "cancelled" ? "canceled" : "failed";
      const detail = errorMessage(state.error) || (item.status === "failed" ? turn.error?.message ?? "" : "");
      return {
        messageId: item.id, kind: "text", status: commandStatus,
        payload: { kind: "agent_system_notice", noticeKind: "system_notice", ...(detail ? { detail } : {}) },
        semantics: { noticeCommand: "compact", noticeCommandStatus: commandStatus, userVisibleAssistantResponse: false, turnSettling: false },
      };
    }
    case "command":
      return toolCall(item.id, status, "Bash", { command: text(state.command) }, text(state.output));
    case "file_change":
      return toolCall(item.id, status, "Edit", { changes: state.changes ?? [] }, JSON.stringify(state.changes ?? []));
    case "tool_call": {
      const tool = text(state.tool);
      const server = text(state.server);
      if (server === CONTEXT_SERVER && tool === "ask_user_question") return null;
      const failure = state.error ? errorMessage(state.error) : "";
      return toolCall(item.id, status, server ? `mcp__${server}__${tool}` : tool || "Tool", state.input, text(state.output) || failure);
    }
    case "image": {
      const attachmentId = text(state.attachment_id);
      if (!attachmentId) return null;
      const mimeType = text(state.mime_type);
      return {
        messageId: item.id, kind: "text",
        payload: { content: [{ type: "image", attachmentId, mimeType }], attachments: [{ attachmentId, mimeType }] },
        semantics: { userVisibleAssistantResponse: false, turnSettling: false },
      };
    }
    case "background_task":
      return backgroundTask(item, locale);
  }
  return null;
}

function toolCall(id: string, status: string, toolName: string, input: unknown, output: string): ItemMessage {
  return {
    messageId: id, kind: "tool_call", status,
    payload: { callId: id, toolName, ...(input !== undefined ? { input } : {}), ...(status === "running" && !output ? {} : { output }) },
  };
}

function backgroundTask(item: V2Item, locale: "en" | "zh-CN", items: V2Item[] = []): ItemMessage {
  const state = item.state;
  const usage = record(state.usage);
  const status: LocalBackgroundTask["status"] = item.status === "in_progress" ? "running" : item.status === "completed" ? "completed" : item.status === "failed" ? "failed" : "cancelled";
  const task: LocalBackgroundTask = {
    id: text(state.task_id) || item.id, description: text(state.description), status,
    summary: text(state.summary) || undefined, progressSummary: text(state.progress_summary) || undefined,
    output: text(state.output) || undefined, createdAt: item.startedAt, updatedAt: item.updatedAt,
    ...(status === "running" ? {} : { completedAt: item.updatedAt }),
    ...(usage ? { usage: { totalTokens: number(usage.total_tokens), toolUses: number(usage.tool_uses), durationMs: number(usage.duration_ms) } } : {}),
  };
  const labels = locale === "zh-CN"
    ? { running: "后台任务运行中", completed: "后台任务已完成", failed: "后台任务失败", cancelled: "后台任务已停止" }
    : { running: "Background task running", completed: "Background task completed", failed: "Background task failed", cancelled: "Background task stopped" };
  const title = `${labels[status]} · ${task.description}`;
  const { progress, detail } = backgroundTaskPresentation(task, [], locale);
  const parent = text(state.tool_use_id);
  const transcript = parent ? items.filter((child) => child.state.parent_tool_use_id === parent && child.kind !== "reasoning")
    .sort((a, b) => number(a.state.item_order ?? a.order) - number(b.state.item_order ?? b.order)) : [];
  const spawn = parent ? items.find((call) => call.id === parent) : undefined;
  const input = record(spawn?.state.input);
  const prompt = text(input?.prompt);
  const subagentTranscript = {
    locale, prompt,
    items: transcript.map((child) => ({
      id: child.id, kind: child.kind, status: child.status === "in_progress" && status !== "running" ? "cancelled" : child.status,
      text: text(child.state.text), tool: child.kind === "command" ? "Bash" : text(child.state.tool),
      input: child.state.input, output: text(child.state.output) || errorMessage(child.state.error),
      attachmentId: text(child.state.attachment_id), mimeType: text(child.state.mime_type),
    })),
  };
  return {
    messageId: item.id, kind: "text", status,
    payload: { kind: "agent_system_notice", noticeKind: "background_task", title, content: progress || title, ...(detail ? { detail } : {}),
      ...(state.task_type !== "local_bash" && (state.task_type === "local_agent" || parent || !detail) ? { subagentTranscript } : {}),
    },
    semantics: { userVisibleAssistantResponse: false, turnSettling: false },
  };
}

function itemStatus(status: string): string {
  return status === "in_progress" ? "running" : status === "completed" ? "completed" : "failed";
}

function errorMessage(value: unknown): string {
  return text(record(value)?.message);
}

function text(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function number(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function millis(value: string): number {
  const result = Date.parse(value);
  return Number.isFinite(result) ? result : 0;
}
