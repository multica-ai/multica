import { createTestPromptOperations } from "../../../shared/testing/createTestPromptOperations";
import { act, renderHook, waitFor } from "@testing-library/react";
import {
  createAgentSessionEngine,
  type EngineEffectOptions
} from "@tutti-os/agent-activity-core";
import { describe, expect, it, vi } from "vitest";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";
import { agentComposerDraftPrompt } from "../model/agentComposerDraft";
import {
  clearSubmittedAgentGUIHomeDraft,
  restoreFailedAgentGUIHomeDraft
} from "./agentGuiController.homeDraftHelpers";
import { useAgentGUISubmitInteractionActions } from "./useAgentGUISubmitInteractionActions";
import { agentGUISubmitSettlementError } from "./agentGuiController.errors";

const draftKey = "node-default:codex:local:codex";

it("preserves a failed submit reason for package-owned presentation", () => {
  expect(
    agentGUISubmitSettlementError({
      errorCode: "workspace_operation_failed",
      errorMessage: "agent process cleanup is still pending",
      errorReason: "agent.process_cleanup_pending"
    })
  ).toMatchObject({
    code: "workspace_operation_failed",
    message: "agent process cleanup is still pending",
    reason: "agent.process_cleanup_pending"
  });
});

function draft(prompt: string): AgentComposerDraft {
  return [{ type: "text", text: prompt }];
}

function createSubmitInput() {
  const baseSession = {
    activeTurnId: null,
    agentSessionId: "session-1",
    cwd: "/workspace",
    latestTurnInteractions: [],
    pendingInteractions: [],
    provider: "codex",
    title: "Session",
    updatedAtUnixMs: 1,
    workspaceId: "workspace-1"
  };
  const sessionEngine = createAgentSessionEngine({
    clock: { nowUnixMs: () => 1 },
    commandPort: {
      kind: "typed",
      effects: {},
      execute: async () => undefined
    } as never,
    identity: { origin: "test", workspaceId: "workspace-1" },
    scheduler: { schedule: () => ({ cancel() {} }) }
  });
  sessionEngine.dispatch({ session: baseSession, type: "session/upserted" });
  const setDetailError = vi.fn();
  const draftByScopeKeyRef = {
    current: {} as Record<string, AgentComposerDraft>
  };
  const setDraftByScopeKey = vi.fn(
    (
      update:
        | Record<string, AgentComposerDraft>
        | ((
            current: Record<string, AgentComposerDraft>
          ) => Record<string, AgentComposerDraft>)
    ) => {
      draftByScopeKeyRef.current =
        typeof update === "function"
          ? update(draftByScopeKeyRef.current)
          : update;
    }
  );
  const input = {
    activation: {
      activate: vi.fn(),
      codeFor: vi.fn(() => null),
      errorFor: vi.fn(() => null)
    },
    activeConversationId: "session-1",
    activeConversationIdRef: { current: "session-1" },
    activeEngineActiveTurn: null,
    activeEnginePendingInteractions: [],
    agentActivityRuntime: { prompts: createTestPromptOperations() },
    conversationListQuery: {},
    conversationsRef: { current: [] },
    dataRef: { current: {} },
    draftByScopeKeyRef,
    executePromptRef: { current: vi.fn() },
    isComposerHomeRef: { current: false },
    isCurrentConversation: (agentSessionId: string) =>
      agentSessionId === "session-1",
    isRespondingToInteraction: false,
    isSessionMarkedNonResumable: () => false,
    persistActiveConversation: vi.fn(),
    promptImagesSupported: true,
    sessionEngine,
    setActiveConversationId: vi.fn(),
    setDetailError,
    setDraftByScopeKey,
    setIntent: vi.fn(),
    submittedDraftSnapshotsRef: { current: {} },
    startConversation: vi.fn(() => null),
    submitPromptRef: { current: vi.fn() },
    transientConversation: null,
    workspaceId: "workspace-1"
  } as unknown as Parameters<typeof useAgentGUISubmitInteractionActions>[0];
  return {
    input,
    draftByScopeKeyRef,
    sessionEngine,
    setDetailError,
    setDraftByScopeKey
  };
}

describe("new-conversation home draft lifecycle", () => {
  it("clears only the draft that still matches the submitted content", () => {
    const submitted = draft("first");
    const matching = { [draftKey]: draft("first") };
    const changed = { [draftKey]: draft("second") };

    expect(
      agentComposerDraftPrompt(
        clearSubmittedAgentGUIHomeDraft({
          draftKey,
          drafts: matching,
          submittedDraft: submitted
        })[draftKey]!
      )
    ).toBe("");
    expect(
      clearSubmittedAgentGUIHomeDraft({
        draftKey,
        drafts: changed,
        submittedDraft: submitted
      })
    ).toBe(changed);
  });

  it("restores a failed activation only when the home draft is still empty", () => {
    const empty = { [draftKey]: draft("") };
    const changed = { [draftKey]: draft("second") };
    const failure = {
      draftKey,
      submittedDraft: draft("first")
    };

    expect(
      agentComposerDraftPrompt(
        restoreFailedAgentGUIHomeDraft({ ...failure, drafts: empty })[draftKey]!
      )
    ).toBe("first");
    expect(
      restoreFailedAgentGUIHomeDraft({ ...failure, drafts: changed })
    ).toBe(changed);
  });
});

describe("conversation stop", () => {
  it("routes activation and active-turn stop through the prompt service", () => {
    const { input } = createSubmitInput();
    const stop = vi.spyOn(input.agentActivityRuntime.prompts, "stop");
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions(input)
    );

    act(() => result.current.interruptCurrentTurn("not running"));

    expect(stop).toHaveBeenCalledTimes(1);
    expect(stop).toHaveBeenCalledWith("session-1");
  });
});

describe("interaction submissions", () => {
  it("routes the explicit answer through the prompt service", () => {
    const { input, sessionEngine, setDetailError } = createSubmitInput();
    const submitInteractionResponse = vi.spyOn(input.agentActivityRuntime.prompts, "respondToInteraction")
      .mockReturnValue(true);
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions({
        ...input,
        activeEnginePendingInteractions: [
          {
            agentSessionId: "session-1",
            createdAtUnixMs: 1,
            kind: "question",
            requestId: "request-1",
            status: "pending",
            turnId: "turn-1",
            updatedAtUnixMs: 1
          }
        ]
      })
    );

    let admitted = false;
    act(() => {
      admitted = result.current.submitApprovalOption({
        agentSessionId: " session-1 ",
        optionId: " allow-once ",
        requestId: " request-1 ",
        turnId: " turn-1 "
      });
    });

    expect(admitted).toBe(true);
    expect(submitInteractionResponse).toHaveBeenCalledWith({
      agentSessionId: "session-1",
      optionId: "allow-once",
      requestId: "request-1",
      turnId: "turn-1"
    });
    expect(setDetailError).toHaveBeenCalledWith(null);
  });

  it("rechecks exact Host readiness at the interaction boundary", () => {
    const { input, sessionEngine } = createSubmitInput();
    const submitInteractionResponse = vi.spyOn(input.agentActivityRuntime.prompts, "respondToInteraction")
      .mockReturnValue(true);
    let readiness: "ready" | "blocked" = "blocked";
    const interactionReadinessSource = {
      getInteractionReadiness: vi.fn(() =>
        readiness === "ready"
          ? ({ status: "ready" } as const)
          : ({ status: "blocked", reason: "synchronizing" } as const)
      ),
      subscribe: vi.fn(() => () => undefined)
    };
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions({
        ...input,
        interactionReadinessSource,
        activeEnginePendingInteractions: [
          {
            agentSessionId: "session-1",
            createdAtUnixMs: 1,
            kind: "question",
            requestId: "request-1",
            status: "pending",
            turnId: "turn-1",
            updatedAtUnixMs: 1
          }
        ]
      })
    );

    let admitted = true;
    act(() => {
      admitted = result.current.submitApprovalOption({
        agentSessionId: "session-1",
        optionId: "allow",
        requestId: "request-1",
        turnId: "turn-1"
      });
    });
    expect(admitted).toBe(false);
    expect(submitInteractionResponse).not.toHaveBeenCalled();

    readiness = "ready";
    act(() => {
      admitted = result.current.submitApprovalOption({
        agentSessionId: "session-1",
        optionId: "allow",
        requestId: "request-1",
        turnId: "turn-1"
      });
    });

    expect(admitted).toBe(true);
    expect(
      interactionReadinessSource.getInteractionReadiness
    ).toHaveBeenLastCalledWith({
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      turnId: "turn-1",
      requestId: "request-1"
    });
    expect(submitInteractionResponse).toHaveBeenCalledTimes(1);
  });

  it("submits only the exact pending interaction when request ids repeat", () => {
    const { input, sessionEngine } = createSubmitInput();
    const submitInteractionResponse = vi.spyOn(input.agentActivityRuntime.prompts, "respondToInteraction")
      .mockReturnValue(true);
    const interactionReadinessSource = {
      getInteractionReadiness: vi.fn(() => ({ status: "ready" }) as const),
      subscribe: vi.fn(() => () => undefined)
    };
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions({
        ...input,
        interactionReadinessSource,
        activeEnginePendingInteractions: [
          {
            agentSessionId: "session-1",
            createdAtUnixMs: 1,
            kind: "question",
            requestId: "request-1",
            status: "pending",
            turnId: "turn-1",
            updatedAtUnixMs: 1
          },
          {
            agentSessionId: "session-2",
            createdAtUnixMs: 2,
            kind: "approval",
            requestId: "request-1",
            status: "pending",
            turnId: "turn-2",
            updatedAtUnixMs: 2
          }
        ]
      })
    );

    let admitted = false;
    act(() => {
      admitted = result.current.submitInteractivePrompt({
        action: "allow",
        agentSessionId: "session-1",
        requestId: "request-1",
        turnId: "turn-1"
      });
    });

    expect(admitted).toBe(true);
    expect(
      interactionReadinessSource.getInteractionReadiness
    ).toHaveBeenCalledWith({
      agentSessionId: "session-1",
      requestId: "request-1",
      turnId: "turn-1",
      workspaceId: "workspace-1"
    });
    expect(submitInteractionResponse).toHaveBeenCalledWith({
      action: "allow",
      agentSessionId: "session-1",
      requestId: "request-1",
      turnId: "turn-1"
    });
  });

  it("rejects a response without an exact pending interaction identity", () => {
    const { input, sessionEngine } = createSubmitInput();
    const submitInteractionResponse = vi.spyOn(input.agentActivityRuntime.prompts, "respondToInteraction");
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions(input)
    );

    let admitted = true;
    act(() => {
      admitted = result.current.submitInteractivePrompt({
        requestId: "request-1"
      });
    });

    expect(admitted).toBe(false);
    expect(submitInteractionResponse).not.toHaveBeenCalled();
  });
});

describe("existing-session prompt submission", () => {
  it("captures the exact active Turn for guidance before routing to the Engine", () => {
    const { input, sessionEngine } = createSubmitInput();
    input.activeEngineActiveTurn = { turnId: "turn-target" } as never;
    const submitPrompt = vi
      .spyOn(sessionEngine, "submitPrompt")
      .mockReturnValue({ accepted: true, queued: false });
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions(input)
    );

    act(() =>
      result.current.submitGuidancePrompt(
        [{ type: "text", text: "steer this turn" }],
        undefined,
        {
          capabilityRefs: [{ capability: "tutti", source: "slash_command" }]
        }
      )
    );

    expect(submitPrompt).toHaveBeenCalledWith(
      expect.objectContaining({
        agentSessionId: "session-1",
        capabilityRefs: [{ capability: "tutti", source: "slash_command" }],
        routing: "send_now",
        targetTurnId: "turn-target"
      })
    );
  });

  it("answers the exact pending question from the ordinary composer without sending a prompt", () => {
    const { input, draftByScopeKeyRef, sessionEngine, setDraftByScopeKey } =
      createSubmitInput();
    input.activeEngineActiveTurn = { turnId: "turn-target" } as never;
    input.activeEnginePendingInteractions = [
      {
        agentSessionId: "session-1",
        createdAtUnixMs: 1,
        input: {
          questions: [
            {
              allowFreeText: true,
              header: "Execution",
              id: "execution-mode",
              options: [{ label: "Safe", description: "Check first" }],
              question: "How should the task continue?"
            }
          ]
        },
        kind: "question",
        requestId: "request-1",
        status: "pending",
        turnId: "turn-target",
        updatedAtUnixMs: 1
      }
    ];
    draftByScopeKeyRef.current = {
      "session:session-1": draft("Continue with a different direction")
    };
    const submitInteractionResponse = vi.spyOn(input.agentActivityRuntime.prompts, "respondToInteraction")
      .mockReturnValue(true);
    const submitPrompt = vi.spyOn(sessionEngine, "submitPrompt");
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions(input)
    );

    act(() =>
      result.current.submitPrompt([
        { type: "text", text: "Continue with a different direction" }
      ])
    );

    expect(submitInteractionResponse).toHaveBeenCalledWith({
      action: "submit",
      agentSessionId: "session-1",
      payload: {
        answers: ["Continue with a different direction"],
        answersByQuestionId: {
          "execution-mode": "Continue with a different direction"
        }
      },
      requestId: "request-1",
      turnId: "turn-target"
    });
    expect(submitPrompt).not.toHaveBeenCalled();
    expect(setDraftByScopeKey).toHaveBeenCalledTimes(1);
    expect(
      agentComposerDraftPrompt(draftByScopeKeyRef.current["session:session-1"]!)
    ).toBe("");
  });

  it("clears the exact pending-question draft when the controller ref is stale", () => {
    const { input, draftByScopeKeyRef, sessionEngine, setDraftByScopeKey } =
      createSubmitInput();
    input.activeEngineActiveTurn = { turnId: "turn-target" } as never;
    input.activeEnginePendingInteractions = [
      {
        agentSessionId: "session-1",
        createdAtUnixMs: 1,
        input: {
          questions: [
            {
              allowFreeText: true,
              header: "Execution",
              id: "execution-mode",
              options: [],
              question: "How should the task continue?"
            }
          ]
        },
        kind: "question",
        requestId: "request-1",
        status: "pending",
        turnId: "turn-target",
        updatedAtUnixMs: 1
      }
    ];
    const submittedDraft = draft("Continue with the exact draft");
    draftByScopeKeyRef.current = {
      "session:session-1": draft("stale projection")
    };
    vi.spyOn(input.agentActivityRuntime.prompts, "respondToInteraction").mockReturnValue(true);
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions(input)
    );

    setDraftByScopeKey.mockImplementationOnce((update) => {
      const current = { "session:session-1": submittedDraft };
      const next = typeof update === "function" ? update(current) : update;
      draftByScopeKeyRef.current = next;
    });

    act(() =>
      result.current.submitPrompt(
        [{ type: "text", text: "Continue with the exact draft" }],
        undefined,
        { submittedDraft }
      )
    );

    expect(
      agentComposerDraftPrompt(draftByScopeKeyRef.current["session:session-1"]!)
    ).toBe("");
  });

  it("keeps the composer draft when the pending question response is not admitted", () => {
    const { input, draftByScopeKeyRef, sessionEngine, setDraftByScopeKey } =
      createSubmitInput();
    input.activeEngineActiveTurn = { turnId: "turn-target" } as never;
    input.activeEnginePendingInteractions = [
      {
        agentSessionId: "session-1",
        createdAtUnixMs: 1,
        input: {
          questions: [
            {
              allowFreeText: true,
              header: "Execution",
              id: "execution-mode",
              options: [],
              question: "How should the task continue?"
            }
          ]
        },
        kind: "question",
        requestId: "request-1",
        status: "pending",
        turnId: "turn-target",
        updatedAtUnixMs: 1
      }
    ];
    draftByScopeKeyRef.current = {
      "session:session-1": draft("Continue with a different direction")
    };
    vi.spyOn(input.agentActivityRuntime.prompts, "respondToInteraction").mockReturnValue(false);
    const submitPrompt = vi.spyOn(sessionEngine, "submitPrompt");
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions(input)
    );

    act(() =>
      result.current.submitPrompt([
        { type: "text", text: "Continue with a different direction" }
      ])
    );

    expect(submitPrompt).not.toHaveBeenCalled();
    expect(setDraftByScopeKey).not.toHaveBeenCalled();
    expect(
      agentComposerDraftPrompt(draftByScopeKeyRef.current["session:session-1"]!)
    ).toBe("Continue with a different direction");
  });

  it("keeps fixed-choice questions on the ordinary queue path", () => {
    const { input, sessionEngine } = createSubmitInput();
    input.activeEngineActiveTurn = { turnId: "turn-target" } as never;
    input.activeEnginePendingInteractions = [
      {
        agentSessionId: "session-1",
        createdAtUnixMs: 1,
        input: {
          questions: [
            {
              allowFreeText: false,
              header: "Execution",
              id: "execution-mode",
              options: [{ label: "Safe", description: "Check first" }],
              question: "How should the task continue?"
            }
          ]
        },
        kind: "question",
        requestId: "request-1",
        status: "pending",
        turnId: "turn-target",
        updatedAtUnixMs: 1
      }
    ];
    const submitInteractionResponse = vi.spyOn(input.agentActivityRuntime.prompts, "respondToInteraction");
    const submitPrompt = vi
      .spyOn(sessionEngine, "submitPrompt")
      .mockReturnValue({ accepted: true, queued: true });
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions(input)
    );

    act(() =>
      result.current.submitPrompt([{ type: "text", text: "You decide" }])
    );

    expect(submitInteractionResponse).not.toHaveBeenCalled();
    expect(submitPrompt).toHaveBeenCalledWith(
      expect.not.objectContaining({ routing: "send_now" })
    );
  });

  it("keeps capability-bearing text on the ordinary queue path", () => {
    const { input, sessionEngine } = createSubmitInput();
    input.activeEngineActiveTurn = { turnId: "turn-target" } as never;
    input.activeEnginePendingInteractions = [
      {
        agentSessionId: "session-1",
        createdAtUnixMs: 1,
        input: {
          questions: [
            {
              allowFreeText: true,
              header: "Execution",
              id: "execution-mode",
              options: [],
              question: "How should the task continue?"
            }
          ]
        },
        kind: "question",
        requestId: "request-1",
        status: "pending",
        turnId: "turn-target",
        updatedAtUnixMs: 1
      }
    ];
    const submitInteractionResponse = vi.spyOn(input.agentActivityRuntime.prompts, "respondToInteraction");
    const submitPrompt = vi
      .spyOn(sessionEngine, "submitPrompt")
      .mockReturnValue({ accepted: true, queued: true });
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions(input)
    );

    act(() =>
      result.current.submitPrompt(
        [{ type: "text", text: "Continue with Tutti" }],
        undefined,
        {
          capabilityRefs: [{ capability: "tutti", source: "slash_command" }]
        }
      )
    );

    expect(submitInteractionResponse).not.toHaveBeenCalled();
    expect(submitPrompt).toHaveBeenCalledWith(
      expect.objectContaining({
        capabilityRefs: [{ capability: "tutti", source: "slash_command" }]
      })
    );
  });

  it("keeps ordinary queue routing when the pending interaction is not a question", () => {
    const { input, sessionEngine } = createSubmitInput();
    input.activeEngineActiveTurn = { turnId: "turn-target" } as never;
    input.activeEnginePendingInteractions = [
      {
        agentSessionId: "session-1",
        createdAtUnixMs: 1,
        kind: "approval",
        requestId: "request-1",
        status: "pending",
        turnId: "turn-target",
        updatedAtUnixMs: 1
      }
    ];
    const submitPrompt = vi
      .spyOn(sessionEngine, "submitPrompt")
      .mockReturnValue({ accepted: true, queued: true });
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions(input)
    );

    act(() =>
      result.current.submitPrompt([{ type: "text", text: "Queue this" }])
    );

    expect(submitPrompt).toHaveBeenCalledWith(
      expect.not.objectContaining({ routing: "send_now" })
    );
  });

  it("routes submission through the Engine semantic operation", () => {
    const { input, draftByScopeKeyRef, sessionEngine, setDraftByScopeKey } =
      createSubmitInput();
    draftByScopeKeyRef.current = {
      "session:session-1": draft("continue")
    };
    const submitPrompt = vi
      .spyOn(sessionEngine, "submitPrompt")
      .mockReturnValue({ accepted: true, queued: false });
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions(input)
    );

    act(() =>
      result.current.submitPrompt(
        [{ type: "text", text: "continue" }],
        " Continue ",
        {
          capabilityRefs: [{ capability: "tutti", source: "slash_command" }],
          requiredSettingsPatch: { computerUse: true }
        }
      )
    );

    expect(submitPrompt).toHaveBeenCalledWith({
      agentSessionId: "session-1",
      capabilityRefs: [{ capability: "tutti", source: "slash_command" }],
      clientSubmitId: expect.any(String),
      content: [{ type: "text", text: "continue" }],
      displayPrompt: " Continue ",
      requiredSettingsPatch: { computerUse: true },
      runtimeContent: [{ type: "text", text: "continue" }],
      submitDiagnostics: expect.objectContaining({
        source: "agent-gui"
      })
    });
    expect(setDraftByScopeKey).toHaveBeenCalledTimes(1);
    expect(
      agentComposerDraftPrompt(draftByScopeKeyRef.current["session:session-1"]!)
    ).toBe("");
  });

  it("clears the draft captured by the Composer when the controller ref is stale", () => {
    const { input, draftByScopeKeyRef, sessionEngine, setDraftByScopeKey } =
      createSubmitInput();
    const submittedDraft = draft("continue");
    draftByScopeKeyRef.current = {
      "session:session-1": draft("stale projection")
    };
    const submitPrompt = vi
      .spyOn(sessionEngine, "submitPrompt")
      .mockReturnValue({ accepted: true, queued: true });
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions(input)
    );

    setDraftByScopeKey.mockImplementationOnce((update) => {
      const current = { "session:session-1": submittedDraft };
      const next = typeof update === "function" ? update(current) : update;
      draftByScopeKeyRef.current = next;
    });

    act(() =>
      result.current.submitPrompt(
        [{ type: "text", text: "continue" }],
        undefined,
        {
          submittedDraft
        }
      )
    );

    expect(submitPrompt).toHaveBeenCalledWith(
      expect.objectContaining({
        content: [{ type: "text", text: "continue" }]
      })
    );
    expect(
      agentComposerDraftPrompt(draftByScopeKeyRef.current["session:session-1"]!)
    ).toBe("");
  });

  it("keeps the draft when the Engine does not admit the submission", () => {
    const { input, draftByScopeKeyRef, sessionEngine, setDraftByScopeKey } =
      createSubmitInput();
    draftByScopeKeyRef.current = {
      "session:session-1": draft("continue")
    };
    vi.spyOn(sessionEngine, "submitPrompt").mockReturnValue({
      accepted: false,
      queued: false
    });
    const { result } = renderHook(() =>
      useAgentGUISubmitInteractionActions(input)
    );

    act(() =>
      result.current.submitPrompt([{ type: "text", text: "continue" }])
    );

    expect(setDraftByScopeKey).not.toHaveBeenCalled();
    expect(
      agentComposerDraftPrompt(draftByScopeKeyRef.current["session:session-1"]!)
    ).toBe("continue");
  });
});
