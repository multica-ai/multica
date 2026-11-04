import { act, renderHook, waitFor } from "@testing-library/react";
import type { AgentSessionEngine } from "@tutti-os/agent-activity-core";
import { useRef, useState } from "react";
import { describe, expect, it, vi } from "vitest";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import { createTestAgentSessionEngine } from "../../../shared/testing/createTestAgentSessionEngine";
import type { AgentGUINodeData } from "../../../types";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import {
  clearRolledBackAgentGUISelection,
  shouldClearMissingAgentGUISelection
} from "./useAgentGUIConversationSelectionController";
import { useAgentGUIConversationSelectionController } from "./useAgentGUIConversationSelectionController";
import { useAgentGUIConversationRouting } from "./useAgentGUIConversationRouting";
import type { ConversationIntent } from "./useAgentConversationSelection";
import type { useAgentGUIActivation } from "./useAgentGUIActivation";

describe("clearRolledBackAgentGUISelection", () => {
  it("does not clear a newer external selection", () => {
    const current = {
      lastActiveAgentSessionId: "session-newer",
      provider: "codex" as const
    };

    expect(clearRolledBackAgentGUISelection(current, "session-failed")).toBe(
      current
    );
    expect(
      clearRolledBackAgentGUISelection(current, "session-newer")
        .lastActiveAgentSessionId
    ).toBeNull();
  });

  it("persists a new selection only after canonical activation confirmation", async () => {
    const agentSessionId = "session-new";
    const data: AgentGUINodeData = {
      provider: "codex",
      lastActiveAgentSessionId: null
    };
    let persistedData = data;
    const onDataChange = vi.fn(
      (updater: (current: AgentGUINodeData) => AgentGUINodeData) => {
        persistedData = updater(persistedData);
      }
    );

    const { result } = renderHook(() => {
      const [activeConversationId, setActiveConversationId] = useState<
        string | null
      >(null);
      const [activePendingActivation, setActivePendingActivation] = useState<{
        agentSessionId: string;
        agentTargetId: string;
        errorMessage: null;
        mode: "new";
        requestId: string;
        status: "requested" | "confirmed";
      } | null>(null);
      const [intent, setIntent] = useState<ConversationIntent>({ tag: "home" });
      const [isComposerHome, setIsComposerHome] = useState(true);
      const activeConversationIdRef = useRef<string | null>(null);
      const isComposerHomeRef = useRef(true);

      useAgentGUIConversationSelectionController({
        activation: {
          clearFailure: vi.fn(),
          unactivate: vi.fn(() => Promise.resolve())
        } as unknown as ReturnType<typeof useAgentGUIActivation>,
        activeConversationId,
        activeConversationIdRef,
        activePendingActivation,
        activeSessionReconcileErrorCode: null,
        agentActivityRuntime: {} as AgentGUIRuntime,
        conversationIdsRef: { current: new Set() },
        conversationsRef: { current: [] },
        conversationListQuery: {},
        currentUserId: null,
        data,
        dataRef: { current: data },
        intent,
        isComposerHomeRef,
        isMountedRef: { current: true },
        loadDraftComposerOptions: vi.fn(),
        loadSelectedConversationMessages: vi.fn(async () => undefined),
        loadSessionState: vi.fn(),
        markSelectedConversationDetailPending: vi.fn(() => null),
        onDataChangeRef: { current: onDataChange },
        sessionEngine: {
          dispatch: vi.fn(),
          getSnapshot: vi.fn(() => ({
            pendingIntents: { activationsByRequestId: {} }
          }))
        } as unknown as AgentSessionEngine,
        setActiveConversationId,
        setDetailError: vi.fn(),
        setIntent,
        setIsComposerHome,
        setIsLoadingMessages: vi.fn(),
        setActiveMessageSession: vi.fn(),
        clearRailRevealRequest: vi.fn(),
        requestRailReveal: vi.fn(),
        transientConversation: null,
        workspaceId: "workspace-1"
      });

      return {
        activate(status: "requested" | "confirmed") {
          activeConversationIdRef.current = agentSessionId;
          setActiveConversationId(agentSessionId);
          setActivePendingActivation({
            agentSessionId,
            agentTargetId: "target-1",
            errorMessage: null,
            mode: "new",
            requestId: "activation-1",
            status
          });
          isComposerHomeRef.current = false;
          setIsComposerHome(false);
          setIntent({ tag: "active", id: agentSessionId });
        },
        activeConversationId,
        isComposerHome
      };
    });

    act(() => result.current.activate("requested"));
    expect(onDataChange).not.toHaveBeenCalled();

    act(() => result.current.activate("confirmed"));
    await waitFor(() => {
      expect(persistedData).toMatchObject({
        lastActiveAgentSessionId: agentSessionId,
        lastActiveAgentSessionIdByAgentTargetId: {
          "target-1": agentSessionId
        }
      });
    });
    expect(onDataChange).toHaveBeenCalledOnce();
  });

  it("clears only an active persisted selection rejected as session.not_found", async () => {
    const missingAgentSessionId = "session-missing";
    const data: AgentGUINodeData = {
      provider: "codex",
      lastActiveAgentSessionId: missingAgentSessionId,
      lastActiveAgentSessionIdByAgentTargetId: {
        "target-1": missingAgentSessionId,
        "target-2": "session-preserved"
      }
    };
    let persistedData = data;
    const onDataChange = vi.fn(
      (updater: (current: AgentGUINodeData) => AgentGUINodeData) => {
        persistedData = updater(persistedData);
      }
    );
    const setDetailError = vi.fn();

    const { result } = renderHook(() => {
      const [activeConversationId, setActiveConversationId] = useState<
        string | null
      >(missingAgentSessionId);
      const [intent, setIntent] = useState<ConversationIntent>({
        tag: "active",
        id: missingAgentSessionId
      });
      const [isComposerHome, setIsComposerHome] = useState(false);
      const activeConversationIdRef = useRef<string | null>(
        missingAgentSessionId
      );
      const isComposerHomeRef = useRef(false);

      useAgentGUIConversationSelectionController({
        activation: {
          clearFailure: vi.fn(),
          unactivate: vi.fn(() => Promise.resolve())
        } as unknown as ReturnType<typeof useAgentGUIActivation>,
        activeConversationId,
        activeConversationIdRef,
        activePendingActivation: null,
        activeSessionReconcileErrorCode: "session.not_found",
        agentActivityRuntime: {} as AgentGUIRuntime,
        conversationIdsRef: { current: new Set() },
        conversationsRef: { current: [] },
        conversationListQuery: {},
        currentUserId: null,
        data,
        dataRef: { current: data },
        intent,
        isComposerHomeRef,
        isMountedRef: { current: true },
        loadDraftComposerOptions: vi.fn(),
        loadSelectedConversationMessages: vi.fn(async () => undefined),
        loadSessionState: vi.fn(),
        markSelectedConversationDetailPending: vi.fn(() => null),
        onDataChangeRef: { current: onDataChange },
        sessionEngine: {
          dispatch: vi.fn(),
          getSnapshot: vi.fn(() => ({
            pendingIntents: { activationsByRequestId: {} }
          }))
        } as unknown as AgentSessionEngine,
        setActiveConversationId,
        setDetailError,
        setIntent,
        setIsComposerHome,
        setIsLoadingMessages: vi.fn(),
        setActiveMessageSession: vi.fn(),
        clearRailRevealRequest: vi.fn(),
        requestRailReveal: vi.fn(),
        transientConversation: null,
        workspaceId: "workspace-1"
      });

      return { activeConversationId, intent, isComposerHome };
    });

    await waitFor(() => {
      expect(result.current).toEqual({
        activeConversationId: null,
        intent: { tag: "home" },
        isComposerHome: true
      });
    });
    expect(persistedData).toMatchObject({
      lastActiveAgentSessionId: null,
      lastActiveAgentSessionIdByAgentTargetId: {
        "target-2": "session-preserved"
      }
    });
    expect(setDetailError).toHaveBeenCalledWith(
      "The previous agent session is no longer available."
    );
  });

  it("keeps an active selection after a transient reconcile failure", () => {
    expect(
      shouldClearMissingAgentGUISelection({
        activeConversationId: "session-1",
        currentActiveConversationId: "session-1",
        reconcileErrorCode: "request_timed_out"
      })
    ).toBe(false);
    expect(
      shouldClearMissingAgentGUISelection({
        activeConversationId: "session-1",
        currentActiveConversationId: "session-newer",
        reconcileErrorCode: "session.not_found"
      })
    ).toBe(false);
  });

  it("does not reinterpret the failed selection persistence echo as a new request", async () => {
    const failedAgentSessionId = "session-failed";
    const data: AgentGUINodeData = {
      provider: "codex",
      lastActiveAgentSessionId: failedAgentSessionId
    };
    const onDataChange = vi.fn();
    const routeSelections = vi.fn();
    const transientConversation = {
      id: failedAgentSessionId
    } as AgentGUIConversationSummary;
    const failedActivationSnapshot = {
      pendingIntents: {
        activationsByRequestId: {
          "activation-1": {
            agentSessionId: failedAgentSessionId,
            agentTargetId: "target-1",
            clientSubmitId: "submit-1",
            content: [],
            cwd: "/workspace",
            errorCode: null,
            errorMessage: "create failed",
            expiresAtUnixMs: 45_001,
            initialPromptRetracted: false,
            initialTurnExpected: false,
            mode: "new",
            requestId: "activation-1",
            requestedAtUnixMs: 1,
            status: "failed",
            title: null,
            workspaceId: "workspace-1"
          }
        }
      },
      sessionLifecycle: { sessionsById: {} }
    };

    const { result } = renderHook(() => {
      const [activeConversationId, setActiveConversationId] = useState<
        string | null
      >(failedAgentSessionId);
      const [intent, setIntent] = useState<ConversationIntent>({
        tag: "active",
        id: failedAgentSessionId
      });
      const [isComposerHome, setIsComposerHome] = useState(false);
      const activeConversationIdRef = useRef<string | null>(
        failedAgentSessionId
      );
      const isComposerHomeRef = useRef(false);
      const dataRef = useRef(data);

      useAgentGUIConversationSelectionController({
        activation: {
          clearFailure: vi.fn(),
          unactivate: vi.fn(() => Promise.resolve())
        } as unknown as ReturnType<typeof useAgentGUIActivation>,
        activeConversationId,
        activeConversationIdRef,
        activePendingActivation:
          activeConversationId === failedAgentSessionId
            ? {
                agentSessionId: failedAgentSessionId,
                agentTargetId: "target-1",
                errorMessage: "create failed",
                mode: "new",
                requestId: "activation-1",
                status: "failed"
              }
            : null,
        activeSessionReconcileErrorCode: null,
        agentActivityRuntime: {} as AgentGUIRuntime,
        conversationIdsRef: { current: new Set() },
        conversationsRef: { current: [transientConversation] },
        conversationListQuery: {},
        currentUserId: null,
        data,
        dataRef,
        intent,
        isComposerHomeRef,
        isMountedRef: { current: true },
        loadDraftComposerOptions: vi.fn(),
        loadSelectedConversationMessages: vi.fn(async () => undefined),
        loadSessionState: vi.fn(),
        markSelectedConversationDetailPending: vi.fn(() => null),
        onDataChangeRef: { current: onDataChange },
        sessionEngine: {
          dispatch: vi.fn(),
          getSnapshot: vi.fn(() => failedActivationSnapshot)
        } as unknown as AgentSessionEngine,
        setActiveConversationId,
        setDetailError: vi.fn(),
        setIntent,
        setIsComposerHome: (next) => {
          setIsComposerHome(next);
        },
        setIsLoadingMessages: vi.fn(),
        setActiveMessageSession: vi.fn(),
        clearRailRevealRequest: vi.fn(),
        requestRailReveal: vi.fn(),
        transientConversation,
        workspaceId: "workspace-1"
      });
      useAgentGUIConversationRouting({
        activeConversationIdRef,
        conversationListQuery: {},
        conversations: [],
        conversationsRef: { current: [] },
        handledOpenSessionSequenceRef: { current: null },
        hasLoadedConversations: true,
        intent,
        openSessionRequest: null,
        pendingOpenSessionRequestRef: { current: null },
        selectConversation: (agentSessionId) => {
          routeSelections(agentSessionId);
          activeConversationIdRef.current = agentSessionId;
          setActiveConversationId(agentSessionId);
          setIntent({ tag: "active", id: agentSessionId });
        },
        sessionEngine: {
          dispatch: vi.fn(),
          getSnapshot: vi.fn(() => failedActivationSnapshot)
        } as unknown as AgentSessionEngine,
        setIntent,
        transientConversation,
        workspaceId: "workspace-1"
      });

      return { activeConversationId, intent, isComposerHome };
    });

    await waitFor(() => {
      expect(result.current).toEqual({
        activeConversationId: null,
        intent: { tag: "home" },
        isComposerHome: true
      });
    });
    expect(onDataChange).toHaveBeenCalledOnce();
    expect(routeSelections).not.toHaveBeenCalled();
  });
});

describe("conversation reload ownership", () => {
  it("ensures message hydration without turning selection into a forced refresh", () => {
    const sessionEngine = createTestAgentSessionEngine("workspace-1");
    const loadSelectedConversationMessages = vi.fn(async () => undefined);
    const data: AgentGUINodeData = {
      lastActiveAgentSessionId: "session-previous",
      provider: "codex"
    };

    const { result } = renderHook(() => {
      const [activeConversationId, setActiveConversationId] = useState<
        string | null
      >("session-previous");
      const [intent, setIntent] = useState<ConversationIntent>({
        tag: "active",
        id: "session-previous"
      });
      const [, setIsComposerHome] = useState(false);
      const activeConversationIdRef = useRef<string | null>("session-previous");
      const isComposerHomeRef = useRef(false);
      const dataRef = useRef(data);

      return useAgentGUIConversationSelectionController({
        activation: {
          clearFailure: vi.fn(),
          unactivate: vi.fn(async () => undefined)
        } as unknown as ReturnType<typeof useAgentGUIActivation>,
        activeConversationId,
        activeConversationIdRef,
        activePendingActivation: null,
        activeSessionReconcileErrorCode: null,
        agentActivityRuntime: {} as AgentGUIRuntime,
        conversationIdsRef: { current: new Set(["session-next"]) },
        conversationsRef: { current: [] },
        conversationListQuery: {},
        currentUserId: null,
        data,
        dataRef,
        intent,
        isComposerHomeRef,
        isMountedRef: { current: true },
        loadDraftComposerOptions: vi.fn(),
        loadSelectedConversationMessages,
        loadSessionState: vi.fn(),
        markSelectedConversationDetailPending: (agentSessionId) =>
          agentSessionId,
        onDataChangeRef: {
          current: (updater) => {
            dataRef.current = updater(dataRef.current);
          }
        },
        sessionEngine,
        setActiveConversationId,
        setDetailError: vi.fn(),
        setIntent,
        setIsComposerHome,
        setIsLoadingMessages: vi.fn(),
        setActiveMessageSession: vi.fn(),
        clearRailRevealRequest: vi.fn(),
        requestRailReveal: vi.fn(),
        transientConversation: null,
        workspaceId: "workspace-1"
      });
    });

    act(() => result.current.selectConversation("session-next"));

    expect(loadSelectedConversationMessages).toHaveBeenCalledWith(
      "session-next"
    );
    sessionEngine.dispose();
  });
});
