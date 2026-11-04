import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { AgentGUINodeViewModel } from "./agentGuiNodeTypes";
import { useAgentGUIViewModel } from "./useAgentGUIViewModel";
import { buildAgentComposerDraft } from "./agentComposerDraft";

describe("useAgentGUIViewModel render budgets", () => {
  it("keeps composer and rail references stable for a streaming detail update", () => {
    assertIsolatedGroupUpdate("detail", ["composer", "rail"]);
  });

  it("keeps detail and composer references stable for a rail interaction", () => {
    assertIsolatedGroupUpdate("rail", ["composer", "detail"]);
  });

  it("keeps rail and detail references stable while typing in the composer", () => {
    assertIsolatedGroupUpdate("composer", ["detail", "rail"]);
  });

  it("publishes the connecting-to-ready Composer gate as one snapshot", () => {
    const initial = createViewModel();
    const connectingGate: AgentGUINodeViewModel["composer"]["gate"] = {
      conversationBusy: false,
      isAwaitingTurnStart: false,
      runtime: {
        status: "blocked",
        reason: "target_connection",
        sessionRuntimeReason: null
      },
      editor: { status: "blocked", reason: "runtime_blocked" },
      submission: { status: "blocked", reason: "runtime_blocked" }
    };
    const readyGate: AgentGUINodeViewModel["composer"]["gate"] = {
      conversationBusy: false,
      isAwaitingTurnStart: false,
      runtime: {
        status: "ready",
        reason: null,
        sessionRuntimeReason: null
      },
      editor: { status: "editable", reason: null },
      submission: { status: "ready", reason: null }
    };
    const rendered = renderHook(
      ({ candidate }) => useAgentGUIViewModel(candidate),
      {
        initialProps: {
          candidate: {
            ...initial,
            composer: { ...initial.composer, gate: connectingGate }
          }
        }
      }
    );

    rendered.rerender({
      candidate: {
        ...initial,
        composer: { ...initial.composer, gate: readyGate }
      }
    });

    expect(rendered.result.current.composer.gate).toBe(readyGate);
    expect(rendered.result.current.composer.gate).toMatchObject({
      runtime: { status: "ready" },
      editor: { status: "editable" },
      submission: { status: "ready" }
    });
  });
});

function assertIsolatedGroupUpdate(
  group: "composer" | "detail" | "rail",
  stableGroups: readonly ("composer" | "detail" | "rail")[]
): void {
  const initial = createViewModel();
  const rendered = renderHook(
    ({ candidate }) => useAgentGUIViewModel(candidate),
    { initialProps: { candidate: initial } }
  );
  const previous = rendered.result.current;

  rendered.rerender({
    candidate: {
      ...initial,
      [group]: changedGroup(group, initial[group])
    }
  });

  expect(rendered.result.current[group]).not.toBe(previous[group]);
  for (const stableGroup of stableGroups) {
    expect(rendered.result.current[stableGroup]).toBe(previous[stableGroup]);
  }
}

function changedGroup<Group extends "composer" | "detail" | "rail">(
  group: Group,
  current: AgentGUINodeViewModel[Group]
): AgentGUINodeViewModel[Group] {
  if (group === "detail") {
    return {
      ...current,
      hasSentUserMessage: !(current as AgentGUINodeViewModel["detail"])
        .hasSentUserMessage
    } as AgentGUINodeViewModel[Group];
  }
  if (group === "rail") {
    return {
      ...current,
      isLoadingConversations: !(current as AgentGUINodeViewModel["rail"])
        .isLoadingConversations
    } as AgentGUINodeViewModel[Group];
  }
  return {
    ...current,
    draftPrompt: `${(current as AgentGUINodeViewModel["composer"]).draftPrompt}x`
  } as AgentGUINodeViewModel[Group];
}

function createViewModel(): AgentGUINodeViewModel {
  return {
    shell: {
      workspaceId: "workspace-1",
      workspacePath: "/workspace",
      currentUserId: "user-1",
      data: {} as AgentGUINodeViewModel["shell"]["data"]
    },
    rail: {
      selectedAgentTarget:
        {} as AgentGUINodeViewModel["rail"]["selectedAgentTarget"],
      agentTargets: [],
      agentTargetsLoading: false,
      providerRailMode: "catalog",
      comingSoonProviders: [],
      conversationFilter: { kind: "all" },
      conversations: [],
      userProjects: [],
      activeConversation: null,
      activeConversationId: null,
      revealRequest: null,
      isLoadingConversations: false,
      listError: null
    },
    detail: {
      availability: "ready",
      isLoadingMessages: false,
      isLoadingOlderMessages: false,
      hasOlderMessages: false,
      usage: null,
      hasSentUserMessage: false,
      avoidGroupingEdits: false,
      conversation: null,
      conversationDetail: null
    },
    composer: {
      handoffAgentTargets: [],
      availableCommands: [],
      availableSkills: [],
      draftPrompt: "",
      draftContent: buildAgentComposerDraft({ prompt: "" }),
      isCreatingConversation: false,
      isSubmitting: false,
      isInterrupting: false,
      isCancelPending: false,
      hasPendingSubmitStopTarget: false,
      promptImagesSupported: false,
      compactSupported: false,
      gate: {
        conversationBusy: false,
        isAwaitingTurnStart: false,
        runtime: {
          status: "ready",
          reason: null,
          sessionRuntimeReason: null
        },
        editor: { status: "editable", reason: null },
        submission: { status: "ready", reason: null }
      },
      composerSettings:
        {} as AgentGUINodeViewModel["composer"]["composerSettings"],
      queuedPrompts: [],
      queueStatus: "active",
      drainingQueuedPromptId: null
    },
    interaction: {} as AgentGUINodeViewModel["interaction"],
    readiness: {} as AgentGUINodeViewModel["readiness"],
    operations: {} as AgentGUINodeViewModel["operations"]
  };
}
