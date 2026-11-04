import { renderHook } from "@testing-library/react";
import {
  AGENT_CAPABILITY_KEYS,
  normalizeAgentActivitySession,
  type AgentActivityComposerOptions,
  type AgentActivitySessionCapabilities,
  type CanonicalAgentSession
} from "@tutti-os/agent-activity-core";
import { describe, expect, it } from "vitest";
import type { AgentGUIComposerOptionsReader } from "./agentGuiController.providerHelpers";
import { useAgentGUIComposerCapabilities } from "./useAgentGUIComposerCapabilities";

function composerWith(
  options: Record<string, AgentActivityComposerOptions>
): AgentGUIComposerOptionsReader {
  return {
    options: (targetKey) => options[targetKey?.trim() ?? ""] ?? null,
    status: () => undefined
  };
}

describe("useAgentGUIComposerCapabilities", () => {
  function engineSession(input: {
    agentTargetId?: string;
    agentSessionId?: string;
    capabilities?: AgentActivitySessionCapabilities;
    provider?: string;
    usage: CanonicalAgentSession["usage"];
  }): CanonicalAgentSession {
    const normalized = normalizeAgentActivitySession({
      ...{
        activeTurnId: null,
        latestTurnInteractions: [],
        pendingInteractions: []
      },
      workspaceId: "workspace-1",
      agentSessionId: input.agentSessionId ?? "session-1",
      agentTargetId: input.agentTargetId ?? "local:opencode",
      provider: input.provider ?? "opencode",
      providerSessionId: "provider-session-1",
      cwd: "/workspace/project",
      title: "OpenCode",
      ...(input.capabilities ? { capabilities: input.capabilities } : {}),
      usage: input.usage
    });
    const {
      activeTurn: _activeTurn,
      latestTurn: _latestTurn,
      latestTurnInteractions: _latestTurnInteractions,
      pendingInteractions: _pendingInteractions,
      ...activeEngineSession
    } = normalized;
    return activeEngineSession as CanonicalAgentSession;
  }

  function composerOptions(input: {
    provider?: string;
    capabilities?: string[];
  }): AgentActivityComposerOptions {
    return {
      provider: input.provider ?? "opencode",
      capabilities: capabilitiesFixture(input.capabilities ?? []),
      models: [],
      reasoningEfforts: [],
      speeds: [],
      modelConfigurable: false,
      reasoningConfigurable: false,
      permissionConfig: {
        configurable: false,
        defaultValue: null,
        modes: []
      },
      capabilityCatalog: [],
      skills: [],
      behavior: {
        collapseModelOptionsToLatest: false,
        modelOptionsAuthoritative: false,
        refreshModelOptionsAfterSettings: false,
        prewarmDraftSession: false,
        planModeExclusiveWithPermissionMode: false
      },
      loadedAtUnixMs: 0
    };
  }

  function capabilitiesFixture(
    capabilities: readonly string[]
  ): AgentActivitySessionCapabilities {
    return Object.fromEntries(
      AGENT_CAPABILITY_KEYS.map((key) => [key, capabilities.includes(key)])
    ) as unknown as AgentActivitySessionCapabilities;
  }

  it("projects typed canonical session usage into the composer footer", () => {
    const activeEngineSession = engineSession({
      usage: {
        contextWindow: { usedTokens: 33_168, totalTokens: 400_000 },
        quotas: []
      }
    });
    const data = {
      provider: "opencode" as const,
      agentTargetId: "local:opencode",
      lastActiveAgentSessionId: "session-1"
    };

    const { result, rerender } = renderHook(() =>
      useAgentGUIComposerCapabilities({
        activeConversationId: "session-1",
        activeEngineSession,
        activeSessionState: null,
        data,
        draftSettingsBySessionId: {},
        selectedComposerTargetData: {
          agentTargetId: "local:opencode",
          data,
          provider: "opencode",
          targetId: "local:opencode"
        },
        composer: composerWith({})
      })
    );

    expect(result.current.usage).toEqual({
      usedTokens: 33_168,
      totalTokens: 400_000,
      percentUsed: 8,
      quotas: []
    });
    const previousUsage = result.current.usage;

    rerender();

    expect(result.current.usage).toBe(previousUsage);
  });

  it("retains the last session usage while a model change awaits fresh usage", () => {
    let activeEngineSession = engineSession({
      usage: {
        contextWindow: { usedTokens: 20_000, totalTokens: 200_000 },
        quotas: []
      }
    });
    const data = {
      provider: "opencode" as const,
      agentTargetId: "local:opencode",
      lastActiveAgentSessionId: "session-1"
    };
    const { result, rerender } = renderHook(() =>
      useAgentGUIComposerCapabilities({
        activeConversationId: activeEngineSession.agentSessionId,
        activeEngineSession,
        activeSessionState: null,
        data,
        draftSettingsBySessionId: {},
        selectedComposerTargetData: {
          agentTargetId: "local:opencode",
          data,
          provider: "opencode",
          targetId: "local:opencode"
        },
        composer: composerWith({})
      })
    );

    const previousUsage = result.current.usage;
    activeEngineSession = engineSession({ usage: null });
    rerender();

    expect(result.current.usage).toBe(previousUsage);

    activeEngineSession = engineSession({
      usage: {
        contextWindow: { usedTokens: 36_103, totalTokens: 1_000_000 },
        quotas: []
      }
    });
    rerender();

    expect(result.current.usage).toEqual({
      usedTokens: 36_103,
      totalTokens: 1_000_000,
      percentUsed: 4,
      quotas: []
    });
  });

  it("does not carry retained usage into another session", () => {
    let activeEngineSession = engineSession({
      usage: {
        contextWindow: { usedTokens: 20_000, totalTokens: 200_000 },
        quotas: []
      }
    });
    const data = {
      provider: "opencode" as const,
      agentTargetId: "local:opencode",
      lastActiveAgentSessionId: "session-1"
    };
    const { result, rerender } = renderHook(() =>
      useAgentGUIComposerCapabilities({
        activeConversationId: activeEngineSession.agentSessionId,
        activeEngineSession,
        activeSessionState: null,
        data,
        draftSettingsBySessionId: {},
        selectedComposerTargetData: {
          agentTargetId: "local:opencode",
          data,
          provider: "opencode",
          targetId: "local:opencode"
        },
        composer: composerWith({})
      })
    );

    expect(result.current.usage?.totalTokens).toBe(200_000);

    activeEngineSession = engineSession({
      agentSessionId: "session-2",
      usage: null
    });
    rerender();

    expect(result.current.usage).toBeNull();
  });

  it("keeps target-declared browser support when active session metadata lacks it", () => {
    const activeEngineSession = engineSession({
      agentTargetId: "extension:hermes",
      capabilities: capabilitiesFixture(["interrupt"]),
      provider: "acp:hermes",
      usage: null
    });
    const data = {
      provider: "acp:hermes" as const,
      agentTargetId: "extension:hermes",
      lastActiveAgentSessionId: "session-1"
    };
    const composer = composerWith({
      "extension:hermes": composerOptions({
        provider: "acp:hermes",
        capabilities: ["interrupt", "browserUse", "skills"]
      })
    });

    const { result } = renderHook(() =>
      useAgentGUIComposerCapabilities({
        activeConversationId: "session-1",
        activeEngineSession,
        activeSessionState: null,
        data,
        draftSettingsBySessionId: {},
        selectedComposerTargetData: {
          agentTargetId: "extension:hermes",
          data,
          provider: "acp:hermes",
          targetId: "extension:hermes"
        },
        composer
      })
    );

    expect(result.current.composerSupport.browser).toBe(true);
  });

  it("reuses the target-scoped Composer Options reference across session creation", () => {
    const data = {
      provider: "codex" as const,
      agentTargetId: "local:codex",
      lastActiveAgentSessionId: null
    };
    const selectedComposerTargetData = {
      agentTargetId: "local:codex",
      data,
      provider: "codex" as const,
      targetId: "local:codex"
    };
    const composer = composerWith({
      "local:codex": composerOptions({ provider: "codex" })
    });
    const cachedOptions = composer.options("local:codex");
    expect(cachedOptions).not.toBeNull();

    type CreationProps = {
      activeConversationId: string | null;
      activeEngineSession: CanonicalAgentSession | null;
      optimisticComposerTarget: {
        agentSessionId: string;
        target: typeof selectedComposerTargetData;
      } | null;
    };
    const initialProps: CreationProps = {
      activeConversationId: null,
      activeEngineSession: null,
      optimisticComposerTarget: null
    };
    const { result, rerender } = renderHook(
      ({
        activeConversationId,
        activeEngineSession,
        optimisticComposerTarget
      }: CreationProps) =>
        useAgentGUIComposerCapabilities({
          activeConversationId,
          activeEngineSession,
          activeSessionState: null,
          data,
          draftSettingsBySessionId: {},
          optimisticComposerTarget,
          selectedComposerTargetData,
          composer
        }),
      { initialProps }
    );

    expect(result.current.providerComposerOptions).toBe(cachedOptions);

    rerender({
      activeConversationId: "session-new",
      activeEngineSession: null,
      optimisticComposerTarget: {
        agentSessionId: "session-new",
        target: selectedComposerTargetData
      }
    });

    expect(result.current.composerTargetData).toBe(selectedComposerTargetData);
    expect(result.current.providerComposerOptions).toBe(cachedOptions);

    rerender({
      activeConversationId: "session-new",
      activeEngineSession: engineSession({
        agentSessionId: "session-new",
        agentTargetId: "local:codex",
        provider: "codex",
        usage: null
      }),
      optimisticComposerTarget: null
    });

    expect(result.current.providerComposerOptions).toBe(cachedOptions);
  });
});
