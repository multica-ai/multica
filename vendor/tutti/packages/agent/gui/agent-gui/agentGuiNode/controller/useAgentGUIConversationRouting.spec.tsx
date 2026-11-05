import { renderHook } from "@testing-library/react";
import { createTestRuntimeState } from "../../../shared/testing/createTestRuntimeState";

import { describe, expect, it, vi } from "vitest";
import { useAgentGUIConversationRouting } from "./useAgentGUIConversationRouting";

describe("useAgentGUIConversationRouting", () => {
  it("keeps an explicitly selected active session outside bounded rail state", () => {
    const conversationServices = createTestRuntimeState("workspace-1");
    const selectConversation = vi.fn();
    const setIntent = vi.fn();

    renderHook(() =>
      useAgentGUIConversationRouting({
        activeConversationIdRef: { current: "historical-session" },
        conversationListQuery: {},
        conversations: [],
        conversationsRef: { current: [] },
        handledOpenSessionSequenceRef: { current: null },
        hasLoadedConversations: true,
        intent: { tag: "active", id: "historical-session" },
        openSessionRequest: null,
        pendingOpenSessionRequestRef: { current: null },
        selectConversation,
        conversationServices,
        setIntent,
        transientConversation: null,
        workspaceId: "workspace-1"
      })
    );

    expect(setIntent).not.toHaveBeenCalled();
    expect(selectConversation).not.toHaveBeenCalled();
  });

  it("reconciles a persisted selection outside the bounded list after restart", () => {
    const conversationServices = createTestRuntimeState("workspace-1");
    const dispatch = vi.spyOn(conversationServices.sessionLoads, "request");
    const selectConversation = vi.fn();
    const setIntent = vi.fn();

    renderHook(() =>
      useAgentGUIConversationRouting({
        activeConversationIdRef: { current: "persisted-session" },
        conversationListQuery: {},
        conversations: [],
        conversationsRef: { current: [] },
        handledOpenSessionSequenceRef: { current: null },
        hasLoadedConversations: true,
        intent: { tag: "requested", id: "persisted-session" },
        openSessionRequest: null,
        pendingOpenSessionRequestRef: { current: null },
        selectConversation,
        conversationServices,
        setIntent,
        transientConversation: null,
        workspaceId: "workspace-1"
      })
    );

    expect(selectConversation).toHaveBeenCalledWith("persisted-session", {
      reloadConversations: false
    });
    expect(dispatch).toHaveBeenCalledWith("persisted-session", {
      state: true,
      messages: true
    });
    expect(setIntent).not.toHaveBeenCalled();
  });

  it.each(["active", "requested"] as const)(
    "does not restore a failed new activation from a stale %s intent",
    (intentTag) => {
      const conversationServices = createTestRuntimeState("workspace-1");
      conversationServices.report({
        agentSessionId: "failed-session",
        agentTargetId: "target-1",
        clientSubmitId: "submit-1",
        content: [{ type: "text", text: "hello" }],
        cwd: "/workspace",
        expiresAtUnixMs: 45_001,
        mode: "new",
        requestedAtUnixMs: 1,
        requestId: "activation-1",
        type: "activation/requested",
        workspaceId: "workspace-1"
      });
      conversationServices.report({
        commandId: "activate:activation-1",
        commandType: "session/activate",
        correlationId: "activation-1",
        errorMessage: "Cursor failed to start.",
        outcome: "failed",
        type: "engine/commandResult"
      });
      const selectConversation = vi.fn();
      const setIntent = vi.fn();

      renderHook(() =>
        useAgentGUIConversationRouting({
          // Mirrors the earlier failure-settlement effect, which clears the
          // active ref before routing runs with the previous render's intent.
          activeConversationIdRef: { current: null },
          conversationListQuery: {},
          conversations: [],
          conversationsRef: { current: [] },
          handledOpenSessionSequenceRef: { current: null },
          hasLoadedConversations: true,
          intent: { tag: intentTag, id: "failed-session" },
          openSessionRequest: null,
          pendingOpenSessionRequestRef: { current: null },
          selectConversation,
          conversationServices,
          setIntent,
          transientConversation: null,
          workspaceId: "workspace-1"
        })
      );

      expect(setIntent).toHaveBeenCalledOnce();
      expect(setIntent).toHaveBeenCalledWith({ tag: "home" });
      expect(selectConversation).not.toHaveBeenCalled();
      expect(
        conversationServices.sessionLoads.record("failed-session")
      ).toBeNull();
    }
  );

  it("keeps a failed activation open when canonical session state exists", () => {
    const conversationServices = createTestRuntimeState("workspace-1");
    conversationServices.report({
      agentSessionId: "failed-session",
      agentTargetId: "target-1",
      clientSubmitId: "submit-1",
      content: [{ type: "text", text: "hello" }],
      cwd: "/workspace",
      expiresAtUnixMs: 45_001,
      mode: "new",
      requestedAtUnixMs: 1,
      requestId: "activation-1",
      type: "activation/requested",
      workspaceId: "workspace-1"
    });
    conversationServices.report({
      commandId: "activate:activation-1",
      commandType: "session/activate",
      correlationId: "activation-1",
      errorMessage: "Initial goal failed.",
      outcome: "failed",
      type: "engine/commandResult"
    });
    conversationServices.sessions.upsertSessions([
      {
        activeTurnId: null,
        agentSessionId: "failed-session",
        createdAtUnixMs: 2,
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        title: "Historical session",
        workspaceId: "workspace-1"
      }
    ]);
    const setIntent = vi.fn();

    renderHook(() =>
      useAgentGUIConversationRouting({
        activeConversationIdRef: { current: "failed-session" },
        conversationListQuery: {},
        conversations: [],
        conversationsRef: { current: [] },
        handledOpenSessionSequenceRef: { current: null },
        hasLoadedConversations: true,
        intent: { id: "failed-session", source: "activation", tag: "active" },
        openSessionRequest: null,
        pendingOpenSessionRequestRef: { current: null },
        selectConversation: vi.fn(),
        conversationServices,
        setIntent,
        transientConversation: null,
        workspaceId: "workspace-1"
      })
    );

    expect(setIntent).not.toHaveBeenCalledWith({ tag: "home" });
  });

  it("does not let a failed activation override an explicit history selection", () => {
    const conversationServices = createTestRuntimeState("workspace-1");
    conversationServices.report({
      agentSessionId: "historical-session",
      agentTargetId: "target-1",
      clientSubmitId: "submit-1",
      content: [{ type: "text", text: "hello" }],
      cwd: "/workspace",
      expiresAtUnixMs: 45_001,
      mode: "new",
      requestedAtUnixMs: 1,
      requestId: "activation-1",
      type: "activation/requested",
      workspaceId: "workspace-1"
    });
    conversationServices.report({
      commandId: "activate:activation-1",
      commandType: "session/activate",
      correlationId: "activation-1",
      errorMessage: "Initial goal failed.",
      outcome: "failed",
      type: "engine/commandResult"
    });
    const setIntent = vi.fn();

    renderHook(() =>
      useAgentGUIConversationRouting({
        activeConversationIdRef: { current: "historical-session" },
        conversationListQuery: {},
        conversations: [],
        conversationsRef: { current: [] },
        handledOpenSessionSequenceRef: { current: null },
        hasLoadedConversations: true,
        intent: {
          id: "historical-session",
          source: "user-selection",
          tag: "active"
        },
        openSessionRequest: null,
        pendingOpenSessionRequestRef: { current: null },
        selectConversation: vi.fn(),
        conversationServices,
        setIntent,
        transientConversation: null,
        workspaceId: "workspace-1"
      })
    );

    expect(setIntent).not.toHaveBeenCalledWith({ tag: "home" });
  });
});
