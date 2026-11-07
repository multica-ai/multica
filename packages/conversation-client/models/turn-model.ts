import { action, makeObservable, observable } from "mobx";
import type { LocalBackgroundTask } from "@multica/core/local-conversation/protocol";
import { backgroundTaskPresentation } from "./background-task-presentation";
import type { ConversationLocale, ConversationMessage } from "./conversation-message";
import type { InteractionModel } from "./interaction-model";
import type { ItemModel } from "./item-model";
import type { UserMessageModel } from "./user-message-model";
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

  /** Each item's row and what it was built from, so an unchanged item keeps its object. */
  private readonly rows = new Map<string, { key: string; row: ConversationMessage | null }>();

  constructor(private readonly conversation: {
    turnItems(turnId: string): readonly ItemModel[];
    turnMessages(turnId: string): readonly UserMessageModel[];
    turnInteractions(turnId: string): readonly InteractionModel[];
  }, init: TurnInit) {
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
  items(): readonly ItemModel[] { return this.conversation.turnItems(this.id); }

  /**
   * This turn's transcript: the prompt first, then output by its start time, so a
   * late event cannot move ahead of the prompt that started the turn.
   */
  messages(locale: ConversationLocale): ConversationMessage[] {
    const rows = this.conversation.turnMessages(this.id).map((message) => message.toMessage());
    rows.push(...this.assistantMessages(locale));
    for (const interaction of this.conversation.turnInteractions(this.id)) if (interaction.kind === "question") rows.push(interaction.toMessage());
    return rows.sort((a, b) => Number(b.role === "user") - Number(a.role === "user") || a.occurredAtUnixMs - b.occurredAtUnixMs);
  }

  /**
   * The rows this turn's items produce. Not one row per item: some produce none,
   * children feed their background task, and settling depends on the turn's state.
   */
  assistantMessages(locale: ConversationLocale): ConversationMessage[] {
    const allItems = this.items();
    const items = allItems.filter((item) => !text(item.state.parent_tool_use_id));
    const lastText = items.findLast((item) => item.kind === "assistant_text")?.id;
    // A background task's row shows its children, so it follows every item of the turn.
    const newest = allItems.reduce((seq, item) => Math.max(seq, item.lastSeq), 0);
    const messages: ConversationMessage[] = [];
    for (const item of items) {
      const background = item.kind === "background_task";
      const final = item.id === lastText;
      const key = [background ? newest : item.lastSeq, this.status, final, this.error?.message ?? "", locale].join("|");
      let entry = this.rows.get(item.id);
      if (entry?.key !== key) {
        const message = background ? backgroundTask(item, locale, allItems) : this.itemMessage(item, final, locale);
        entry = { key, row: message && { turnId: this.id, role: "assistant", occurredAtUnixMs: millis(item.startedAt), ...message } };
        this.rows.set(item.id, entry);
      }
      if (entry.row) messages.push(entry.row);
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
        return toolCall(item.id, status, "Bash", {
          command: text(state.command), ...(text(state.cwd) ? { cwd: state.cwd } : {}),
        }, {
          text: item.output,
          ...(typeof state.exit_code === "number" ? { exitCode: state.exit_code } : {}),
          ...(typeof state.duration_ms === "number" ? { durationMs: state.duration_ms } : {}),
        }, state.error);
      case "file_change":
        return toolCall(item.id, status, "Edit", { changes: state.changes ?? [] }, { changes: state.changes ?? [] }, state.error);
      case "tool_call": {
        const tool = text(state.tool);
        const server = text(state.server);
        if (server === CONTEXT_SERVER && tool === "ask_user_question") return null;
        const failure = state.error ? errorMessage(state.error) : "";
        return toolCall(item.id, status, server ? `mcp__${server}__${tool}` : tool || "Tool", state.input, item.output || failure, state.error);
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

function toolCall(id: string, status: string, toolName: string, input: unknown, output: string | Record<string, unknown>, error?: unknown): ItemMessage {
  const failure = record(error);
  return {
    messageId: id, kind: "tool_call", status,
    payload: {
      callId: id, toolName, ...(input !== undefined ? { input } : {}),
      ...(status === "running" && !output ? {} : { output: typeof output === "string" ? toolOutput(toolName, output) : output }),
      ...(failure ? { error: { ...failure, text: errorMessage(failure) } } : {}),
    },
  };
}

function toolOutput(toolName: string, output: string): Record<string, unknown> {
  // ImageGeneration reports a JSON object; command and MCP output remain literal text.
  if (toolName === "ImageGeneration") {
    try {
      const value = record(JSON.parse(output));
      if (value) return value;
    } catch { /* Retain malformed output as diagnostic text. */ }
  }
  return { text: output };
}

function backgroundTask(item: ItemModel, locale: ConversationLocale, items: readonly ItemModel[]): ItemMessage {
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
  return status === "in_progress" ? "running" : status === "completed" ? "completed" : status === "cancelled" ? "canceled" : "failed";
}
