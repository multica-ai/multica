import { renderHook } from "@testing-library/react";
import type {
  AgentActivityMessage,
  AgentQueuedPrompt,
  PendingActivationIntentRecord,
  PendingSubmitIntentRecord
} from "@tutti-os/agent-activity-core";
import { describe, expect, it } from "vitest";
import { readPendingConversationMessages } from "../../../state/pendingConversationMessages";

type NewPendingActivationIntentRecord = Extract<
  PendingActivationIntentRecord,
  { mode: "new" }
>;

describe("readPendingConversationMessages", () => {
  it("does not rematerialize an authoritatively retracted initial prompt", () => {
    const storedMessage = message("replacement", "replacement-submit");
    const { result } = renderHook(() =>
      readPendingConversationMessages({
        activeConversationId: "session-1",
        activePendingActivation: activation({ initialPromptRetracted: true }),
        activePendingSubmits: [],
        activeQueuedPrompts: [],
        currentUserId: "user-1",
        storedMessages: [storedMessage],
        workspaceId: "workspace-1"
      })
    );

    expect(result.current.activeMessages).toEqual([storedMessage]);
  });

  it("keeps an unretracted initial prompt optimistic until canonical confirmation", () => {
    const { result } = renderHook(() =>
      readPendingConversationMessages({
        activeConversationId: "session-1",
        activePendingActivation: activation({
          initialPromptRetracted: false
        }),
        activePendingSubmits: [],
        activeQueuedPrompts: [],
        currentUserId: "user-1",
        storedMessages: [],
        workspaceId: "workspace-1"
      })
    );

    expect(result.current.activeMessages).toHaveLength(1);
    expect(result.current.activeMessages[0]?.payload).toMatchObject({
      __agentGuiOptimisticPrompt: true,
      clientSubmitId: "initial-submit"
    });
  });

  it("keeps a dequeued optimistic image prompt after the durable assistant response", () => {
    const durableUserMessage = message("user-a", "submit-a", {
      occurredAtUnixMs: 100,
      sequence: 1,
      turnId: "turn-a"
    });
    const durableAssistantMessage = message("assistant-a", "", {
      occurredAtUnixMs: 300,
      payload: { text: "Assistant A" },
      role: "assistant",
      sequence: 2,
      turnId: "turn-a",
      version: 2
    });
    const pendingImageSubmit = pendingSubmit();
    const queuedImageSubmit = queuedPrompt();
    const { result, rerender } = renderHook(
      ({
        activeQueuedPrompts,
        storedMessages
      }: {
        activeQueuedPrompts: readonly AgentQueuedPrompt[];
        storedMessages: readonly AgentActivityMessage[];
      }) =>
        readPendingConversationMessages({
          activeConversationId: "session-1",
          activePendingActivation: null,
          activePendingSubmits: [pendingImageSubmit],
          activeQueuedPrompts,
          currentUserId: "user-1",
          storedMessages,
          workspaceId: "workspace-1"
        }),
      {
        initialProps: {
          activeQueuedPrompts: [queuedImageSubmit],
          storedMessages: [durableUserMessage, durableAssistantMessage]
        }
      }
    );

    expect(timelineEventIds(result.current.activeTimelineItems)).toEqual([
      "user-a",
      "assistant-a"
    ]);

    rerender({
      activeQueuedPrompts: [],
      storedMessages: [durableUserMessage, durableAssistantMessage]
    });

    expect(timelineEventIds(result.current.activeTimelineItems)).toEqual([
      "user-a",
      "assistant-a",
      "client-submit:user:submit-b"
    ]);

    rerender({
      activeQueuedPrompts: [],
      storedMessages: [
        durableUserMessage,
        durableAssistantMessage,
        message("client-submit:user:submit-b", "submit-b", {
          occurredAtUnixMs: 400,
          payload: {
            clientSubmitId: "submit-b",
            content: pendingImageSubmit.content,
            text: "[Image]"
          },
          sequence: 3,
          turnId: "turn-b",
          version: 3
        })
      ]
    });

    expect(timelineEventIds(result.current.activeTimelineItems)).toEqual([
      "user-a",
      "assistant-a",
      "client-submit:user:submit-b"
    ]);
  });
});

function activation(
  patch: Partial<NewPendingActivationIntentRecord>
): NewPendingActivationIntentRecord {
  return {
    agentSessionId: "session-1",
    agentTargetId: "target-1",
    clientSubmitId: "initial-submit",
    commandOutcome: "succeeded",
    commandSettledAtUnixMs: 2,
    content: [{ type: "text", text: "old prompt" }],
    cwd: "/workspace",
    errorCode: null,
    errorMessage: null,
    expiresAtUnixMs: 120_000,
    initialPromptRetracted: false,
    initialTurnExpected: true,
    lastObservedStage: "confirmed",
    mode: "new",
    requestedAtUnixMs: 1,
    requestId: "activation-1",
    snapshotObservedAtUnixMs: 2,
    snapshotOutcome: "matched",
    status: "confirmed",
    title: "Session",
    workspaceId: "workspace-1",
    ...patch
  };
}

function message(
  messageId: string,
  clientSubmitId: string,
  patch: Partial<AgentActivityMessage> = {}
): AgentActivityMessage {
  return {
    agentSessionId: "session-1",
    kind: "text",
    messageId,
    occurredAtUnixMs: 2,
    payload: { clientSubmitId, text: "replacement prompt" },
    role: "user",
    turnId: "replacement-turn",
    version: 1,
    workspaceId: "workspace-1",
    ...patch
  };
}

function pendingSubmit(): PendingSubmitIntentRecord {
  return {
    acceptedSessionVersion: null,
    agentSessionId: "session-1",
    clientSubmitId: "submit-b",
    content: [
      {
        type: "image",
        data: "aW1hZ2UtYQ==",
        mimeType: "image/png",
        name: "image-a.png"
      }
    ],
    errorCode: null,
    errorMessage: null,
    errorReason: null,
    expiresAtUnixMs: 120_000,
    requestedAtUnixMs: 200,
    status: "requested",
    turnId: null,
    workspaceId: "workspace-1"
  };
}

function queuedPrompt(): AgentQueuedPrompt {
  const pending = pendingSubmit();
  return {
    clientSubmitId: pending.clientSubmitId,
    content: pending.content,
    createdAtUnixMs: pending.requestedAtUnixMs,
    id: "queued-b"
  };
}

function timelineEventIds(
  items: ReturnType<
    typeof readPendingConversationMessages
  >["activeTimelineItems"]
): string[] {
  return items.map((item) => item.eventId);
}
