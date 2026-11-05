import { renderHook } from "@testing-library/react";
import type { AgentActivityComposerOptions } from "@tutti-os/agent-activity-core";
import { describe, expect, it } from "vitest";
import type { ConversationServices } from "../../../conversationServices";
import type { AgentSessionComposerSettings } from "../../../shared/agentSessionTypes";
import {
  ComposerSettingsState,
  type ComposerSettingsInput
} from "../../../state/ComposerSettingsState";
import type { AgentGUINodeData } from "../../../types";
import { composerSettingsSupportFromOptions } from "../model/composerSettingsSupport";
import type { AgentGUIComposerTargetData } from "./agentGuiController.composerPresentation";

function useAgentGUIComposerPresentation(input: ComposerSettingsInput) {
  return new ComposerSettingsState(input);
}

describe("useAgentGUIComposerPresentation", () => {
  it("exposes a terminal options error without leaving the composer in loading state", () => {
    const data: AgentGUINodeData = {
      provider: "opencode",
      agentTargetId: "local:opencode",
      lastActiveAgentSessionId: null
    };
    const target: AgentGUIComposerTargetData = {
      agentTargetId: "local:opencode",
      data,
      provider: "opencode",
      targetId: "local:opencode"
    };

    const { result } = renderHook(() =>
      useAgentGUIComposerPresentation({
        activeConversation: null,
        activeConversationId: null,
        activeCanonicalSession: null,
        activeSessionState: null,
        agentActivityRuntime: {
          projectPathIsRemote: false
        } as ConversationServices,
        composerOptionsLoadStatus: "error",
        composerOptionsLoading: false,
        connectorOptionsLoading: false,
        composerSupport: composerSettingsSupportFromOptions(null, null),
        composerTargetProvider: "opencode",
        composerTargetData: target,
        data,
        defaultReasoningEffort: null,
        draftSettingsBySessionId: {},
        providerComposerOptions: null,
        selectedComposerTargetData: target,
        selectedProjectPath: null,
        shouldApplyPreparedProjectSelection: false,
        userProjects: []
      })
    );

    expect(result.current.stableComposerSettings).toMatchObject({
      composerOptionsError: true,
      composerOptionsLoadStatus: "error",
      isSettingsLoading: false
    });
  });

  it("keeps all explicit home defaults above stale options, then yields to authority after retirement", () => {
    const data: AgentGUINodeData = {
      provider: "opencode",
      agentTargetId: "local:opencode",
      lastActiveAgentSessionId: null
    };
    const target: AgentGUIComposerTargetData = {
      agentTargetId: "local:opencode",
      data,
      provider: "opencode",
      targetId: "local:opencode"
    };
    const options: AgentActivityComposerOptions = {
      provider: "opencode",
      codexSaverModeSupported: true,
      capabilities: null,
      models: [{ value: "opencode/old-model", label: "Old model" }],
      reasoningEfforts: [{ value: "low", label: "Low" }],
      reasoningOptionsByModel: {
        "opencode/old-model": {
          defaultValue: "low",
          options: [{ value: "low", label: "Low" }]
        }
      },
      speeds: [{ value: "normal", label: "Normal" }],
      modelConfigurable: true,
      reasoningConfigurable: false,
      skills: [],
      behavior: {
        collapseModelOptionsToLatest: false,
        modelOptionsAuthoritative: false,
        refreshModelOptionsAfterSettings: false,
        prewarmDraftSession: false,
        planModeExclusiveWithPermissionMode: false
      },
      loadedAtUnixMs: 1,
      effectiveSettings: {
        codexSaverMode: true,
        model: "opencode/old-model",
        permissionModeId: "ask",
        reasoningEffort: "low",
        speed: "normal"
      },
      permissionConfig: {
        configurable: true,
        defaultValue: "ask",
        modes: [{ id: "ask", label: "Ask" }]
      }
    };
    const draftSettingsBySessionId = {
      "__agent_gui_node_defaults__:target:local:opencode": {
        model: "opencode/new-model",
        permissionModeId: "full-access",
        reasoningEffort: "high" as const,
        speed: "fast" as const
      }
    };
    const { result, rerender } = renderHook(
      ({ currentOptions, drafts, entryEnabled }) =>
        useAgentGUIComposerPresentation({
          activeConversation: null,
          activeConversationId: null,
          activeCanonicalSession: null,
          activeSessionState: null,
          agentActivityRuntime: {
            projectPathIsRemote: false
          } as ConversationServices,
          composerSupport: {
            ...composerSettingsSupportFromOptions(currentOptions, null),
            reasoning: false
          },
          composerOptionsLoading: false,
          connectorOptionsLoading: false,
          composerTargetProvider: "opencode",
          codexSaverModeEntryEnabled: entryEnabled,
          composerTargetData: target,
          data,
          defaultReasoningEffort: null,
          draftSettingsBySessionId: drafts,
          providerComposerOptions: currentOptions,
          selectedComposerTargetData: target,
          selectedProjectPath: null,
          shouldApplyPreparedProjectSelection: true,
          userProjects: []
        }),
      {
        initialProps: {
          currentOptions: options,
          entryEnabled: true,
          drafts: draftSettingsBySessionId as Record<
            string,
            AgentSessionComposerSettings
          >
        }
      }
    );

    expect(result.current.stableComposerSettings.draftSettings).toMatchObject({
      codexSaverMode: true,
      model: "opencode/new-model",
      permissionModeId: "full-access",
      reasoningEffort: "high",
      speed: "fast"
    });
    expect(result.current.stableComposerSettings).toMatchObject({
      selectedModelValue: "opencode/new-model",
      modelChoiceHistory: {
        targetId: "local:opencode",
        catalog: {
          authoritative: false,
          effectiveModel: "opencode/old-model",
          loading: false
        }
      },
      selectedPermissionModeValue: "full-access",
      selectedReasoningEffortValue: "high",
      selectedSpeedValue: "fast"
    });
    expect(result.current.stableComposerSettings.supportsCodexSaverMode).toBe(
      true
    );

    rerender({
      currentOptions: options,
      entryEnabled: true,
      drafts: {
        "__agent_gui_node_defaults__:target:local:opencode": {
          ...draftSettingsBySessionId[
            "__agent_gui_node_defaults__:target:local:opencode"
          ],
          codexSaverMode: false
        }
      }
    });
    expect(
      result.current.stableComposerSettings.draftSettings.codexSaverMode
    ).toBe(false);

    rerender({
      currentOptions: {
        ...options,
        loadedAtUnixMs: 2,
        models: [{ value: "opencode/authority-model", label: "Authority" }],
        reasoningEfforts: [{ value: "medium", label: "Medium" }],
        reasoningOptionsByModel: {
          "opencode/authority-model": {
            defaultValue: "medium",
            options: [{ value: "medium", label: "Medium" }]
          }
        },
        effectiveSettings: {
          model: "opencode/authority-model",
          permissionModeId: "sandbox",
          reasoningEffort: "medium",
          speed: "normal"
        },
        permissionConfig: {
          configurable: true,
          defaultValue: "sandbox",
          modes: [{ id: "sandbox", label: "Sandbox" }]
        }
      },
      entryEnabled: false,
      drafts: {}
    });
    expect(result.current.stableComposerSettings).toMatchObject({
      selectedModelValue: "opencode/authority-model",
      selectedPermissionModeValue: "sandbox",
      selectedReasoningEffortValue: "medium",
      selectedSpeedValue: "normal"
    });
    expect(result.current.stableComposerSettings).toMatchObject({
      supportsCodexSaverMode: false,
      draftSettings: { codexSaverMode: false }
    });
  });

  it("switches reasoning visibility and values with the selected model profile", () => {
    const data: AgentGUINodeData = {
      provider: "opencode",
      agentTargetId: "local:opencode",
      lastActiveAgentSessionId: "session-1"
    };
    const target: AgentGUIComposerTargetData = {
      agentTargetId: "local:opencode",
      data,
      provider: "opencode",
      targetId: "local:opencode"
    };
    const options: AgentActivityComposerOptions = {
      provider: "opencode",
      capabilities: null,
      models: [
        { value: "opencode/big-pickle", label: "Big Pickle" },
        { value: "gpt-5.6-sol", label: "GPT-5.6 Sol" },
        { value: "gpt-5.6-luna", label: "GPT-5.6 Luna" }
      ],
      reasoningEfforts: [{ value: "high", label: "High" }],
      reasoningOptionsByModel: {
        "opencode/big-pickle": {
          defaultValue: null,
          options: []
        },
        "gpt-5.6-sol": {
          defaultValue: "medium",
          options: [
            { value: "minimal", label: "Minimal" },
            { value: "low", label: "Low" },
            { value: "medium", label: "Medium" },
            { value: "high", label: "High" },
            { value: "xhigh", label: "Extra high" }
          ]
        },
        "gpt-5.6-luna": {
          defaultValue: "medium",
          options: [
            { value: "low", label: "Low" },
            { value: "medium", label: "Medium" }
          ]
        }
      },
      speeds: [],
      modelConfigurable: true,
      reasoningConfigurable: false,
      skills: [],
      behavior: {
        collapseModelOptionsToLatest: false,
        modelOptionsAuthoritative: false,
        refreshModelOptionsAfterSettings: false,
        prewarmDraftSession: false,
        planModeExclusiveWithPermissionMode: false
      },
      loadedAtUnixMs: 1
    };
    const { result, rerender } = renderHook(
      ({ model }: { model: string }) => {
        const draftSettingsBySessionId = {
          "session-1": {
            model,
            reasoningEffort: "high" as const
          }
        };
        return useAgentGUIComposerPresentation({
          activeConversation: null,
          activeConversationId: "session-1",
          activeCanonicalSession: null,
          activeSessionState: null,
          agentActivityRuntime: {
            projectPathIsRemote: false
          } as ConversationServices,
          composerSupport: composerSettingsSupportFromOptions(options, null),
          composerOptionsLoading: false,
          connectorOptionsLoading: false,
          composerTargetProvider: "opencode",
          composerTargetData: target,
          data,
          defaultReasoningEffort: "high",
          draftSettingsBySessionId,
          providerComposerOptions: options,
          selectedComposerTargetData: target,
          selectedProjectPath: null,
          shouldApplyPreparedProjectSelection: false,
          userProjects: []
        });
      },
      { initialProps: { model: "opencode/big-pickle" } }
    );

    expect(
      result.current.stableComposerSettings.availableReasoningEfforts
    ).toEqual([]);
    expect(
      result.current.stableComposerSettings.selectedReasoningEffortValue
    ).toBeNull();
    expect(
      result.current.stableComposerSettings.draftSettings.reasoningEffort
    ).toBeNull();

    rerender({ model: "gpt-5.6-sol" });

    expect(
      result.current.stableComposerSettings.availableReasoningEfforts.map(
        (option) => option.value
      )
    ).toEqual(["minimal", "low", "medium", "high", "xhigh"]);
    expect(
      result.current.stableComposerSettings.selectedReasoningEffortValue
    ).toBe("high");

    rerender({ model: "gpt-5.6-luna" });

    expect(
      result.current.stableComposerSettings.availableReasoningEfforts.map(
        (option) => option.value
      )
    ).toEqual(["low", "medium"]);
    expect(
      result.current.stableComposerSettings.selectedReasoningEffortValue
    ).toBe("medium");

    rerender({ model: "opencode/big-pickle" });

    expect(
      result.current.stableComposerSettings.availableReasoningEfforts
    ).toEqual([]);
    expect(
      result.current.stableComposerSettings.selectedReasoningEffortValue
    ).toBeNull();
  });
});
