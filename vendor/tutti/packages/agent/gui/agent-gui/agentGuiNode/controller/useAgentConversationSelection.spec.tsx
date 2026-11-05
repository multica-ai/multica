import { act, renderHook } from "@testing-library/react";
import { createTestRuntimeState } from "../../../shared/testing/createTestRuntimeState";

import { describe, expect, it, vi } from "vitest";
import type { AgentGUINodeData } from "../../../types";
import { useAgentConversationSelection } from "./useAgentConversationSelection";

describe("useAgentConversationSelection", () => {
  it("reconciles detail when a selected rail session has no cached messages", () => {
    const active = { current: "recent-session" as string | null };
    const markPending = vi.fn();
    const ensureHydrated = vi.fn();
    const setLoading = vi.fn();
    const requestReveal = vi.fn();
    const hasConversationListQuery = vi.fn(() => true);
    const { result } = renderHook(() =>
      useAgentConversationSelection({
        activation: {
          canReload: () => true,
          forget: vi.fn(),
          isPending: () => false
        },
        conversations: {
          agentTargetIdFor: () => "local:codex",
          contains: () => true
        },
        detail: {
          ensureHydrated,
          ensureStateHydrated: vi.fn(),
          isHydrated: () => false,
          isStateHydrated: () => false,
          markPending,
          setLoading
        },
        hasConversationListQuery,
        isMounted: () => true,
        onMissingConversationListQuery: vi.fn(),
        persistence: { update: vi.fn() },
        rail: {
          clearRevealRequest: vi.fn(),
          requestReveal
        },
        selection: {
          clearDetailError: vi.fn(),
          getActiveSessionId: () => active.current,
          setActiveSessionId: (agentSessionId) => {
            active.current = agentSessionId;
          },
          setComposerHome: vi.fn(),
          setIntent: vi.fn()
        }
      })
    );

    act(() =>
      result.current.selectConversation("historical-session", {
        reveal: "external-open"
      })
    );

    expect(markPending).toHaveBeenCalledWith("historical-session");
    expect(setLoading).not.toHaveBeenCalled();
    expect(hasConversationListQuery).toHaveBeenCalledOnce();
    expect(ensureHydrated).toHaveBeenCalledWith("historical-session");
    expect(requestReveal).toHaveBeenCalledWith(
      "historical-session",
      "external-open"
    );
  });

  it("reuses cached detail when selecting another hydrated session", () => {
    const active = { current: "session-1" as string | null };
    const markPending = vi.fn();
    const ensureHydrated = vi.fn();
    const ensureStateHydrated = vi.fn();
    const setLoading = vi.fn();
    const data: AgentGUINodeData = {
      agentTargetId: null,
      lastActiveAgentSessionId: active.current,
      provider: "codex"
    };
    const { result } = renderHook(() =>
      useAgentConversationSelection({
        activation: {
          canReload: () => true,
          forget: vi.fn(),
          isPending: () => false
        },
        conversations: {
          agentTargetIdFor: () => "local:codex",
          contains: () => true
        },
        detail: {
          ensureHydrated,
          ensureStateHydrated,
          isHydrated: () => true,
          isStateHydrated: () => true,
          markPending,
          setLoading
        },
        hasConversationListQuery: () => true,
        isMounted: () => true,
        onMissingConversationListQuery: vi.fn(),
        persistence: {
          update: (updater) => {
            updater(data);
          }
        },
        rail: {
          clearRevealRequest: vi.fn(),
          requestReveal: vi.fn()
        },
        selection: {
          clearDetailError: vi.fn(),
          getActiveSessionId: () => active.current,
          setActiveSessionId: (agentSessionId) => {
            active.current = agentSessionId;
          },
          setComposerHome: vi.fn(),
          setIntent: vi.fn()
        }
      })
    );

    act(() => result.current.selectConversation("session-2"));

    expect(setLoading).toHaveBeenCalledWith(false);
    expect(markPending).not.toHaveBeenCalled();
    expect(ensureHydrated).not.toHaveBeenCalled();
    expect(ensureStateHydrated).not.toHaveBeenCalled();
  });

  it("hydrates authoritative state when cached messages came from a lightweight projection", () => {
    const active = { current: "session-1" as string | null };
    const ensureHydrated = vi.fn();
    const ensureStateHydrated = vi.fn();
    const { result } = renderHook(() =>
      useAgentConversationSelection({
        activation: {
          canReload: () => true,
          forget: vi.fn(),
          isPending: () => false
        },
        conversations: {
          agentTargetIdFor: () => "local:codex",
          contains: () => true
        },
        detail: {
          ensureHydrated,
          ensureStateHydrated,
          isHydrated: () => true,
          isStateHydrated: () => false,
          markPending: vi.fn(),
          setLoading: vi.fn()
        },
        hasConversationListQuery: () => true,
        isMounted: () => true,
        onMissingConversationListQuery: vi.fn(),
        persistence: { update: vi.fn() },
        rail: {
          clearRevealRequest: vi.fn(),
          requestReveal: vi.fn()
        },
        selection: {
          clearDetailError: vi.fn(),
          getActiveSessionId: () => active.current,
          setActiveSessionId: (agentSessionId) => {
            active.current = agentSessionId;
          },
          setComposerHome: vi.fn(),
          setIntent: vi.fn()
        }
      })
    );

    act(() => result.current.selectConversation("session-2"));

    expect(ensureHydrated).not.toHaveBeenCalled();
    expect(ensureStateHydrated).toHaveBeenCalledWith("session-2");
  });

  it("selects an optimistic pending session without reloading durable detail", () => {
    const active = { current: "session-b" as string | null };
    const ensureHydrated = vi.fn();
    const setLoading = vi.fn();
    const setIntent = vi.fn();
    const { result } = renderHook(() =>
      useAgentConversationSelection({
        activation: {
          canReload: () => true,
          forget: vi.fn(),
          isPending: (agentSessionId) => agentSessionId === "session-a"
        },
        conversations: {
          agentTargetIdFor: () => "local:codex",
          contains: () => true
        },
        detail: {
          ensureHydrated,
          ensureStateHydrated: vi.fn(),
          isHydrated: () => false,
          isStateHydrated: () => false,
          markPending: vi.fn(),
          setLoading
        },
        hasConversationListQuery: () => true,
        isMounted: () => true,
        onMissingConversationListQuery: vi.fn(),
        persistence: { update: vi.fn() },
        rail: {
          clearRevealRequest: vi.fn(),
          requestReveal: vi.fn()
        },
        selection: {
          clearDetailError: vi.fn(),
          getActiveSessionId: () => active.current,
          setActiveSessionId: (agentSessionId) => {
            active.current = agentSessionId;
          },
          setComposerHome: vi.fn(),
          setIntent
        }
      })
    );

    act(() => result.current.selectConversation("session-a"));

    expect(active.current).toBe("session-a");
    expect(setIntent).toHaveBeenCalledWith({
      tag: "active",
      id: "session-a",
      source: "user-selection"
    });
    expect(setLoading).toHaveBeenCalledWith(false);
    expect(ensureHydrated).not.toHaveBeenCalled();
  });

  it("does not reload Rail or detail for an activation that cannot reload", () => {
    const active = { current: "session-b" as string | null };
    const hasConversationListQuery = vi.fn(() => true);
    const ensureHydrated = vi.fn();
    const { result } = renderHook(() =>
      useAgentConversationSelection({
        activation: {
          canReload: () => false,
          forget: vi.fn(),
          isPending: () => false
        },
        conversations: {
          agentTargetIdFor: () => "local:codex",
          contains: () => true
        },
        detail: {
          ensureHydrated,
          ensureStateHydrated: vi.fn(),
          isHydrated: () => false,
          isStateHydrated: () => false,
          markPending: vi.fn(),
          setLoading: vi.fn()
        },
        hasConversationListQuery,
        isMounted: () => true,
        onMissingConversationListQuery: vi.fn(),
        persistence: { update: vi.fn() },
        rail: {
          clearRevealRequest: vi.fn(),
          requestReveal: vi.fn()
        },
        selection: {
          clearDetailError: vi.fn(),
          getActiveSessionId: () => active.current,
          setActiveSessionId: (agentSessionId) => {
            active.current = agentSessionId;
          },
          setComposerHome: vi.fn(),
          setIntent: vi.fn()
        }
      })
    );

    act(() => result.current.selectConversation("session-a"));

    expect(hasConversationListQuery).not.toHaveBeenCalled();
    expect(ensureHydrated).not.toHaveBeenCalled();
  });

  it("hydrates a historical session after its new activation failed", () => {
    const agentSessionId = "historical-session";
    const engine = createTestRuntimeState("workspace-1");
    engine.report({
      agentSessionId,
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
    engine.report({
      commandId: "activate:activation-1",
      commandType: "session/activate",
      correlationId: "activation-1",
      errorMessage: "Initial goal failed.",
      outcome: "failed",
      type: "engine/commandResult"
    });

    const active = { current: null as string | null };
    const ensureHydrated = vi.fn();
    const setIntent = vi.fn();
    const { result } = renderHook(() =>
      useAgentConversationSelection({
        activation: {
          canReload: (sessionId) => engine.sessionLoads.canReload(sessionId),
          forget: vi.fn(),
          isPending: () => false
        },
        conversations: {
          agentTargetIdFor: () => "local:codex",
          contains: () => true
        },
        detail: {
          ensureHydrated,
          ensureStateHydrated: vi.fn(),
          isHydrated: () => false,
          isStateHydrated: () => false,
          markPending: vi.fn(),
          setLoading: vi.fn()
        },
        hasConversationListQuery: () => true,
        isMounted: () => true,
        onMissingConversationListQuery: vi.fn(),
        persistence: { update: vi.fn() },
        rail: {
          clearRevealRequest: vi.fn(),
          requestReveal: vi.fn()
        },
        selection: {
          clearDetailError: vi.fn(),
          getActiveSessionId: () => active.current,
          setActiveSessionId: (sessionId) => {
            active.current = sessionId;
          },
          setComposerHome: vi.fn(),
          setIntent
        }
      })
    );

    act(() => result.current.selectConversation(agentSessionId));

    expect(setIntent).toHaveBeenCalledWith({
      id: agentSessionId,
      source: "user-selection",
      tag: "active"
    });
    expect(ensureHydrated).toHaveBeenCalledWith(agentSessionId);
  });
});
