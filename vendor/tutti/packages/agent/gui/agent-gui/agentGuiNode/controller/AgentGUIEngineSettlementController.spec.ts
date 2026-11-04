import { createTestPromptOperations } from "../../../shared/testing/createTestPromptOperations";
import {
  normalizeAgentActivitySession,
  selectEngineHasVisibleQueuedSubmit,
  selectPendingSubmitsForSession,
  type AgentActivityInteraction,
  type AgentActivityTurn
} from "@tutti-os/agent-activity-core";
import { describe, expect, it, vi } from "vitest";
import {
  createTestAgentSessionEngine,
  createTestAgentSessionEngineWithEffects
} from "../../../shared/testing/createTestAgentSessionEngine";
import {
  agentComposerDraftPrompt,
  buildAgentComposerDraft,
  emptyAgentComposerDraft
} from "../model/agentComposerDraft";
import type {
  AgentComposerDraft,
  SubmittedDraftSnapshot
} from "../model/agentGuiNodeTypes";
import {
  agentGUIInteractionDraftSettlementKey,
  AgentGUIEngineSettlementController
} from "./AgentGUIEngineSettlementController";

describe("AgentGUIEngineSettlementController", () => {
  it("clears a matching home draft after activation confirmation", () => {
    const engine = createTestAgentSessionEngine();
    const sourceScopeKey = "project:/workspace/app";
    const submittedDraft: AgentComposerDraft = [
      { type: "text", text: "first" }
    ];
    const snapshots: Record<string, SubmittedDraftSnapshot> = {
      "submit-1": { content: submittedDraft, sourceScopeKey }
    };
    let drafts: Record<string, AgentComposerDraft> = {
      [sourceScopeKey]: submittedDraft
    };
    const controller = new AgentGUIEngineSettlementController({
      prompts: createTestPromptOperations(),
      applyDraftUpdate: (update) => {
        drafts = update(drafts);
      },
      engine,
      snapshots
    });
    const detach = controller.attach();

    requestActivation(engine, "submit-1");
    engine.dispatch({
      type: "session/upserted",
      session: normalizeAgentActivitySession({
        activeTurnId: null,
        agentSessionId: "session-1",
        createdAtUnixMs: Date.now() + 60_000,
        cwd: "/workspace/app",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        title: "first",
        workspaceId: "test-workspace"
      })
    });

    expect(agentComposerDraftPrompt(drafts[sourceScopeKey]!)).toBe("");
    expect(drafts["session:session-1"]).toEqual(emptyAgentComposerDraft());
    expect(snapshots).toEqual({});
    detach();
    engine.dispose();
  });

  it("seeds the new session draft with submitted connectors after home activation", () => {
    const engine = createTestAgentSessionEngine();
    const sourceScopeKey = "home";
    const submittedDraft = buildAgentComposerDraft({
      prompt: "first",
      connectors: [{ connectorKey: "lark-cli" }, { connectorKey: "github" }]
    });
    const snapshots: Record<string, SubmittedDraftSnapshot> = {
      "submit-1": { content: submittedDraft, sourceScopeKey }
    };
    let drafts: Record<string, AgentComposerDraft> = {
      [sourceScopeKey]: submittedDraft
    };
    const controller = new AgentGUIEngineSettlementController({
      prompts: createTestPromptOperations(),
      applyDraftUpdate: (update) => {
        drafts = update(drafts);
      },
      engine,
      snapshots
    });
    const detach = controller.attach();

    requestActivation(engine, "submit-1");
    engine.dispatch({
      type: "session/upserted",
      session: normalizeAgentActivitySession({
        activeTurnId: null,
        agentSessionId: "session-1",
        createdAtUnixMs: Date.now() + 60_000,
        cwd: "/workspace/app",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        title: "first",
        workspaceId: "test-workspace"
      })
    });

    expect(drafts[sourceScopeKey]).toEqual(emptyAgentComposerDraft());
    expect(drafts["session:session-1"]).toEqual(
      buildAgentComposerDraft({
        prompt: "",
        connectors: [{ connectorKey: "lark-cli" }, { connectorKey: "github" }]
      })
    );
    expect(snapshots).toEqual({});
    detach();
    engine.dispose();
  });

  it("restores a failed activation only while its home draft is empty", async () => {
    let rejectActivation: (error: Error) => void = vi.fn();
    const engine = createTestAgentSessionEngine("test-workspace", {
      execute(command) {
        if (command.type !== "session/activate") {
          return Promise.resolve({ ok: true });
        }
        return new Promise((_, reject) => {
          rejectActivation = reject;
        });
      }
    });
    const sourceScopeKey = "project:/workspace/app";
    const snapshots: Record<string, SubmittedDraftSnapshot> = {
      "submit-1": {
        content: [{ type: "text", text: "first" }],
        sourceScopeKey
      }
    };
    let drafts: Record<string, AgentComposerDraft> = {
      [sourceScopeKey]: emptyAgentComposerDraft()
    };
    const controller = new AgentGUIEngineSettlementController({
      prompts: createTestPromptOperations(),
      applyDraftUpdate: (update) => {
        drafts = update(drafts);
      },
      engine,
      snapshots
    });
    const detach = controller.attach();

    requestActivation(engine, "submit-1");
    rejectActivation(new Error("activation failed"));
    await vi.waitFor(() => {
      expect(agentComposerDraftPrompt(drafts[sourceScopeKey]!)).toBe("first");
    });
    expect(snapshots).toEqual({});
    detach();
    engine.dispose();
  });

  it("restores a composer answer when the interaction response fails", async () => {
    let responseStatus: "responding" | "failed" = "responding";
    let responseChanged: () => void = vi.fn();
    const prompts = createTestPromptOperations({
      interactionResponse: () => ({ status: responseStatus }),
      subscribeInteractionResponses: (listener) => {
        responseChanged = listener;
        return () => undefined;
      }
    });
    const engine = createTestAgentSessionEngine("test-workspace", {
      execute: () => Promise.resolve({ ok: true })
    });
    const { interaction, session } = waitingQuestionSession();
    engine.dispatch({ sessions: [session], type: "session/snapshotReceived" });
    const sourceScopeKey = "session:session-1";
    const submittedDraft: AgentComposerDraft = [
      { type: "text", text: "You decide" }
    ];
    const settlementKey = agentGUIInteractionDraftSettlementKey(interaction);
    const interactionDraftSettlements = {
      [settlementKey]: {
        agentSessionId: interaction.agentSessionId,
        requestId: interaction.requestId,
        submittedDraftSnapshot: {
          content: submittedDraft,
          sourceScopeKey,
          targetAgentSessionId: interaction.agentSessionId
        },
        turnId: interaction.turnId
      }
    };
    let drafts: Record<string, AgentComposerDraft> = {
      [sourceScopeKey]: emptyAgentComposerDraft()
    };
    const controller = new AgentGUIEngineSettlementController({
      prompts,
      applyDraftUpdate: (update) => {
        drafts = update(drafts);
      },
      engine,
      interactionDraftSettlements,
      snapshots: {}
    });
    const detach = controller.attach();

    expect(agentComposerDraftPrompt(drafts[sourceScopeKey]!)).toBe("");
    responseStatus = "failed";
    responseChanged();

    expect(agentComposerDraftPrompt(drafts[sourceScopeKey]!)).toBe(
      "You decide"
    );
    expect(interactionDraftSettlements).toEqual({});
    detach();
    engine.dispose();
  });

  it("keeps a composer answer cleared after the interaction is answered", () => {
    const engine = createTestAgentSessionEngine();
    const { interaction, session } = waitingQuestionSession();
    engine.dispatch({ sessions: [session], type: "session/snapshotReceived" });
    const sourceScopeKey = "session:session-1";
    const settlementKey = agentGUIInteractionDraftSettlementKey(interaction);
    const submittedDraft: AgentComposerDraft = [
      { type: "text", text: "You decide" }
    ];
    const interactionDraftSettlements = {
      [settlementKey]: {
        agentSessionId: interaction.agentSessionId,
        requestId: interaction.requestId,
        submittedDraftSnapshot: {
          content: submittedDraft,
          sourceScopeKey,
          targetAgentSessionId: interaction.agentSessionId
        },
        turnId: interaction.turnId
      }
    };
    let drafts: Record<string, AgentComposerDraft> = {
      [sourceScopeKey]: emptyAgentComposerDraft()
    };
    const controller = new AgentGUIEngineSettlementController({
      prompts: createTestPromptOperations(),
      applyDraftUpdate: (update) => {
        drafts = update(drafts);
      },
      engine,
      interactionDraftSettlements,
      snapshots: {}
    });
    const detach = controller.attach();

    engine.dispatch({
      interaction: { ...interaction, status: "answered", updatedAtUnixMs: 3 },
      type: "interaction/upserted"
    });

    expect(drafts[sourceScopeKey]).toEqual(emptyAgentComposerDraft());
    expect(interactionDraftSettlements).toEqual({});
    detach();
    engine.dispose();
  });
});

function waitingQuestionSession(): {
  interaction: AgentActivityInteraction;
  session: ReturnType<typeof normalizeAgentActivitySession>;
} {
  const turn: AgentActivityTurn = {
    agentSessionId: "session-1",
    origin: "user_prompt",
    phase: "waiting",
    startedAtUnixMs: 1,
    turnId: "turn-1",
    updatedAtUnixMs: 2
  };
  const interaction: AgentActivityInteraction = {
    agentSessionId: "session-1",
    createdAtUnixMs: 2,
    input: {
      questions: [
        {
          allowFreeText: true,
          header: "Execution",
          id: "execution-mode",
          options: [{ description: "Check first", label: "Safe" }],
          question: "How should the task continue?"
        }
      ]
    },
    kind: "question",
    requestId: "request-1",
    status: "pending",
    turnId: turn.turnId,
    updatedAtUnixMs: 2
  };
  return {
    interaction,
    session: normalizeAgentActivitySession({
      activeTurn: turn,
      activeTurnId: turn.turnId,
      agentSessionId: "session-1",
      cwd: "/workspace/app",
      latestTurn: turn,
      latestTurnInteractions: [interaction],
      pendingInteractions: [interaction],
      provider: "codex",
      title: "session",
      workspaceId: "test-workspace"
    })
  };
}

function requestActivation(
  engine: ReturnType<typeof createTestAgentSessionEngine>,
  clientSubmitId: string
): void {
  engine.dispatch({
    type: "activation/requested",
    agentSessionId: "session-1",
    agentTargetId: "local:codex",
    clientSubmitId,
    content: [{ type: "text", text: "first" }],
    cwd: "/workspace/app",
    expiresAtUnixMs: Date.now() + 45_000,
    mode: "new",
    requestedAtUnixMs: Date.now(),
    requestId: "request-1",
    workspaceId: "test-workspace"
  });
}
