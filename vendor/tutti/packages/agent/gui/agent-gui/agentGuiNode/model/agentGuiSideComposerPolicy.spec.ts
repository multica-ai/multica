import { describe, expect, it } from "vitest";
import { createAgentActivityEphemeralConversationProjector } from "@tutti-os/agent-activity-core";
import type { AgentSideConversationState } from "../../../agentSideConversationRuntime";
import type { AgentGUIComposerSettingsVM } from "./agentGuiNodeTypes";
import {
  projectAgentSideCapabilityMenuState,
  projectAgentSideComposerGate,
  projectAgentSideComposerSettings
} from "./agentGuiSideComposerPolicy";

function sideState(
  patch: Partial<AgentSideConversationState> = {}
): AgentSideConversationState {
  return {
    workspaceId: "workspace-1",
    sideAgentSessionId: "side-1",
    sourceAgentSessionId: "source-1",
    sequence: 0,
    status: "idle",
    activeTurnId: null,
    pendingInteraction: null,
    projection: createAgentActivityEphemeralConversationProjector({
      workspaceId: "workspace-1",
      agentSessionId: "side-1",
      sourceAgentSessionId: "source-1",
      provider: "codex"
    }).getSnapshot(),
    error: null,
    ...patch
  };
}

function parentSettings(): AgentGUIComposerSettingsVM {
  return {
    sessionSettings: null,
    draftSettings: {
      model: "gpt-5.6",
      reasoningEffort: "high",
      speed: "fast",
      planMode: false,
      browserUse: true,
      computerUse: true,
      permissionModeId: "full-access"
    },
    supportsModel: true,
    supportsReasoningEffort: true,
    supportsSpeed: true,
    supportsPermissionMode: true,
    supportsPlanMode: true,
    supportsBrowser: true,
    supportsComputerUse: true,
    isSettingsLoading: false,
    modelUnavailable: false,
    reasoningUnavailable: false,
    speedUnavailable: false,
    permissionModeUnavailable: false,
    selectedModelValue: "gpt-5.6",
    selectedReasoningEffortValue: "high",
    selectedSpeedValue: "fast",
    selectedPermissionModeValue: "full-access",
    selectedProjectPath: "/workspace",
    selectedProjectSectionKey: "workspace",
    projectPathIsRemote: false,
    availableModels: [{ value: "gpt-5.6", label: "GPT-5.6" }],
    availableReasoningEfforts: [{ value: "high", label: "High" }],
    availableSpeeds: [{ value: "fast", label: "Fast" }],
    availablePermissionModes: [{ value: "full-access", label: "Full access" }]
  };
}

describe("Agent Side composer policy", () => {
  it("inherits only the host-owned connector visibility gate", () => {
    expect(
      projectAgentSideCapabilityMenuState({
        connectors: { enabled: false },
        computerUse: { installed: true }
      })
    ).toEqual({ connectors: { enabled: false } });
    expect(projectAgentSideCapabilityMenuState(undefined)).toBeUndefined();
  });

  it("keeps Side submit state independent from the parent composer", () => {
    expect(projectAgentSideComposerGate(sideState()).submission.status).toBe(
      "ready"
    );
    expect(
      projectAgentSideComposerGate(
        sideState({ status: "running", activeTurnId: "turn-1" })
      ).submission.reason
    ).toBe("conversation_busy");
    expect(
      projectAgentSideComposerGate(sideState({ status: "expired" })).runtime
        .status
    ).toBe("blocked");
  });

  it("inherits values while making Side settings read-only", () => {
    const projected = projectAgentSideComposerSettings(parentSettings());
    expect(projected.draftSettings).toMatchObject({
      model: "gpt-5.6",
      reasoningEffort: "high",
      speed: "fast",
      permissionModeId: "full-access"
    });
    expect(projected).toMatchObject({
      supportsModel: false,
      supportsReasoningEffort: false,
      supportsSpeed: false,
      supportsPermissionMode: false,
      supportsPlanMode: false,
      projectLocked: true,
      availableModels: [],
      availablePermissionModes: []
    });
  });
});
