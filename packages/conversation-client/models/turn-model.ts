import { action, makeObservable, observable } from "mobx";
import type { LocalBackgroundTask } from "@multica/core/local-conversation/protocol";
import { backgroundTaskPresentation } from "./background-task-presentation";
import type { ConversationLocale, ConversationMessage } from "./conversation-message";
import type { ItemModel } from "./item-model";
import { errorMessage, millis, number, record, text } from "./values";

/** The daemon's context MCP server; its question tool is shown as an interaction instead. */
const CONTEXT_SERVER = "multica_desktop_context";

export type TurnStatus = "running" | "completed" | "failed" | "cancelled";
export interface TurnError { kind: string; message: string }

export interface TurnInit {
  id: string;
  index: number;
  requestId: string;
  status: TurnStatus;
  startedAt: string;
  completedAt: string | null;
  error: TurnError | null;
}

type ItemMessage = Pick<ConversationMessage, "messageId" | "kind" | "payload"> & Partial<Pick<ConversationMessage, "status" | "semantics">>;

/**
 * One turn. Its items belong to the conversation, because an open background
 * item can point at a turn that is not loaded, so the turn looks them up by id.
 */
export class TurnModel {
  readonly id: string;
  readonly index: number;
  readonly requestId: string;
  readonly startedAt: string;

  status: TurnStatus;
  completedAt: string | null;
  error: TurnError | null;

  constructor(private readonly conversation: { turnItems(turnId: string): ItemModel[] }, init: TurnInit) {
    this.id = init.id;
    this.index = init.index;
    this.requestId = init.requestId;
    this.startedAt = init.startedAt;
    this.status = init.status;
    this.completedAt = init.completedAt;
    this.error = init.error;
    makeObservable(this, {
      status: observable,
      completedAt: observable,
      error: observable.ref,
      complete: action,
    });
  }

  complete(status: TurnStatus, at: string, error: TurnError | null): void {
    this.status = status;
    this.completedAt = at;
    this.error = error;
  }

  /** Items of this turn in the order they started. */
  items(): ItemModel[] { return this.conversation.turnItems(this.id); }

  /**
   * The rows this turn's items produce. Not one row per item: some produce none,
   * children feed their background task, and settling depends on the turn's state.
   */
  assistantMessages(locale: ConversationLocale): ConversationMessage[] {
    const allItems = this.items();
    const items = allItems.filter((item) => !text(item.state.parent_tool_use_id));
    const lastText = items.findLast((item) => item.kind === "assistant_text")?.id;
    const messages: ConversationMessage[] = [];
    for (const item of items) {
      const message = item.kind === "background_task"
        ? backgroundTask(item, locale, allItems)
        : this.itemMessage(item, item.id === lastText, locale);
      if (message) messages.push({ turnId: this.id, role: "assistant", occurredAtUnixMs: millis(item.startedAt), ...message });
    }
    return messages;
  }

  private itemMessage(item: ItemModel, finalText: boolean, locale: ConversationLocale): ItemMessage | null {
    const state = item.state;
    const status = itemStatus(item.status);
    switch (item.kind) {
      case "assistant_text":
        return {
          messageId: item.id, kind: "text", payload: { content: item.text },
          // The turn's last answer settles it once the turn has ended.
          semantics: { userVisibleAssistantResponse: true, turnSettling: finalText && this.status !== "running" },
        };
      case "reasoning": {
        const running = item.status === "in_progress" && this.status === "running";
        // The transcript drops a reasoning row without text, and summaries can be off entirely.
        const pending = running ? (locale === "zh-CN" ? "思考中" : "Thinking") : "";
        return { messageId: item.id, kind: "reasoning", ...(running ? { status } : {}), payload: { content: item.text || pending } };
      }
      case "error":
        return { messageId: item.id, kind: "error", payload: { content: errorMessage(state.error) } };
      case "compaction": {
        const commandStatus = item.status === "in_progress" ? "running" : item.status === "completed" ? "completed" : item.status === "cancelled" ? "canceled" : "failed";
        const detail = errorMessage(state.error) || (item.status === "failed" ? this.error?.message ?? "" : "");
        return {
          messageId: item.id, kind: "text", status: commandStatus,
          payload: { kind: "agent_system_notice", noticeKind: "system_notice", ...(detail ? { detail } : {}) },
          semantics: { noticeCommand: "compact", noticeCommandStatus: commandStatus, userVisibleAssistantResponse: false, turnSettling: false },
        };
      }
      case "command":
        return toolCall(item.id, status, "Bash", { command: text(state.command) }, item.output);
      case "file_change":
        return toolCall(item.id, status, "Edit", { changes: state.changes ?? [] }, JSON.stringify(state.changes ?? []));
      case "tool_call": {
        const tool = text(state.tool);
        const server = text(state.server);
        if (server === CONTEXT_SERVER && tool === "ask_user_question") return null;
        const failure = state.error ? errorMessage(state.error) : "";
        return toolCall(item.id, status, server ? `mcp__${server}__${tool}` : tool || "Tool", state.input, item.output || failure);
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
    }
    return null;
  }
}

function toolCall(id: string, status: string, toolName: string, input: unknown, output: string): ItemMessage {
  return {
    messageId: id, kind: "tool_call", status,
    payload: { callId: id, toolName, ...(input !== undefined ? { input } : {}), ...(status === "running" && !output ? {} : { output }) },
  };
}

function backgroundTask(item: ItemModel, locale: ConversationLocale, items: ItemModel[]): ItemMessage {
  const state = item.state;
  const usage = record(state.usage);
  const status: LocalBackgroundTask["status"] = item.status === "in_progress" ? "running" : item.status === "completed" ? "completed" : item.status === "failed" ? "failed" : "cancelled";
  const task: LocalBackgroundTask = {
    id: text(state.task_id) || item.id, description: text(state.description), status,
    summary: text(state.summary) || undefined, progressSummary: text(state.progress_summary) || undefined,
    output: item.output || undefined, createdAt: item.startedAt, updatedAt: item.updatedAt,
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
      text: child.text, tool: child.kind === "command" ? "Bash" : text(child.state.tool),
      input: child.state.input, output: child.output || errorMessage(child.state.error),
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
