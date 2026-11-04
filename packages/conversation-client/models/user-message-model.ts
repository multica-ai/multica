import { localMessageDisplayContent } from "@multica/core/local-conversation/protocol";
import { chatAuthoredPrompt } from "@multica/core/chat/context-snapshot";
import type { ConversationMessage } from "./conversation-message";
import { millis } from "./values";

export interface UserMessageInit {
  id: string;
  requestId: string;
  turnId: string | null;
  content: string;
  attachmentIds: string[];
  steered: boolean;
  failed: boolean;
  createdAt: string;
}

/** What the user sent. It does not change once recorded, so nothing here is observable. */
export class UserMessageModel {
  readonly id: string;
  readonly requestId: string;
  readonly turnId: string | null;
  readonly content: string;
  readonly attachmentIds: string[];
  readonly steered: boolean;
  readonly failed: boolean;
  readonly createdAt: string;
  private row?: ConversationMessage;

  constructor(init: UserMessageInit) {
    this.id = init.id;
    this.requestId = init.requestId;
    this.turnId = init.turnId;
    this.content = init.content;
    this.attachmentIds = init.attachmentIds;
    this.steered = init.steered;
    this.failed = init.failed;
    this.createdAt = init.createdAt;
  }

  toMessage(): ConversationMessage { return this.row ??= this.buildMessage(); }

  private buildMessage(): ConversationMessage {
    const content = localMessageDisplayContent({ role: "user", content: chatAuthoredPrompt(this.content), attachmentIds: this.attachmentIds });
    return {
      messageId: this.id, turnId: this.turnId, role: "user", kind: "text",
      occurredAtUnixMs: millis(this.createdAt),
      payload: {
        text: content, content,
        ...(this.requestId ? { clientSubmitId: this.requestId } : {}),
        ...(this.attachmentIds.length ? {
          attachments: this.attachmentIds.map((attachmentId) => ({ attachmentId })),
          content: [{ type: "text", text: content }, ...this.attachmentIds.map((attachmentId) => ({ type: "image", attachmentId }))],
        } : {}),
      },
    };
  }
}
