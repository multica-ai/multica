import { renderHook, waitFor } from "@testing-library/react";
import type { SetStateAction } from "react";
import { describe, expect, it, vi } from "vitest";
import { createLocalAgentGUIAgentTarget } from "../../../agentTargets";
import type { ConversationServices } from "../../../conversationServices";
import type { AgentGUINodeData } from "../../../types";
import {
  agentComposerDraftPrompt,
  buildAgentComposerDraft
} from "../model/agentComposerDraft";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";
import type { AgentGUIComposerTargetData } from "./agentGuiController.composerPresentation";
import { useAgentGUIConversationHome } from "./useAgentGUIConversationHome";

describe("useAgentGUIConversationHome composer append", () => {
  it("waits for the routed session, applies the request once, and acknowledges it", async () => {
    const target = createLocalAgentGUIAgentTarget("codex");
    const dataRef: { current: AgentGUINodeData } = {
      current: {
        agentTargetId: target.agentTargetId,
        lastActiveAgentSessionId: "other-session",
        provider: "codex"
      }
    };
    const targetData: AgentGUIComposerTargetData = {
      agentTargetId: target.agentTargetId ?? null,
      data: dataRef.current,
      provider: "codex",
      targetId: target.targetId
    };
    const activeConversationIdRef = { current: "other-session" as string };
    const draftByScopeKeyRef = {
      current: {
        "session:source-session": buildAgentComposerDraft({
          prompt: "Existing draft"
        })
      } as Record<string, AgentComposerDraft>
    };
    const handledComposerAppendSequenceRef = { current: null as number | null };
    const onComposerAppendHandled = vi.fn();
    const setDraftByScopeKey = vi.fn(
      (next: SetStateAction<Record<string, AgentComposerDraft>>) => {
        draftByScopeKeyRef.current =
          typeof next === "function" ? next(draftByScopeKeyRef.current) : next;
      }
    );

    const rendered = renderHook(
      ({ activeConversationId }) => {
        activeConversationIdRef.current = activeConversationId;
        useAgentGUIConversationHome({
          activeConversationId,
          activeConversationIdRef,
          activePendingActivation: null,
          agentActivityRuntime: {} as ConversationServices,
          composerAppendRequest: {
            agentSessionId: "source-session",
            prompt: "Modify the managed issue",
            sequence: 8
          },
          composerTargetDataFromProviderTarget: () => targetData,
          conversationFilterRef: { current: { kind: "all" } },
          currentProvider: "codex",
          dataRef,
          defaultAgentTargetId: target.agentTargetId ?? null,
          draftByScopeKeyRef,
          handledComposerAppendSequenceRef,
          handledPrefillPromptSequenceRef: { current: null },
          isComposerHomeRef: { current: false },
          isExplicitAgentGUIAgentTarget: () => true,
          loadDraftComposerOptions: vi.fn(),
          normalizedExplicitProviderTargets: [target],
          normalizedProviderTargets: [target],
          onComposerAppendHandled,
          onDataChangeRef: {
            current: (updater) => {
              dataRef.current = updater(dataRef.current);
            }
          },
          persistActiveConversation: vi.fn(),
          prefillPromptRequest: null,
          reportActiveConversationCleared: vi.fn(),
          selectedComposerTargetDataRef: { current: targetData },
          selectedProjectPathRef: { current: null },
          setActiveConversationId: vi.fn(),
          setConversationFilter: vi.fn(),
          setDetailError: vi.fn(),
          setDraftByScopeKey,
          setHomeComposerTargetOverride: vi.fn(),
          setIntent: vi.fn(),
          setIsComposerHome: vi.fn(),
          setIsLoadingMessages: vi.fn(),
          setSelectedProjectPath: vi.fn(),
          shouldUseStaticProviderTargets: true,
          submitPrefillPrompt: vi.fn(),
          unactivate: async () => undefined,
          workspaceId: "workspace-1"
        });
      },
      { initialProps: { activeConversationId: "other-session" } }
    );

    expect(onComposerAppendHandled).not.toHaveBeenCalled();
    expect(handledComposerAppendSequenceRef.current).toBeNull();

    rendered.rerender({ activeConversationId: "source-session" });

    await waitFor(() => {
      expect(onComposerAppendHandled).toHaveBeenCalledWith(8);
    });
    expect(handledComposerAppendSequenceRef.current).toBe(8);
    expect(
      agentComposerDraftPrompt(
        draftByScopeKeyRef.current["session:source-session"]!
      )
    ).toBe("Existing draft Modify the managed issue ");

    rendered.rerender({ activeConversationId: "source-session" });
    expect(onComposerAppendHandled).toHaveBeenCalledTimes(1);
  });
});
