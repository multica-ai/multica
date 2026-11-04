import { describe, expect, it } from "vitest";
import type {
  AgentActivityComposerOptions,
  AgentActivityComposerOptionsLoadStatus
} from "@tutti-os/agent-activity-core";
import {
  composerOptionsForTarget,
  composerOptionsLoadingForTarget,
  ownerDeviceLabelForConversation,
  resolveAgentGUIProviderRailTargetSelection,
  targetConnectionForAgentGUIView
} from "./agentGuiController.providerHelpers";
import { createSharedAgentGUIAgentTarget } from "../../../agentTargets";
import type { AgentGUIComposerTargetData } from "./agentGuiController.composerPresentation";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";

const target = {
  agentTargetId: "target-1"
} as AgentGUIComposerTargetData;

describe("composer options target state", () => {
  const composer = (
    options: AgentActivityComposerOptions | null,
    status: AgentActivityComposerOptionsLoadStatus
  ) => ({ options: () => options, status: () => status });

  it("reports loading only before the target has cached options", () => {
    const loading = composer(null, "loading");
    expect(composerOptionsLoadingForTarget({ composer: loading, target })).toBe(true);
    expect(composerOptionsForTarget({ composer: loading, target })).toBe(null);

    const cachedOptions = {} as AgentActivityComposerOptions;
    const refreshing = composer(cachedOptions, "loading");
    expect(composerOptionsLoadingForTarget({ composer: refreshing, target })).toBe(false);
    expect(composerOptionsForTarget({ composer: refreshing, target })).toBe(cachedOptions);
  });

  it("does not leave the target loading after a request error", () => {
    expect(
      composerOptionsLoadingForTarget({ composer: composer(null, "error"), target })
    ).toBe(false);
  });
});

describe("provider rail target selection", () => {
  it("keeps an active conversation that belongs to the selected target", () => {
    expect(
      resolveAgentGUIProviderRailTargetSelection({
        activeConversation: conversation(
          "claude-session",
          "local:claude-code",
          "claude-code"
        ),
        nextFilter: {
          kind: "agentTarget",
          agentTargetId: "local:claude-code"
        }
      })
    ).toBe("keep-active-conversation");
  });

  it("opens the selected target home when the active conversation belongs elsewhere", () => {
    expect(
      resolveAgentGUIProviderRailTargetSelection({
        activeConversation: conversation(
          "codex-session",
          "local:codex",
          "codex"
        ),
        nextFilter: {
          kind: "agentTarget",
          agentTargetId: "local:claude-code"
        }
      })
    ).toBe("open-home-composer");
  });

  it("opens the selected target home when there is no active conversation", () => {
    expect(
      resolveAgentGUIProviderRailTargetSelection({
        activeConversation: null,
        nextFilter: {
          kind: "agentTarget",
          agentTargetId: "local:claude-code"
        }
      })
    ).toBe("open-home-composer");
  });

  it("opens the selected target home when it has no agent target id", () => {
    expect(
      resolveAgentGUIProviderRailTargetSelection({
        activeConversation: conversation(
          "codex-session",
          "local:codex",
          "codex"
        ),
        nextFilter: { kind: "all" }
      })
    ).toBe("open-home-composer");
  });
});

describe("conversation owner device label", () => {
  it("uses the exact active conversation target instead of the selected composer target", () => {
    const deviceA = createSharedAgentGUIAgentTarget({
      agentTargetId: "target-a",
      label: "Shared A",
      ownerDeviceLabel: "Device A",
      provider: "codex",
      sharedAgentId: "shared-a"
    });
    const deviceB = createSharedAgentGUIAgentTarget({
      agentTargetId: "target-b",
      label: "Shared B",
      ownerDeviceLabel: "Device B",
      provider: "codex",
      sharedAgentId: "shared-b"
    });

    expect(
      ownerDeviceLabelForConversation(
        conversation("session-b", "target-b", "codex"),
        [deviceA, deviceB]
      )
    ).toBe("Device B");
  });

  it("fails closed when the active conversation target is unavailable", () => {
    expect(
      ownerDeviceLabelForConversation(
        conversation("session-b", "missing", "codex"),
        [
          createSharedAgentGUIAgentTarget({
            agentTargetId: "target-a",
            label: "Shared A",
            ownerDeviceLabel: "Device A",
            provider: "codex",
            sharedAgentId: "shared-a"
          })
        ]
      )
    ).toBeNull();
  });
});

describe("target connection selection", () => {
  const selectedTarget = createSharedAgentGUIAgentTarget({
    agentTargetId: "target-home",
    label: "Shared Home",
    ownerDeviceLabel: "Home Device",
    provider: "codex",
    sharedAgentId: "shared-home"
  });

  it("uses the selected Home target before a Session exists", () => {
    expect(
      targetConnectionForAgentGUIView({
        activeConversation: null,
        selectedTarget,
        targets: [selectedTarget]
      })
    ).toEqual({
      agentTargetId: "target-home",
      ownerDeviceLabel: "Home Device"
    });
  });

  it("preserves the exact Session target when its presentation is stale", () => {
    expect(
      targetConnectionForAgentGUIView({
        activeConversation: conversation(
          "session-1",
          "target-missing",
          "codex"
        ),
        selectedTarget,
        targets: [selectedTarget]
      })
    ).toEqual({ agentTargetId: "target-missing", ownerDeviceLabel: null });
  });
});


function conversation(
  id: string,
  agentTargetId: string,
  provider: AgentGUIConversationSummary["provider"]
): AgentGUIConversationSummary {
  return {
    id,
    agentTargetId,
    cwd: "/repo",
    provider,
    status: "completed",
    title: id,
    updatedAtUnixMs: 1
  };
}
