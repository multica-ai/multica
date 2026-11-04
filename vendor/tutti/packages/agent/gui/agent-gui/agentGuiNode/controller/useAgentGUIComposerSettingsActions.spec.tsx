import { act, renderHook } from "@testing-library/react";
import {
  createAgentSessionEngine,
  normalizeAgentActivitySession,
  type AgentActivityComposerOptions
} from "@tutti-os/agent-activity-core";
import { describe, expect, it, vi } from "vitest";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import { createTestEngineCommandPort } from "../../../shared/testing/createTestAgentSessionEngine";
import type { AgentSessionComposerSettings } from "../../../shared/agentSessionTypes";
import type { AgentGUINodeData } from "../../../types";
import type { AgentGUIRememberComposerDefaultsResult } from "./agentGuiController.providerHelpers";
import type { AgentGUIComposerDefaultsAuthorityReconciler } from "./agentGuiComposerDefaultsReconciliation";
import type { useAgentGUIActivation } from "./useAgentGUIActivation";
import { useAgentGUIComposerSettingsActions } from "./useAgentGUIComposerSettingsActions";

describe("useAgentGUIComposerSettingsActions", () => {
  it("retires a remembered model rejected by the authoritative target catalog", () => {
    const sessionEngine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: createTestEngineCommandPort({ execute: vi.fn() }),
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    const data: AgentGUINodeData = {
      agentTargetId: "local:codex",
      lastActiveAgentSessionId: null,
      provider: "codex"
    };
    const target = {
      agentTargetId: "local:codex",
      data,
      provider: "codex" as const,
      targetId: "local:codex"
    };
    const draftKey = "__agent_gui_node_defaults__:target:local:codex";
    const draftSettingsBySessionIdRef: {
      current: Record<string, AgentSessionComposerSettings>;
    } = {
      current: {
        [draftKey]: { model: "gpt-5.6-sol" }
      }
    };
    const onComposerDefaultsAuthorityReloadedRef =
      createComposerDefaultsAuthorityReconcilerRef();
    let persistedData = data;
    const onDataChange = vi.fn(
      (updater: (current: AgentGUINodeData) => AgentGUINodeData) => {
        persistedData = updater(persistedData);
      }
    );
    const setDraftSettingsBySessionId = vi.fn();
    renderHook(() =>
      useAgentGUIComposerSettingsActions({
        activation: {
          stateFor: vi.fn(() => "inactive" as const)
        } as unknown as ReturnType<typeof useAgentGUIActivation>,
        activeCanonicalComposerSettings: {},
        activeConversationIdRef: { current: null },
        activeEngineActiveTurn: null,
        agentActivityRuntime: {
          composer: { options: () => null, status: () => undefined }
        } as unknown as AgentGUIRuntime,
        composerSupportPermissionModeChangeDeferred: false,
        dataRef: { current: data },
        defaultReasoningEffort: null,
        draftSettingsBySessionIdRef,
        isMountedRef: { current: true },
        loadDraftComposerOptions: vi.fn(),
        onComposerDefaultsAuthorityReloadedRef,
        onDataChangeRef: { current: onDataChange },
        onRememberComposerDefaultsRef: { current: undefined },
        onShowMessageRef: { current: vi.fn() },
        reloadComposerOptionsForTarget: vi.fn(async () => {}),
        selectedComposerTargetDataRef: { current: target },
        sessionEngine,
        setDraftSettingsBySessionId,
        updateComposerSettingsRef: { current: vi.fn() },
        workspaceId: "workspace-1"
      })
    );
    const options: AgentActivityComposerOptions = {
      provider: "codex",
      capabilities: null,
      models: [
        { value: "glm-5", label: "GLM-5" },
        {
          value: "gpt-5.6-sol",
          label: "GPT-5.6-Sol",
          requested: true
        }
      ],
      reasoningEfforts: [],
      speeds: [],
      modelConfigurable: true,
      reasoningConfigurable: false,
      skills: [],
      behavior: {
        collapseModelOptionsToLatest: false,
        modelOptionsAuthoritative: true,
        refreshModelOptionsAfterSettings: false,
        prewarmDraftSession: false,
        planModeExclusiveWithPermissionMode: false
      },
      loadedAtUnixMs: 1,
      effectiveSettings: { model: "glm-5" }
    };

    act(() => {
      onComposerDefaultsAuthorityReloadedRef.current.reconcileHomeDefaults(
        target,
        options
      );
    });

    expect(draftSettingsBySessionIdRef.current[draftKey]?.model).toBeNull();
    expect(setDraftSettingsBySessionId).toHaveBeenCalledOnce();
    expect(
      persistedData.composerOverridesByAgentTargetId?.["local:codex"]?.model
    ).toBeNull();
    expect(onDataChange).toHaveBeenCalledOnce();
  });

  it("preserves all explicit home defaults across stale options, transient empty selects, and unrelated patches", () => {
    const sessionEngine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: createTestEngineCommandPort({ execute: vi.fn() }),
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    const data: AgentGUINodeData = {
      agentTargetId: "local:codex",
      lastActiveAgentSessionId: null,
      provider: "codex",
      composerOverridesByAgentTargetId: {
        "local:codex": { permissionModeId: "auto" }
      }
    };
    const onDataChange = vi.fn();
    const setDetailError = vi.fn();
    const onRememberComposerDefaults = vi.fn();
    const draftSettingsBySessionIdRef: {
      current: Record<string, AgentSessionComposerSettings>;
    } = { current: {} };
    const target = {
      agentTargetId: "local:codex",
      data,
      provider: "codex" as const,
      targetId: "local:codex"
    };
    const rendered = renderHook(() =>
      useAgentGUIComposerSettingsActions({
        activation: {
          stateFor: vi.fn(() => "inactive" as const)
        } as unknown as ReturnType<typeof useAgentGUIActivation>,
        activeCanonicalComposerSettings: {},
        activeConversationIdRef: { current: null },
        activeEngineActiveTurn: null,
        agentActivityRuntime: {
          composer: {
            options: () => ({
                behavior: { refreshModelOptionsAfterSettings: false },
                models: [],
                permissionConfig: {
                  configurable: true,
                  defaultValue: "auto",
                  modes: [{ id: "auto", label: "Approve for me" }]
                },
                reasoningConfigurable: false,
                reasoningEfforts: [],
                speeds: []
              }),
            status: () => undefined
          },
          trackDraftComposerSettingsChange: vi.fn()
        } as unknown as AgentGUIRuntime,
        composerSupportPermissionModeChangeDeferred: false,
        dataRef: { current: data },
        defaultReasoningEffort: null,
        draftSettingsBySessionIdRef,
        isMountedRef: { current: true },
        loadDraftComposerOptions: vi.fn(),
        onComposerDefaultsAuthorityReloadedRef:
          createComposerDefaultsAuthorityReconcilerRef(),
        onDataChangeRef: { current: onDataChange },
        onRememberComposerDefaultsRef: {
          current: onRememberComposerDefaults
        },
        onShowMessageRef: { current: vi.fn() },
        reloadComposerOptionsForTarget: vi.fn(async () => {}),
        selectedComposerTargetDataRef: { current: target },
        sessionEngine,
        setDraftSettingsBySessionId: vi.fn(),
        setDetailError,
        updateComposerSettingsRef: { current: vi.fn() },
        workspaceId: "workspace-1"
      })
    );

    act(() => {
      rendered.result.current.updateComposerSettings({
        model: "gpt-5-codex",
        permissionModeId: "full-access",
        reasoningEffort: "high",
        speed: "fast"
      });
    });

    expect(
      draftSettingsBySessionIdRef.current[
        "__agent_gui_node_defaults__:target:local:codex"
      ]
    ).toMatchObject({
      model: "gpt-5-codex",
      permissionModeId: "full-access",
      reasoningEffort: "high",
      speed: "fast"
    });
    expect(onRememberComposerDefaults).toHaveBeenCalledWith({
      agentTargetId: "local:codex",
      provider: "codex",
      defaults: {
        model: "gpt-5-codex",
        permissionModeId: "full-access",
        reasoningEffort: "high",
        speed: "fast"
      }
    });
    expect(onDataChange).not.toHaveBeenCalled();
    expect(setDetailError).toHaveBeenCalledWith(null);

    act(() => {
      rendered.result.current.updateComposerSettings({
        model: null,
        permissionModeId: null,
        reasoningEffort: null,
        speed: null
      });
    });

    act(() => {
      rendered.result.current.updateComposerSettings({ planMode: false });
    });

    expect(
      draftSettingsBySessionIdRef.current[
        "__agent_gui_node_defaults__:target:local:codex"
      ]
    ).toMatchObject({
      model: "gpt-5-codex",
      permissionModeId: "full-access",
      reasoningEffort: "high",
      speed: "fast"
    });
    expect(onRememberComposerDefaults).toHaveBeenCalledTimes(1);
  });

  it("asks the composer to write an active-session selection and remembers it", () => {
    const execute = vi.fn(() => new Promise<unknown>(() => undefined));
    const sessionEngine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: createTestEngineCommandPort({ execute }),
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    sessionEngine.dispatch({
      type: "session/snapshotReceived",
      sessions: [
        normalizeAgentActivitySession({
          activeTurnId: null,
          agentTargetId: "local:claude-code",
          agentSessionId: "session-1",
          cwd: "/workspace",
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "claude-code",
          settings: { permissionModeId: "dontAsk", planMode: false },
          title: "Historical session",
          workspaceId: "workspace-1"
        })
      ]
    });

    const data: AgentGUINodeData = {
      agentTargetId: "local:claude-code",
      lastActiveAgentSessionId: null,
      provider: "claude-code"
    };
    const onDataChange = vi.fn();
    const onRememberComposerDefaults = vi.fn();
    const setDraftSettingsBySessionId = vi.fn();
    const draftSettingsBySessionIdRef: {
      current: Record<string, AgentSessionComposerSettings>;
    } = { current: {} };
    const updateSettings = vi.fn();
    const activeSettings: AgentSessionComposerSettings = {
      browserUse: true,
      computerUse: true,
      permissionModeId: "dontAsk",
      planMode: false
    };
    const activation = {
      stateFor: vi.fn(() => "inactive" as const)
    } as unknown as ReturnType<typeof useAgentGUIActivation>;
    const rendered = renderHook(() =>
      useAgentGUIComposerSettingsActions({
        activation,
        activeCanonicalComposerSettings: activeSettings,
        activeConversationIdRef: { current: "session-1" },
        activeEngineActiveTurn: null,
        agentActivityRuntime: {
          composer: { options: () => null, status: () => undefined, updateSettings }
        } as unknown as AgentGUIRuntime,
        composerSupportPermissionModeChangeDeferred: false,
        dataRef: { current: data },
        defaultReasoningEffort: null,
        draftSettingsBySessionIdRef,
        isMountedRef: { current: true },
        loadDraftComposerOptions: vi.fn(),
        onComposerDefaultsAuthorityReloadedRef:
          createComposerDefaultsAuthorityReconcilerRef(),
        onDataChangeRef: { current: onDataChange },
        onRememberComposerDefaultsRef: {
          current: onRememberComposerDefaults
        },
        onShowMessageRef: { current: vi.fn() },
        reloadComposerOptionsForTarget: vi.fn(async () => {}),
        selectedComposerTargetDataRef: {
          current: {
            agentTargetId: "local:claude-code",
            data,
            provider: "claude-code",
            targetId: "local:claude-code"
          }
        },
        sessionEngine,
        setDraftSettingsBySessionId,
        updateComposerSettingsRef: { current: vi.fn() },
        workspaceId: "workspace-1"
      })
    );

    act(() => {
      rendered.result.current.updateComposerSettings({
        permissionModeId: "acceptEdits"
      });
    });

    expect(updateSettings).toHaveBeenCalledWith({
      agentSessionId: "session-1",
      settings: { permissionModeId: "acceptEdits" }
    });
    expect(onRememberComposerDefaults).toHaveBeenCalledWith({
      agentTargetId: "local:claude-code",
      provider: "claude-code",
      defaults: { permissionModeId: "acceptEdits" }
    });
    expect(draftSettingsBySessionIdRef.current).toEqual({});
    expect(setDraftSettingsBySessionId).not.toHaveBeenCalled();
    expect(onDataChange).not.toHaveBeenCalled();
  });

  it("reconciles A to B to A by exact field generation", async () => {
    const sessionEngine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: createTestEngineCommandPort({ execute: vi.fn() }),
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    const data: AgentGUINodeData = {
      agentTargetId: "local:opencode",
      lastActiveAgentSessionId: null,
      provider: "opencode"
    };
    const target = {
      agentTargetId: "local:opencode",
      data,
      provider: "opencode" as const,
      targetId: "local:opencode"
    };
    const first = deferred<AgentGUIRememberComposerDefaultsResult>();
    const second = deferred<AgentGUIRememberComposerDefaultsResult>();
    const third = deferred<AgentGUIRememberComposerDefaultsResult>();
    const onRememberComposerDefaults = vi
      .fn()
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise)
      .mockImplementationOnce(() => third.promise);
    const draftSettingsBySessionIdRef: {
      current: Record<string, AgentSessionComposerSettings>;
    } = { current: {} };
    const setDraftSettingsBySessionId = vi.fn();
    const onComposerDefaultsAuthorityReloadedRef =
      createComposerDefaultsAuthorityReconcilerRef();
    const reloadComposerOptionsForTarget = vi.fn(
      async (reloadInput: {
        settings: AgentSessionComposerSettings;
        target: typeof target;
      }) => {
        const authorityRead =
          onComposerDefaultsAuthorityReloadedRef.current.prepareRead(
            reloadInput.target,
            reloadInput.settings
          );
        onComposerDefaultsAuthorityReloadedRef.current.reloaded(
          authorityRead.receipt,
          {
            effectiveSettings: reloadInput.settings
          } as AgentActivityComposerOptions
        );
      }
    );
    const rendered = renderHook(() =>
      useAgentGUIComposerSettingsActions({
        activation: {
          stateFor: vi.fn(() => "inactive" as const)
        } as unknown as ReturnType<typeof useAgentGUIActivation>,
        activeCanonicalComposerSettings: {},
        activeConversationIdRef: { current: null },
        activeEngineActiveTurn: null,
        agentActivityRuntime: {
          composer: { options: () => null, status: () => undefined }
        } as unknown as AgentGUIRuntime,
        composerSupportPermissionModeChangeDeferred: false,
        dataRef: { current: data },
        defaultReasoningEffort: null,
        draftSettingsBySessionIdRef,
        isMountedRef: { current: true },
        loadDraftComposerOptions: vi.fn(),
        onComposerDefaultsAuthorityReloadedRef,
        onDataChangeRef: { current: vi.fn() },
        onRememberComposerDefaultsRef: {
          current: onRememberComposerDefaults
        },
        onShowMessageRef: { current: vi.fn() },
        reloadComposerOptionsForTarget,
        selectedComposerTargetDataRef: { current: target },
        sessionEngine,
        setDraftSettingsBySessionId,
        updateComposerSettingsRef: { current: vi.fn() },
        workspaceId: "workspace-1"
      })
    );

    act(() => {
      rendered.result.current.updateComposerSettings({
        permissionModeId: "ask"
      });
      rendered.result.current.updateComposerSettings({
        model: "opencode/new-model",
        permissionModeId: "full-access",
        reasoningEffort: "high",
        speed: "fast"
      });
      rendered.result.current.updateComposerSettings({
        permissionModeId: "ask"
      });
    });

    await act(async () => {
      first.resolve({
        acknowledgedFields: [],
        supersededFields: ["permissionModeId"]
      });
      await first.promise;
    });
    expect(reloadComposerOptionsForTarget).not.toHaveBeenCalled();
    expect(
      draftSettingsBySessionIdRef.current[
        "__agent_gui_node_defaults__:target:local:opencode"
      ]?.permissionModeId
    ).toBe("ask");
    setDraftSettingsBySessionId.mockClear();

    await act(async () => {
      second.resolve({
        acknowledgedFields: ["model", "reasoningEffort", "speed"],
        supersededFields: ["permissionModeId"]
      });
      await second.promise;
    });
    expect(reloadComposerOptionsForTarget).toHaveBeenCalledWith({
      settings: {
        model: "opencode/new-model",
        permissionModeId: "ask",
        reasoningEffort: "high",
        speed: "fast"
      },
      target
    });
    expect(
      draftSettingsBySessionIdRef.current[
        "__agent_gui_node_defaults__:target:local:opencode"
      ]
    ).toEqual({ permissionModeId: "ask" });

    reloadComposerOptionsForTarget.mockClear();
    setDraftSettingsBySessionId.mockClear();
    await act(async () => {
      third.resolve({
        acknowledgedFields: ["permissionModeId"],
        supersededFields: []
      });
      await third.promise;
    });
    expect(reloadComposerOptionsForTarget).toHaveBeenCalledWith({
      settings: { permissionModeId: "ask" },
      target
    });
    expect(draftSettingsBySessionIdRef.current).toEqual({});
    expect(setDraftSettingsBySessionId).toHaveBeenCalledTimes(1);
  });

  it("keeps acknowledged intent after a failed reload and releases confirmation when authority omits the field", async () => {
    const sessionEngine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: createTestEngineCommandPort({ execute: vi.fn() }),
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    const data: AgentGUINodeData = {
      agentTargetId: "local:opencode",
      lastActiveAgentSessionId: null,
      provider: "opencode"
    };
    const target = {
      agentTargetId: "local:opencode",
      data,
      provider: "opencode" as const,
      targetId: "local:opencode"
    };
    const acknowledgement = deferred<AgentGUIRememberComposerDefaultsResult>();
    const draftSettingsBySessionIdRef: {
      current: Record<string, AgentSessionComposerSettings>;
    } = { current: {} };
    const onComposerDefaultsAuthorityReloadedRef =
      createComposerDefaultsAuthorityReconcilerRef();
    const onShowMessage = vi.fn();
    const reloadComposerOptionsForTarget = vi.fn(async () => {
      throw new Error("transient options failure");
    });
    const rendered = renderHook(() =>
      useAgentGUIComposerSettingsActions({
        activation: {
          stateFor: vi.fn(() => "inactive" as const)
        } as unknown as ReturnType<typeof useAgentGUIActivation>,
        activeCanonicalComposerSettings: {},
        activeConversationIdRef: { current: null },
        activeEngineActiveTurn: null,
        agentActivityRuntime: {
          composer: { options: () => null, status: () => undefined }
        } as unknown as AgentGUIRuntime,
        composerSupportPermissionModeChangeDeferred: false,
        dataRef: { current: data },
        defaultReasoningEffort: null,
        draftSettingsBySessionIdRef,
        isMountedRef: { current: true },
        loadDraftComposerOptions: vi.fn(),
        onComposerDefaultsAuthorityReloadedRef,
        onDataChangeRef: { current: vi.fn() },
        onRememberComposerDefaultsRef: {
          current: vi.fn(() => acknowledgement.promise)
        },
        onShowMessageRef: { current: onShowMessage },
        reloadComposerOptionsForTarget,
        selectedComposerTargetDataRef: { current: target },
        sessionEngine,
        setDraftSettingsBySessionId: vi.fn(),
        updateComposerSettingsRef: { current: vi.fn() },
        workspaceId: "workspace-1"
      })
    );

    act(() => {
      rendered.result.current.updateComposerSettings({
        permissionModeId: "full-access"
      });
    });
    const preAckRead =
      onComposerDefaultsAuthorityReloadedRef.current.prepareRead(
        target,
        draftSettingsBySessionIdRef.current[
          "__agent_gui_node_defaults__:target:local:opencode"
        ] ?? {}
      );
    expect(preAckRead.receipt).toBeNull();
    act(() => {
      // The daemon changed event may be observed before the publish ack.
      onComposerDefaultsAuthorityReloadedRef.current.reloaded(
        preAckRead.receipt,
        {
          effectiveSettings: { permissionModeId: "full-access" }
        } as AgentActivityComposerOptions
      );
    });
    expect(
      draftSettingsBySessionIdRef.current[
        "__agent_gui_node_defaults__:target:local:opencode"
      ]
    ).toEqual({ permissionModeId: "full-access" });
    await act(async () => {
      acknowledgement.resolve({
        acknowledgedFields: ["permissionModeId"],
        supersededFields: []
      });
      await acknowledgement.promise;
    });
    expect(reloadComposerOptionsForTarget).toHaveBeenCalledTimes(1);
    expect(
      draftSettingsBySessionIdRef.current[
        "__agent_gui_node_defaults__:target:local:opencode"
      ]
    ).toEqual({ permissionModeId: "full-access" });
    expect(onShowMessage).not.toHaveBeenCalled();

    const authorityRead =
      onComposerDefaultsAuthorityReloadedRef.current.prepareRead(
        target,
        draftSettingsBySessionIdRef.current[
          "__agent_gui_node_defaults__:target:local:opencode"
        ] ?? {}
      );
    expect(authorityRead).toMatchObject({
      force: true,
      receipt: {
        draftKey: "__agent_gui_node_defaults__:target:local:opencode",
        fields: {
          permissionModeId: { value: "full-access" }
        }
      },
      settings: {}
    });
    act(() => {
      onComposerDefaultsAuthorityReloadedRef.current.reloaded(
        authorityRead.receipt,
        {} as AgentActivityComposerOptions
      );
    });
    expect(
      draftSettingsBySessionIdRef.current[
        "__agent_gui_node_defaults__:target:local:opencode"
      ]
    ).toEqual({ permissionModeId: "full-access" });
    expect(
      onComposerDefaultsAuthorityReloadedRef.current.prepareRead(
        target,
        draftSettingsBySessionIdRef.current[
          "__agent_gui_node_defaults__:target:local:opencode"
        ] ?? {}
      )
    ).toEqual({
      force: false,
      receipt: null,
      settings: { permissionModeId: "full-access" }
    });
  });

  it("does not sanitize an acknowledged model before a stale authority read is reconciled", async () => {
    const sessionEngine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: createTestEngineCommandPort({ execute: vi.fn() }),
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    const data: AgentGUINodeData = {
      agentTargetId: "local:opencode",
      lastActiveAgentSessionId: null,
      provider: "opencode"
    };
    const target = {
      agentTargetId: "local:opencode",
      data,
      provider: "opencode" as const,
      targetId: "local:opencode"
    };
    const draftKey = "__agent_gui_node_defaults__:target:local:opencode";
    const draftSettingsBySessionIdRef: {
      current: Record<string, AgentSessionComposerSettings>;
    } = { current: {} };
    const acknowledgement = deferred<AgentGUIRememberComposerDefaultsResult>();
    const onComposerDefaultsAuthorityReloadedRef =
      createComposerDefaultsAuthorityReconcilerRef();
    const onDataChange = vi.fn();
    const staleOptions = {
      behavior: {
        collapseModelOptionsToLatest: false,
        modelOptionsAuthoritative: true,
        refreshModelOptionsAfterSettings: false,
        prewarmDraftSession: false,
        planModeExclusiveWithPermissionMode: false
      },
      capabilities: null,
      effectiveSettings: { model: "opencode/model-a" },
      loadedAtUnixMs: 1,
      modelConfigurable: true,
      models: [{ label: "Model A", value: "opencode/model-a" }],
      provider: "opencode",
      reasoningConfigurable: false,
      reasoningEfforts: [],
      skills: [],
      speeds: []
    } satisfies AgentActivityComposerOptions;
    const reloadComposerOptionsForTarget = vi.fn(
      async (reloadInput: {
        settings: AgentSessionComposerSettings;
        target: typeof target;
      }) => {
        const authorityRead =
          onComposerDefaultsAuthorityReloadedRef.current.prepareRead(
            reloadInput.target,
            reloadInput.settings
          );
        // Match the production ordering: generic option sanitization runs
        // before the authority receipt is settled.
        onComposerDefaultsAuthorityReloadedRef.current.reconcileHomeDefaults(
          reloadInput.target,
          staleOptions
        );
        onComposerDefaultsAuthorityReloadedRef.current.reloaded(
          authorityRead.receipt,
          staleOptions
        );
      }
    );
    const rendered = renderHook(() =>
      useAgentGUIComposerSettingsActions({
        activation: {
          stateFor: vi.fn(() => "inactive" as const)
        } as unknown as ReturnType<typeof useAgentGUIActivation>,
        activeCanonicalComposerSettings: {},
        activeConversationIdRef: { current: null },
        activeEngineActiveTurn: null,
        agentActivityRuntime: {
          composer: { options: () => null, status: () => undefined }
        } as unknown as AgentGUIRuntime,
        composerSupportPermissionModeChangeDeferred: false,
        dataRef: { current: data },
        defaultReasoningEffort: null,
        draftSettingsBySessionIdRef,
        isMountedRef: { current: true },
        loadDraftComposerOptions: vi.fn(),
        onComposerDefaultsAuthorityReloadedRef,
        onDataChangeRef: { current: onDataChange },
        onRememberComposerDefaultsRef: {
          current: vi.fn(() => acknowledgement.promise)
        },
        onShowMessageRef: { current: vi.fn() },
        reloadComposerOptionsForTarget,
        selectedComposerTargetDataRef: { current: target },
        sessionEngine,
        setDraftSettingsBySessionId: vi.fn(),
        updateComposerSettingsRef: { current: vi.fn() },
        workspaceId: "workspace-1"
      })
    );

    act(() => {
      rendered.result.current.updateComposerSettings({
        model: "opencode/model-b"
      });
    });
    await act(async () => {
      acknowledgement.resolve({
        acknowledgedFields: ["model"],
        supersededFields: []
      });
      await acknowledgement.promise;
    });

    expect(reloadComposerOptionsForTarget).toHaveBeenCalledOnce();
    expect(draftSettingsBySessionIdRef.current[draftKey]?.model).toBe(
      "opencode/model-b"
    );
    expect(onDataChange).not.toHaveBeenCalled();
    expect(
      onComposerDefaultsAuthorityReloadedRef.current.prepareRead(
        target,
        draftSettingsBySessionIdRef.current[draftKey] ?? {}
      )
    ).toMatchObject({
      force: true,
      receipt: {
        fields: {
          model: { value: "opencode/model-b" }
        }
      },
      settings: {}
    });
  });

  it("retries composer options without forcing a superseding request", () => {
    const sessionEngine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: createTestEngineCommandPort({ execute: vi.fn() }),
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    const data: AgentGUINodeData = {
      agentTargetId: "local:codex",
      lastActiveAgentSessionId: null,
      provider: "codex"
    };
    const target = {
      agentTargetId: "local:codex",
      data,
      provider: "codex" as const,
      targetId: "local:codex"
    };
    const loadDraftComposerOptions = vi.fn();
    const rendered = renderHook(() =>
      useAgentGUIComposerSettingsActions({
        activation: {
          stateFor: vi.fn(() => "inactive" as const)
        } as unknown as ReturnType<typeof useAgentGUIActivation>,
        activeCanonicalComposerSettings: {},
        activeConversationIdRef: { current: null },
        activeEngineActiveTurn: null,
        agentActivityRuntime: {
          composer: { options: () => null, status: () => undefined }
        } as unknown as AgentGUIRuntime,
        composerSupportPermissionModeChangeDeferred: false,
        dataRef: { current: data },
        defaultReasoningEffort: null,
        draftSettingsBySessionIdRef: { current: {} },
        isMountedRef: { current: true },
        loadDraftComposerOptions,
        onComposerDefaultsAuthorityReloadedRef:
          createComposerDefaultsAuthorityReconcilerRef(),
        onDataChangeRef: { current: vi.fn() },
        onRememberComposerDefaultsRef: { current: undefined },
        onShowMessageRef: { current: vi.fn() },
        reloadComposerOptionsForTarget: vi.fn(async () => {}),
        selectedComposerTargetDataRef: { current: target },
        sessionEngine,
        setDraftSettingsBySessionId: vi.fn(),
        updateComposerSettingsRef: { current: vi.fn() },
        workspaceId: "workspace-1"
      })
    );

    act(() => {
      rendered.result.current.retryComposerOptions();
      rendered.result.current.retryComposerOptions({
        force: true,
        section: "capabilities"
      });
    });

    expect(loadDraftComposerOptions).toHaveBeenNthCalledWith(1);
    expect(loadDraftComposerOptions).toHaveBeenNthCalledWith(2, {
      force: true,
      section: "capabilities"
    });
  });
});

function createComposerDefaultsAuthorityReconcilerRef(): {
  current: AgentGUIComposerDefaultsAuthorityReconciler;
} {
  return {
    current: {
      prepareRead: vi.fn((_target, settings) => ({
        force: false,
        receipt: null,
        settings
      })),
      reconcileHomeDefaults: vi.fn(),
      reloaded: vi.fn()
    }
  };
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}
