import { action, computed, makeObservable, observable } from "mobx";
import type { ConversationMessage } from "./conversation-message";
import { millis, text } from "./values";

export interface InteractionInit {
  id: string;
  turnId: string;
  kind: string;
  request: Record<string, unknown>;
  createdAt: string;
}

/** A question the provider asked, and its answer once there is one. */
export class InteractionModel {
  readonly id: string;
  readonly turnId: string;
  readonly kind: string;
  readonly request: Record<string, unknown>;
  readonly createdAt: string;

  status: "pending" | "answered" | "cancelled" = "pending";
  answer: Record<string, unknown> | null = null;
  updatedAt: string;

  constructor(init: InteractionInit) {
    this.id = init.id;
    this.turnId = init.turnId;
    this.kind = init.kind;
    this.request = init.request;
    this.createdAt = init.createdAt;
    this.updatedAt = init.createdAt;
    makeObservable(this, {
      status: observable,
      answer: observable.ref,
      updatedAt: observable,
      resolve: action,
      output: computed,
    });
  }

  resolve(status: "answered" | "cancelled", answer: Record<string, unknown> | null, at: string): void {
    this.status = status;
    this.answer = answer;
    this.updatedAt = at;
  }

  get input() {
    return { requestId: text(this.request.request_id), questions: this.request.questions ?? [] };
  }

  get output() {
    const answers = this.answer;
    return answers ? {
      answersByQuestionId: answers,
      answers: Object.values(answers).map((value) => Array.isArray(value) ? value.join(", ") : value),
    } : undefined;
  }

  toMessage(): ConversationMessage {
    const output = this.output;
    return {
      turnId: this.turnId, messageId: this.id, role: "assistant", kind: "tool_call",
      status: this.status === "pending" ? "running" : this.status === "answered" ? "completed" : "failed",
      occurredAtUnixMs: millis(this.createdAt),
      payload: {
        callId: this.id, callType: "interactive", toolName: "AskUserQuestion", input: this.input, ...(output ? { output } : {}),
        metadata: { callType: "interactive", interactiveKind: "ask-user", toolName: "AskUserQuestion" },
      },
    };
  }
}
