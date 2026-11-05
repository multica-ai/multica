import { renderHook } from "@testing-library/react";
import type { AgentActivityMessage } from "@tutti-os/agent-activity-core";
import type { PropsWithChildren, RefObject } from "react";
import { describe, expect, it } from "vitest";
import {
  ConversationServicesProvider,
  type ConversationServices
} from "../../../conversationServices";
import { createTestRuntimeState } from "../../../shared/testing/createTestRuntimeState";
import type { AgentGUINodeData } from "../../../types";
import { useAgentGUISessionDetailTransport } from "./useAgentGUISessionDetailTransport";

describe("useAgentGUISessionDetailTransport", () => {
  it("renders with no active conversation", () => {
    const runtime = createRuntime([]);
    const wrapper = ({ children }: PropsWithChildren) => (
      <ConversationServicesProvider runtime={runtime}>
        {children}
      </ConversationServicesProvider>
    );
    const rendered = renderHook(
      () =>
        useAgentGUISessionDetailTransport({
          activeConversationId: null,
          activeConversationIdRef: ref(null),
          agentActivityRuntime: runtime,
          agentActivityRuntimeOrigin: "test",
          dataRef: ref({ provider: "codex" } as AgentGUINodeData),
          isMountedRef: ref(true),
          conversationServices: createTestRuntimeState("workspace-1"),
          sessionFamily: {
            childSessions: [],
            pendingInteractions: [],
            rootSession: null
          },
          workspaceId: "workspace-1"
        }),
      { wrapper }
    );

    expect(rendered.result.current.activeSessionView).toBeNull();
  });

  it("renders optimistic text without treating its version as history coverage", () => {
    const optimisticMessage = message();
    const runtime = createRuntime([optimisticMessage]);
    const wrapper = ({ children }: PropsWithChildren) => (
      <ConversationServicesProvider runtime={runtime}>
        {children}
      </ConversationServicesProvider>
    );
    const rendered = renderHook(
      () =>
        useAgentGUISessionDetailTransport({
          activeConversationId: "session-1",
          activeConversationIdRef: ref("session-1"),
          agentActivityRuntime: runtime,
          agentActivityRuntimeOrigin: "test",
          dataRef: ref({ provider: "codex" } as AgentGUINodeData),
          isMountedRef: ref(true),
          conversationServices: createTestRuntimeState("workspace-1"),
          sessionFamily: {
            childSessions: [],
            pendingInteractions: [],
            rootSession: null
          },
          workspaceId: "workspace-1"
        }),
      { wrapper }
    );

    expect(rendered.result.current.activeSessionView?.hasOlderMessages).toBe(
      false
    );
    expect(rendered.result.current.resolveSessionMessages("session-1")).toEqual(
      [optimisticMessage]
    );
  });
});

function createRuntime(
  messages: readonly AgentActivityMessage[]
): ConversationServices {
  return {
    origin: "test",
    transcript: {
      messages: () => messages,
      hasOlderMessages: () => false,
      conversationIds: () => ["session-1"]
    }
  } as unknown as ConversationServices;
}

function message(): AgentActivityMessage {
  return {
    agentSessionId: "session-1",
    kind: "text",
    messageId: "message-1",
    occurredAtUnixMs: 1,
    payload: { text: "streaming" },
    role: "assistant",
    status: "streaming",
    turnId: "turn-1",
    version: 0,
    workspaceId: "workspace-1"
  };
}

function ref<T>(current: T): RefObject<T> {
  return { current };
}
