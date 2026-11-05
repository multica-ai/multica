import { act, renderHook, waitFor } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import type { ConversationServices } from "../../../conversationServices";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import { useAgentGUIConversationDeletion } from "./useAgentGUIConversationDeletion";

const targetConversation: AgentGUIConversationSummary = {
  cwd: "/workspace",
  id: "session-1",
  provider: "codex",
  sortTimeUnixMs: 2,
  status: "completed",
  title: "Session 1",
  titleFallback: null,
  updatedAtUnixMs: 2,
  userId: "user-1"
};

const nextConversation: AgentGUIConversationSummary = {
  ...targetConversation,
  id: "session-2",
  sortTimeUnixMs: 1,
  title: "Session 2",
  updatedAtUnixMs: 1
};

function createInput(agentActivityRuntime: ConversationServices) {
  const activeConversationIdRef = { current: targetConversation.id };
  const unactivate = vi.fn(async () => {});
  const toastError = vi.fn();
  const input = {
    activeConversationIdRef,
    activation: { unactivate } as never,
    agentActivityRuntime,
    agentHostApi: { toast: { error: toastError } } as never,
    conversations: [targetConversation, nextConversation],
    deleteAgentSessionView: vi.fn(),
    isDeletingConversation: false,
    pendingDeleteConversation: targetConversation,
    persistActiveConversation: vi.fn(),
    removeConversations: vi.fn(),
    sessionViewRef: (agentSessionId: string | null | undefined) => ({
      agentSessionId,
      origin: "local",
      workspaceId: "workspace-1"
    }),
    setActiveConversationId: vi.fn(),
    setDetailError: vi.fn(),
    setDraftByScopeKey: vi.fn(),
    setIntent: vi.fn(),
    setIsDeletingConversation: vi.fn(),
    setIsLoadingMessages: vi.fn(),
    setPendingDeleteConversation: vi.fn(),
    submittedDraftSnapshotsRef: { current: {} },
    workspaceId: "workspace-1"
  };
  return { input, toastError, unactivate };
}

describe("useAgentGUIConversationDeletion", () => {
  it("returns home only after deleting the active session", async () => {
    let committedActiveConversationId: string | null = targetConversation.id;
    let activeConversationIdObservedByDelete: string | null = null;
    const deleteSession = vi.fn(async () => {
      activeConversationIdObservedByDelete = committedActiveConversationId;
      return {
        cleanupFailed: false,
        removed: true,
        removedMessages: 0
      };
    });
    const { input, unactivate } = createInput({
      mutations: { deleteSessions: deleteSession }
    } as unknown as ConversationServices);
    const { result } = renderHook(() => {
      const [activeConversationId, setActiveConversationId] = useState<
        string | null
      >(targetConversation.id);
      committedActiveConversationId = activeConversationId;
      return {
        activeConversationId,
        ...useAgentGUIConversationDeletion({
          ...input,
          setActiveConversationId
        })
      };
    });

    act(() => result.current.confirmDeleteConversation());

    await waitFor(() => expect(deleteSession).toHaveBeenCalledTimes(1));
    expect(activeConversationIdObservedByDelete).toBe(targetConversation.id);
    expect(deleteSession).toHaveBeenCalledWith({
      agentSessionIds: [targetConversation.id]
    });
    await waitFor(() =>
      expect(input.deleteAgentSessionView).toHaveBeenCalledTimes(1)
    );
    expect(result.current.activeConversationId).toBeNull();
    expect(input.persistActiveConversation).toHaveBeenCalledWith(null);
    expect(input.setIntent).toHaveBeenCalledWith({ tag: "home" });
    expect(input.setIsLoadingMessages).toHaveBeenCalledWith(false);
    expect(unactivate).toHaveBeenCalledWith(targetConversation.id);
    expect(input.removeConversations).toHaveBeenCalledWith([
      targetConversation.id
    ]);
  });

  it("keeps the current session and rail state when deletion fails", async () => {
    const deleteSession = vi.fn(async () => {
      throw new Error("delete failed");
    });
    const { input, toastError, unactivate } = createInput({
      mutations: { deleteSessions: deleteSession }
    } as unknown as ConversationServices);
    const { result } = renderHook(() => {
      const [activeConversationId, setActiveConversationId] = useState<
        string | null
      >(targetConversation.id);
      return {
        activeConversationId,
        ...useAgentGUIConversationDeletion({
          ...input,
          setActiveConversationId
        })
      };
    });

    act(() => result.current.confirmDeleteConversation());

    await waitFor(() => expect(toastError).toHaveBeenCalledTimes(1));
    expect(result.current.activeConversationId).toBe(targetConversation.id);
    expect(input.activeConversationIdRef.current).toBe(targetConversation.id);
    expect(input.persistActiveConversation).not.toHaveBeenCalled();
    expect(input.setIntent).not.toHaveBeenCalled();
    expect(input.setIsLoadingMessages).not.toHaveBeenCalled();
    expect(unactivate).not.toHaveBeenCalled();
    expect(input.deleteAgentSessionView).not.toHaveBeenCalled();
    expect(input.removeConversations).not.toHaveBeenCalled();
    expect(input.setIsDeletingConversation).toHaveBeenLastCalledWith(false);
  });

  it("surfaces a protected Tutti execution conflict without selection side effects", async () => {
    const conflict = {
      code: "workspace_issue_resource_exists",
      reason: "tutti_execution_active",
      details: {
        protectedIssues: [
          {
            executionId: "execution-1",
            issueId: "issue-1",
            sourceSessionId: targetConversation.id,
            status: "running"
          }
        ]
      }
    };
    const deleteSession = vi.fn(async () => {
      throw conflict;
    });
    const { input, toastError, unactivate } = createInput({
      mutations: { deleteSessions: deleteSession }
    } as unknown as ConversationServices);
    const { result } = renderHook(() => useAgentGUIConversationDeletion(input));

    act(() => result.current.confirmDeleteConversation());

    await waitFor(() =>
      expect(result.current.protectedDeleteConflict).toEqual(conflict.details)
    );
    expect(input.activeConversationIdRef.current).toBe(targetConversation.id);
    expect(input.persistActiveConversation).not.toHaveBeenCalled();
    expect(input.setIntent).not.toHaveBeenCalled();
    expect(input.removeConversations).not.toHaveBeenCalled();
    expect(input.deleteAgentSessionView).not.toHaveBeenCalled();
    expect(unactivate).not.toHaveBeenCalled();
    expect(toastError).not.toHaveBeenCalled();
  });
});
