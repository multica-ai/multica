import { renderHook } from "@testing-library/react";
import { useEffect, useMemo, useRef } from "react";
import { describe, expect, it, vi } from "vitest";
import {
  ConversationOverviewState,
  type ConversationOverviewInput
} from "../../../state/ConversationOverviewState";
import type { AgentGUINodeData } from "../../../types";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";

type PresentationInput = ConversationOverviewInput & {
  activeLatestPendingSubmitTurnId?: string | null;
  hasUnconfirmedSubmit?: boolean;
  isCreatingConversation?: boolean;
  isSubmitting?: boolean;
};
// Controlled sources keep the same model while the table supplies new immutable values.
function useAgentGUIConversationPresentation(input: PresentationInput) {
  const current = useRef(input);
  current.current = input;
  const model = useMemo(
    () =>
      new ConversationOverviewState(
        new Proxy({} as ConversationOverviewInput, {
          get: (_, key) =>
            current.current[key as keyof ConversationOverviewInput]
        })
      ),
    []
  );
  useEffect(() => model.startRemembering(), [model, input]);
  return model;
}

describe("useAgentGUIConversationPresentation", () => {
  it("does not project a pending submit into Conversation working status", () => {
    const conversation = createConversation();
    const input = createInput(conversation);
    const pendingInput = {
      ...input,
      activeLatestPendingSubmitTurnId: "turn-pending",
      hasUnconfirmedSubmit: true,
      isCreatingConversation: true,
      isSubmitting: true
    };
    const rendered = renderHook(
      ({ value }: { value: PresentationInput }) =>
        useAgentGUIConversationPresentation(value),
      { initialProps: { value: pendingInput } }
    );

    expect(rendered.result.current.activeConversation?.status).toBe("ready");

    rendered.rerender({
      value: { ...pendingInput, conversations: [] }
    });
    expect(rendered.result.current.activeConversation?.status).toBe("ready");
  });

  it("projects canonical activity status into Conversation status", () => {
    const conversation = createConversation();
    const input = createInput(conversation);
    const rendered = renderHook(
      ({ value }: { value: PresentationInput }) =>
        useAgentGUIConversationPresentation(value),
      {
        initialProps: {
          value: {
            ...input,
            activityDisplayStatuses: new Map([
              [conversation.id, "working" as const]
            ])
          }
        }
      }
    );

    expect(rendered.result.current.activeConversation?.status).toBe("working");
  });

  it("lets canonical idle clear stale Conversation working status", () => {
    const conversation = createConversation();
    const input = createInput({ ...conversation, status: "working" });
    const rendered = renderHook(() =>
      useAgentGUIConversationPresentation({
        ...input,
        activityDisplayStatuses: new Map([[conversation.id, "idle" as const]])
      })
    );

    expect(rendered.result.current.activeConversation?.status).toBe("ready");
    expect(rendered.result.current.visibleConversations[0]?.status).toBe(
      "ready"
    );
  });

  it("does not project the provider name as a fallback conversation title", () => {
    const conversation = createConversation();
    const input = createInput(conversation);
    const rendered = renderHook(() =>
      useAgentGUIConversationPresentation({
        ...input,
        conversations: []
      })
    );

    expect(rendered.result.current.activeConversation).toEqual(
      expect.objectContaining({
        provider: "codex",
        title: "",
        titleFallback: "untitled-conversation"
      })
    );
  });

  it("does not infer fallback conversation project identity from workspacePath", () => {
    const conversation = createConversation();
    const input = createInput(conversation);
    const rendered = renderHook(() =>
      useAgentGUIConversationPresentation({
        ...input,
        conversations: [],
        userProjects: [
          {
            id: "workspace-project",
            label: "Workspace",
            path: "/workspace",
            pinnedAtUnixMs: 0,
            sectionKey: "project:/workspace"
          }
        ]
      })
    );

    expect(rendered.result.current.activeConversation?.project).toBeNull();
  });

  it("reuses visible and active conversation references for render-equal input", () => {
    const conversation = createConversation();
    const input = createInput(conversation);
    const rendered = renderHook(
      ({ value }: { value: PresentationInput }) =>
        useAgentGUIConversationPresentation(value),
      { initialProps: { value: input } }
    );
    const previous = {
      activeConversation: rendered.result.current.activeConversation,
      visibleConversations: rendered.result.current.visibleConversations
    };

    rendered.rerender({
      value: {
        ...input,
        activityDisplayStatuses: new Map(),
        conversations: [{ ...conversation }]
      }
    });

    expect(rendered.result.current.visibleConversations).toBe(
      previous.visibleConversations
    );
    expect(rendered.result.current.activeConversation).toBe(
      previous.activeConversation
    );
  });

  it("updates active semantic metadata without invalidating the rail list", () => {
    const conversation = createConversation();
    const input = createInput(conversation);
    const rendered = renderHook(
      ({ value }: { value: PresentationInput }) =>
        useAgentGUIConversationPresentation(value),
      { initialProps: { value: input } }
    );
    const previous = {
      activeConversation: rendered.result.current.activeConversation,
      visibleConversations: rendered.result.current.visibleConversations
    };

    rendered.rerender({
      value: {
        ...input,
        conversations: [{ ...conversation, resumable: false }]
      }
    });

    expect(rendered.result.current.visibleConversations).toBe(
      previous.visibleConversations
    );
    expect(rendered.result.current.activeConversation).not.toBe(
      previous.activeConversation
    );
    expect(rendered.result.current.activeConversation?.resumable).toBe(false);
  });

  it("preserves the active agent target identity for open-provider fallback conversations", () => {
    const conversation = createConversation();
    const input = createInput({
      ...conversation,
      agentTargetId: "extension:example",
      provider: "acp:example"
    });
    const rendered = renderHook(() =>
      useAgentGUIConversationPresentation({
        ...input,
        conversations: [],
        data: {
          ...input.data,
          agentTargetId: "extension:example",
          provider: "acp:example"
        },
        dataRef: {
          current: {
            ...input.dataRef.current,
            agentTargetId: "extension:example",
            provider: "acp:example"
          }
        },
        normalizedProviderTargets: [
          {
            agentTargetId: "extension:example",
            label: "Example Agent",
            provider: "acp:example",
            ref: {
              kind: "agent_extension",
              provider: "acp:example"
            },
            targetId: "extension:example"
          }
        ]
      })
    );

    expect(rendered.result.current.activeConversation?.title).toBe("");
    expect(rendered.result.current.activeConversation?.titleFallback).toBe(
      "untitled-conversation"
    );
    expect(rendered.result.current.activeConversation?.agentTargetId).toBe(
      "extension:example"
    );
  });

  it("preserves a missing explicit target instead of selecting a provider sibling", () => {
    const conversation = createConversation();
    const input = createInput({
      ...conversation,
      agentTargetId: "extension:missing",
      provider: "acp:example"
    });
    const rendered = renderHook(() =>
      useAgentGUIConversationPresentation({
        ...input,
        conversations: [],
        data: {
          ...input.data,
          agentTargetId: "extension:missing",
          provider: "acp:example"
        },
        dataRef: {
          current: {
            ...input.dataRef.current,
            agentTargetId: "extension:missing",
            provider: "acp:example"
          }
        },
        normalizedProviderTargets: [
          {
            agentTargetId: "extension:sibling",
            label: "Sibling Agent",
            provider: "acp:example",
            ref: {
              kind: "agent_extension",
              provider: "acp:example"
            },
            targetId: "extension:sibling"
          }
        ]
      })
    );

    expect(rendered.result.current.activeConversation?.agentTargetId).toBe(
      "extension:missing"
    );
  });

  it("backfills target memory when canonical session metadata arrives", () => {
    const conversation = createConversation();
    const input = createInput(conversation);
    let data: AgentGUINodeData = input.data;

    input.dataRef = { current: data };
    input.normalizedExplicitProviderTargets = [
      {
        agentTargetId: "target-1",
        label: "Codex",
        provider: "codex",
        ref: { kind: "local", provider: "codex" },
        targetId: "target-1"
      }
    ];
    input.normalizedProviderTargets = input.normalizedExplicitProviderTargets;
    input.onDataChangeRef = {
      current: (updater) => {
        data = updater(data);
        input.dataRef.current = data;
      }
    };

    renderHook(() => useAgentGUIConversationPresentation(input));

    expect(data.lastActiveAgentSessionIdByAgentTargetId).toEqual({
      "target-1": "session-1"
    });
  });

  it("keeps a hidden transient conversation out of the rail while presenting its identity", () => {
    const conversation = createConversation();
    const hiddenTransient: AgentGUIConversationSummary = {
      ...createConversation(),
      hiddenFromRail: true,
      id: "hidden-delegate-1",
      title: "Delegated task"
    };
    const input = createInput(conversation);
    const rendered = renderHook(() =>
      useAgentGUIConversationPresentation({
        ...input,
        activeConversationId: hiddenTransient.id,
        transientConversation: hiddenTransient
      })
    );

    expect(
      rendered.result.current.visibleConversations.map((entry) => entry.id)
    ).toEqual([conversation.id]);
    expect(rendered.result.current.activeConversation).toEqual(
      expect.objectContaining({
        id: "hidden-delegate-1",
        provider: "codex",
        title: "Delegated task"
      })
    );
  });

  it("projects exact activity for a hidden transient conversation", () => {
    const conversation = createConversation();
    const hiddenTransient: AgentGUIConversationSummary = {
      ...createConversation(),
      hiddenFromRail: true,
      id: "hidden-delegate-1",
      status: "working",
      title: "Delegated task"
    };
    const input = createInput(conversation);
    const rendered = renderHook(() =>
      useAgentGUIConversationPresentation({
        ...input,
        activeConversationId: hiddenTransient.id,
        activityDisplayStatuses: new Map([
          [hiddenTransient.id, "idle" as const]
        ]),
        transientConversation: hiddenTransient
      })
    );

    expect(rendered.result.current.activeConversation?.status).toBe("ready");
    expect(
      rendered.result.current.visibleConversations.some(
        (entry) => entry.id === hiddenTransient.id
      )
    ).toBe(false);
  });
});

function createInput(
  conversation: AgentGUIConversationSummary
): PresentationInput {
  const data = {
    lastActiveAgentSessionId: conversation.id,
    provider: conversation.provider
  };
  return {
    activeConversationId: conversation.id,
    activeLatestPendingSubmitTurnId: null,
    activityDisplayStatuses: new Map(),
    agentTargetsLoading: false,
    conversations: [conversation],
    currentUserId: "user-1",
    data,
    dataRef: { current: data },
    defaultAgentTargetId: null,
    hasUnconfirmedSubmit: false,
    isCreatingConversation: false,
    isSubmitting: true,
    normalizedExplicitProviderTargets: [],
    normalizedProviderTargets: [],
    onDataChangeRef: { current: vi.fn() },
    shouldUseStaticProviderTargets: false,
    transientConversation: null,
    userProjects: [],
    workspacePath: "/workspace"
  };
}

function createConversation(): AgentGUIConversationSummary {
  return {
    agentTargetId: "target-1",
    cwd: "/workspace",
    id: "session-1",
    provider: "codex",
    status: "ready",
    title: "streaming session",
    titleFallback: null,
    updatedAtUnixMs: 1,
    userId: "user-1"
  };
}
