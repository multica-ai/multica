import { describe, expect, it } from "vitest";
import {
  MAX_CHAT_OUTBOX_ATTEMPTS,
  nextChatOutboxItem,
  nextFailedChatOutboxItem,
  permanentlyFailedChatOutboxItem,
  type ChatOutboxItem,
} from "./chat-outbox";

const NOW = new Date("2026-08-12T00:00:00.000Z");

function item(overrides: Partial<ChatOutboxItem> = {}): ChatOutboxItem {
  return {
    clientId: "00000000-0000-4000-8000-000000000001",
    sessionId: "session-1",
    workspaceSlug: "workspace",
    userId: "user-1",
    content: "Hello",
    attachmentIds: [],
    createdAt: "2026-08-12T00:00:00.000Z",
    status: "queued",
    attemptCount: 0,
    retryable: true,
    lastError: null,
    nextAttemptAt: null,
    ...overrides,
  };
}

describe("chat outbox state machine", () => {
  it("enforces FIFO and never skips an unconfirmed head", () => {
    const first = item({ status: "failed" });
    const second = item({
      clientId: "00000000-0000-4000-8000-000000000002",
      createdAt: "2026-08-12T00:00:01.000Z",
    });

    expect(nextChatOutboxItem([second, first], "session-1", NOW)).toBeNull();
  });

  it("applies exponential backoff and stops at the retry ceiling", () => {
    const afterFirstFailure = nextFailedChatOutboxItem(item(), "Network unavailable", NOW);
    expect(afterFirstFailure).toMatchObject({
      status: "queued",
      attemptCount: 1,
      nextAttemptAt: "2026-08-12T00:00:01.000Z",
    });
    expect(nextChatOutboxItem([afterFirstFailure], "session-1", NOW)).toBeNull();

    const final = nextFailedChatOutboxItem(
      item({ attemptCount: MAX_CHAT_OUTBOX_ATTEMPTS - 1 }),
      "Network unavailable",
      NOW,
    );
    expect(final).toMatchObject({
      status: "failed",
      attemptCount: MAX_CHAT_OUTBOX_ATTEMPTS,
      retryable: false,
      nextAttemptAt: null,
    });
  });

  it("moves 4xx failures directly to failed", () => {
    const failed = permanentlyFailedChatOutboxItem(item(), "Chat session is archived");
    expect(failed).toMatchObject({
      status: "failed",
      attemptCount: 1,
      retryable: false,
      lastError: "Chat session is archived",
      nextAttemptAt: null,
    });
  });
});
