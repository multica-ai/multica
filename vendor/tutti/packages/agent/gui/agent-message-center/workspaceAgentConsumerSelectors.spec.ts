import { describe, expect, it } from "vitest";
import {
  createAgentSessionEngine,
  normalizeAgentActivitySession,
  type AgentActivitySession
} from "@tutti-os/agent-activity-core";
import { createTestEngineCommandPort } from "../shared/testing/createTestAgentSessionEngine";
import {
  buildWorkspaceAgentMessageCenterModelFromEngine,
  selectWorkspaceAgentAttentionItems,
  selectWorkspaceAgentMessageCenterPresentation,
  workspaceAgentAttentionItemsEqual,
  workspaceAgentMessageCenterPromptStatus,
  workspaceAgentMessageCenterPresentationEqual
} from "./workspaceAgentMessageCenterEngineModel";
import { selectMessageCenterAttentionDeckItems } from "./workspaceAgentMessageCenterModel";
import {
  selectWorkspaceAgentConsumerCounts,
  selectWorkspaceAgentConsumerSessions
} from "./workspaceAgentConsumerSelectors";

describe("workspaceAgentConsumerSelectors", () => {
  it("projects every pending target with exact target-level identity", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [
        session({
          activeTurnId: "turn-1",
          activeTurn: {
            turnId: "turn-1",
            agentSessionId: "session-1",
            origin: "user_prompt",
            phase: "waiting",
            startedAtUnixMs: 10,
            updatedAtUnixMs: 22
          },
          pendingInteractions: [
            {
              requestId: "request-approval",
              agentSessionId: "session-1",
              turnId: "turn-1",
              kind: "approval",
              status: "pending",
              toolName: "Bash",
              input: {
                command: "pnpm test",
                options: [{ optionId: "allow", label: "Allow" }]
              },
              createdAtUnixMs: 21,
              updatedAtUnixMs: 21
            },
            {
              requestId: "request-question",
              agentSessionId: "session-1",
              turnId: "turn-1",
              kind: "question",
              status: "pending",
              toolName: "AskUserQuestion",
              input: {
                questions: [
                  {
                    question: "Which option?",
                    options: [{ label: "First" }]
                  }
                ]
              },
              createdAtUnixMs: 22,
              updatedAtUnixMs: 22
            }
          ]
        })
      ]
    });

    const first = selectWorkspaceAgentAttentionItems(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      { workspaceId: "workspace-1", sessionMessagesById: {} }
    );
    const second = selectWorkspaceAgentAttentionItems(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      { workspaceId: "workspace-1", sessionMessagesById: {} }
    );

    expect(first).toHaveLength(2);
    expect(first.map((entry) => entry.item.id)).toEqual([
      "workspace-1\nsession-1\nturn-1\nrequest-approval",
      "workspace-1\nsession-1\nturn-1\nrequest-question"
    ]);
    expect(first.map((entry) => entry.target)).toEqual([
      {
        kind: "interaction",
        workspaceId: "workspace-1",
        agentSessionId: "session-1",
        turnId: "turn-1",
        requestId: "request-approval"
      },
      {
        kind: "interaction",
        workspaceId: "workspace-1",
        agentSessionId: "session-1",
        turnId: "turn-1",
        requestId: "request-question"
      }
    ]);
    expect(workspaceAgentAttentionItemsEqual(first, second)).toBe(true);
  });

  it("normalizes canonical exit-plan interactions for the shared prompt surface", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [
        session({
          activeTurnId: "turn-1",
          activeTurn: {
            turnId: "turn-1",
            agentSessionId: "session-1",
            origin: "user_prompt",
            phase: "waiting",
            startedAtUnixMs: 10,
            updatedAtUnixMs: 21
          },
          pendingInteractions: [
            {
              requestId: "request-exit-plan",
              agentSessionId: "session-1",
              turnId: "turn-1",
              kind: "approval",
              status: "pending",
              toolName: "ExitPlanMode",
              input: {
                toolCall: { kind: "switch_mode" },
                options: [
                  { optionId: "acceptEdits", label: "Accept edits" },
                  { optionId: "plan", label: "Keep planning" }
                ]
              },
              createdAtUnixMs: 21,
              updatedAtUnixMs: 21
            }
          ]
        })
      ]
    });

    const [entry] = selectWorkspaceAgentAttentionItems(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      { workspaceId: "workspace-1", sessionMessagesById: {} }
    );

    expect(entry?.item.pendingPrompt).toEqual(
      expect.objectContaining({
        kind: "exit-plan",
        requestId: "request-exit-plan",
        keepPlanningOptionId: "plan",
        options: [expect.objectContaining({ id: "acceptEdits" })]
      })
    );
  });

  it("projects an eligible completed plan turn as a canonical plan target", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [
        session({
          latestTurn: {
            turnId: "turn-plan",
            agentSessionId: "session-1",
            origin: "user_prompt",
            phase: "settled",
            outcome: "completed",
            startedAtUnixMs: 10,
            settledAtUnixMs: 20,
            updatedAtUnixMs: 20
          },
          capabilities: {
            imageInput: false,
            modelImageInputRequired: false,
            modelPlanBinding: false,
            modelSwitch: false,
            skills: false,
            compact: false,
            tokenUsage: false,
            rateLimits: false,
            planMode: true,
            interrupt: false,
            activeTurnGuidance: false,
            browserUse: false,
            computerUse: false,
            goalPause: false,
            planImplementation: true,
            permissionModeChangeDuringTurn: false,
            permissionModeChangeDeferred: false,
            review: false,
            resumeRunningTurn: false
          }
        })
      ]
    });

    const [entry] = selectWorkspaceAgentAttentionItems(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      {
        workspaceId: "workspace-1",
        sessionMessagesById: {
          "session-1": [
            {
              workspaceId: "workspace-1",
              agentSessionId: "session-1",
              messageId: "plan-message",
              version: 1,
              turnId: "turn-plan",
              role: "agent",
              kind: "message",
              payload: { messageKind: "plan" },
              occurredAtUnixMs: 19
            }
          ]
        }
      }
    );

    expect(entry).toMatchObject({
      status: "idle",
      target: {
        kind: "plan-implementation",
        workspaceId: "workspace-1",
        agentSessionId: "session-1",
        turnId: "turn-plan",
        requestId: "turn-plan"
      },
      item: {
        id: "workspace-1\nsession-1\nturn-plan\nturn-plan",
        pendingPrompt: {
          kind: "plan-implementation",
          requestId: "turn-plan"
        }
      }
    });
  });

  it("keeps a completed plan waiting in the production message center model when host messages are empty", () => {
    const engine = createCompletedPlanEngine({
      latestTurnId: "turn-plan",
      planTurnId: "turn-plan",
      capabilities: planImplementationCapabilities()
    });

    const model = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      { workspaceId: "workspace-1", sessionMessagesById: {} }
    );

    expect(model.waitingCount).toBe(1);
    expect(model.items[0]).toMatchObject({
      id: "message-center-session-1",
      agentSessionId: "session-1",
      status: "waiting",
      needsAttentionKind: "constraint",
      pendingInteractionTarget: null,
      pendingPrompt: {
        kind: "plan-implementation",
        requestId: "turn-plan"
      },
      latestTurnOutcome: null
    });
  });

  it.each([
    {
      name: "the tagged plan belongs to an older turn",
      latestTurnId: "turn-current",
      planTurnId: "turn-old",
      capabilities: planImplementationCapabilities()
    },
    {
      name: "the session explicitly disables plan implementation",
      latestTurnId: "turn-plan",
      planTurnId: "turn-plan",
      capabilities: {
        ...planImplementationCapabilities(),
        planImplementation: false
      }
    }
  ])("does not synthesize plan waiting when $name", (input) => {
    const engine = createCompletedPlanEngine(input);

    const model = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      { workspaceId: "workspace-1", sessionMessagesById: {} }
    );

    expect(model.waitingCount).toBe(0);
    expect(model.items[0]).toMatchObject({
      status: "completed",
      pendingPrompt: null,
      latestTurnOutcome: {
        status: "completed",
        turnId: input.latestTurnId
      }
    });
  });

  it("keeps the legacy presentation shape compatible and fails closed", () => {
    const engine = createCompletedPlanEngine({
      latestTurnId: "turn-plan",
      planTurnId: "turn-plan",
      capabilities: planImplementationCapabilities()
    });
    const {
      awaitingPlanImplementationTurnIdBySessionId: _derivedPlanWait,
      ...legacyPresentation
    } = selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot());

    const model = buildWorkspaceAgentMessageCenterModelFromEngine(
      legacyPresentation,
      {
        workspaceId: "workspace-1",
        sessionMessagesById: { "session-1": [planMessage("turn-plan")] }
      }
    );

    expect(model.waitingCount).toBe(0);
    expect(model.items[0]).toMatchObject({
      status: "completed",
      pendingPrompt: null
    });
  });

  it("keeps a durable interaction authoritative over a synthetic plan wait", () => {
    const engine = createCompletedPlanEngine({
      latestTurnId: "turn-plan",
      planTurnId: "turn-plan",
      capabilities: planImplementationCapabilities(),
      pendingInteractions: [
        {
          requestId: "request-approval",
          agentSessionId: "session-1",
          turnId: "turn-plan",
          kind: "approval",
          status: "pending",
          toolName: "Bash",
          input: {
            command: "pnpm test",
            options: [{ optionId: "allow", label: "Allow" }]
          },
          createdAtUnixMs: 21,
          updatedAtUnixMs: 21
        }
      ]
    });

    const model = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      { workspaceId: "workspace-1", sessionMessagesById: {} }
    );

    expect(model.waitingCount).toBe(1);
    expect(model.items[0]).toMatchObject({
      status: "waiting",
      pendingInteractionTarget: {
        agentSessionId: "session-1",
        requestId: "request-approval",
        turnId: "turn-plan"
      },
      pendingPrompt: {
        kind: "approval",
        requestId: "request-approval"
      }
    });
  });

  it("keeps message-center presentation equal across unrelated engine changes", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [session({})]
    });
    const before = selectWorkspaceAgentMessageCenterPresentation(
      engine.getSnapshot()
    );
    engine.dispatch({ type: "engine/connectionChanged", status: "connected" });
    const after = selectWorkspaceAgentMessageCenterPresentation(
      engine.getSnapshot()
    );
    expect(workspaceAgentMessageCenterPresentationEqual(before, after)).toBe(
      true
    );
  });
  it("derives shared consumer state from canonical turn and interaction entities", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [
        session({
          activeTurnId: "turn-1",
          activeTurn: {
            turnId: "turn-1",
            agentSessionId: "session-1",
            origin: "user_prompt",
            phase: "running",
            startedAtUnixMs: 10,
            updatedAtUnixMs: 20
          },
          pendingInteractions: [
            {
              requestId: "request-1",
              agentSessionId: "session-1",
              turnId: "turn-1",
              kind: "question",
              status: "pending",
              input: { question: "Choose an option" },
              createdAtUnixMs: 21,
              updatedAtUnixMs: 21
            }
          ]
        })
      ]
    });

    const consumers = selectWorkspaceAgentConsumerSessions(
      engine.getSnapshot()
    );
    expect(consumers).toHaveLength(1);
    expect(consumers[0]?.displayStatus).toBe("waiting");
    expect(selectWorkspaceAgentConsumerCounts(engine.getSnapshot())).toEqual({
      canceled: 0,
      completed: 0,
      failed: 0,
      idle: 0,
      waiting: 1,
      working: 0
    });

    const model = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      { workspaceId: "workspace-1", sessionMessagesById: {} }
    );
    expect(model.items[0]).toMatchObject({
      agentSessionId: "session-1",
      needsAttentionKind: "question",
      needsAttentionSummary: "Choose an option",
      pendingPrompt: {
        agentSessionId: "session-1",
        kind: "ask-user",
        requestId: "request-1",
        turnId: "turn-1"
      },
      status: "waiting"
    });

    engine.dispatch({
      type: "interaction/upserted",
      interaction: {
        requestId: "request-1",
        agentSessionId: "session-1",
        turnId: "turn-1",
        kind: "question",
        status: "answered",
        input: { question: "Choose an option" },
        createdAtUnixMs: 21,
        updatedAtUnixMs: 30
      }
    });
    const resolvedModel = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      { workspaceId: "workspace-1", sessionMessagesById: {} }
    );
    expect(resolvedModel.items[0]?.pendingPrompt).toBeNull();
    expect(selectMessageCenterAttentionDeckItems(resolvedModel.items)).toEqual(
      []
    );
  });

  it("aggregates a child approval into its root conversation card", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [
        session({
          activeTurnId: "root-turn-1",
          activeTurn: {
            turnId: "root-turn-1",
            agentSessionId: "session-1",
            origin: "user_prompt",
            phase: "running",
            startedAtUnixMs: 10,
            updatedAtUnixMs: 20
          },
          provider: "claude-code"
        }),
        session({
          activeTurnId: "child-turn-1",
          activeTurn: {
            turnId: "child-turn-1",
            agentSessionId: "child-1",
            origin: "provider_initiated",
            phase: "waiting",
            startedAtUnixMs: 15,
            updatedAtUnixMs: 25
          },
          agentSessionId: "child-1",
          kind: "child",
          parentAgentSessionId: "session-1",
          parentToolCallId: "toolu-agent-1",
          parentTurnId: "root-turn-1",
          pendingInteractions: [
            {
              requestId: "child-approval-1",
              agentSessionId: "child-1",
              turnId: "child-turn-1",
              kind: "approval",
              status: "pending",
              toolName: "Bash",
              input: {
                question: "Allow Bash?",
                options: [{ optionId: "allow", label: "Allow" }]
              },
              createdAtUnixMs: 26,
              updatedAtUnixMs: 26
            }
          ],
          provider: "claude-code",
          providerSessionId: "agent-1",
          rootAgentSessionId: "session-1",
          rootTurnId: "root-turn-1",
          title: "Child"
        })
      ]
    });

    const presentation = selectWorkspaceAgentMessageCenterPresentation(
      engine.getSnapshot()
    );
    const model = buildWorkspaceAgentMessageCenterModelFromEngine(
      presentation,
      { workspaceId: "workspace-1", sessionMessagesById: {} }
    );

    expect(model.items).toHaveLength(1);
    expect(model.items[0]).toMatchObject({
      agentSessionId: "session-1",
      status: "waiting",
      pendingInteractionTarget: {
        agentSessionId: "child-1",
        requestId: "child-approval-1",
        turnId: "child-turn-1"
      },
      pendingPrompt: {
        kind: "approval",
        requestId: "child-approval-1",
        turnId: "child-turn-1"
      }
    });
    expect(model.waitingCount).toBe(1);
    expect(selectMessageCenterAttentionDeckItems(model.items)).toHaveLength(1);

    const respondingPresentation =
      selectWorkspaceAgentMessageCenterPresentation(
        engine.getSnapshot(),
        (agentSessionId, turnId, requestId) =>
          agentSessionId === "child-1" &&
          turnId === "child-turn-1" &&
          requestId === "child-approval-1"
            ? { status: "responding" }
            : null
      );
    expect(
      workspaceAgentMessageCenterPromptStatus(
        respondingPresentation,
        model.items[0]!
      )
    ).toBe("responding");

    engine.dispatch({
      type: "interaction/upserted",
      interaction: {
        requestId: "child-approval-1",
        agentSessionId: "child-1",
        turnId: "child-turn-1",
        kind: "approval",
        status: "answered",
        toolName: "Bash",
        input: { question: "Allow Bash?" },
        createdAtUnixMs: 26,
        updatedAtUnixMs: 30
      }
    });
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [
        session({
          activeTurnId: null,
          latestTurn: {
            turnId: "root-turn-1",
            agentSessionId: "session-1",
            origin: "user_prompt",
            phase: "settled",
            outcome: "completed",
            startedAtUnixMs: 10,
            settledAtUnixMs: 30,
            updatedAtUnixMs: 30
          },
          provider: "claude-code"
        }),
        session({
          activeTurnId: "child-turn-1",
          activeTurn: {
            turnId: "child-turn-1",
            agentSessionId: "child-1",
            origin: "provider_initiated",
            phase: "running",
            startedAtUnixMs: 15,
            updatedAtUnixMs: 31
          },
          agentSessionId: "child-1",
          kind: "child",
          parentAgentSessionId: "session-1",
          parentToolCallId: "toolu-agent-1",
          parentTurnId: "root-turn-1",
          provider: "claude-code",
          providerSessionId: "agent-1",
          rootAgentSessionId: "session-1",
          rootTurnId: "root-turn-1",
          title: "Child"
        })
      ]
    });
    const resumedModel = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      { workspaceId: "workspace-1", sessionMessagesById: {} }
    );
    expect(resumedModel.items).toHaveLength(1);
    expect(resumedModel.items[0]).toMatchObject({
      agentSessionId: "session-1",
      latestTurnOutcome: null,
      pendingPrompt: null,
      status: "working"
    });
  });

  it("projects signed Agent Target presentation for open-provider sessions", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [
        session({
          agentTargetId: "extension:gemini",
          provider: "acp:gemini"
        })
      ]
    });

    const model = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      { workspaceId: "workspace-1", sessionMessagesById: {} },
      {
        agentPresentations: [
          {
            agentTargetId: "extension:gemini",
            iconUrl: "data:image/svg+xml;base64,gemini",
            name: "Gemini CLI"
          }
        ]
      }
    );

    expect(model.items[0]).toMatchObject({
      agentTargetId: "extension:gemini",
      agentName: "Gemini CLI",
      agentAvatarUrl: "data:image/svg+xml;base64,gemini",
      provider: "acp:gemini"
    });
  });

  it("keeps canonical settled turn state authoritative over stale status messages", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [
        session({
          activeTurnId: null,
          activeTurn: {
            turnId: "turn-completed",
            agentSessionId: "session-1",
            origin: "user_prompt",
            phase: "settled",
            outcome: "completed",
            startedAtUnixMs: 10,
            settledAtUnixMs: 20,
            updatedAtUnixMs: 20
          }
        })
      ]
    });

    const model = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      {
        workspaceId: "workspace-1",
        sessionMessagesById: {
          "session-1": [
            {
              workspaceId: "workspace-1",
              agentSessionId: "session-1",
              messageId: "stale-question",
              version: 1,
              turnId: "turn-stale",
              role: "agent",
              kind: "question",
              status: "failed",
              payload: {
                input: {
                  questions: [{ question: "Stale question" }]
                }
              },
              occurredAtUnixMs: 5
            }
          ]
        }
      }
    );

    expect(model.items[0]).toMatchObject({
      status: "completed",
      pendingPrompt: null,
      needsAttentionKind: null,
      latestTurnOutcome: {
        status: "completed",
        turnId: "turn-completed"
      }
    });
    expect(model.waitingCount).toBe(0);
  });

  it("keeps the session title for a continued historical session", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [session({ title: "Original request" })]
    });

    const model = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      {
        workspaceId: "workspace-1",
        sessionMessagesById: {
          "session-1": [
            {
              workspaceId: "workspace-1",
              agentSessionId: "session-1",
              messageId: "user-old",
              version: 1,
              turnId: "turn-old",
              role: "user",
              kind: "message",
              payload: { text: "Original request" },
              occurredAtUnixMs: 10
            },
            {
              workspaceId: "workspace-1",
              agentSessionId: "session-1",
              messageId: "user-latest",
              version: 2,
              turnId: "turn-latest",
              role: "user",
              kind: "message",
              payload: { text: "Continue with the latest request" },
              occurredAtUnixMs: 20
            }
          ]
        }
      }
    );

    expect(model.items[0]?.title).toBe("Original request");
  });

  it("keeps the session title and waiting plan prompt", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [
        session({
          title: "Original request",
          activeTurnId: "turn-plan",
          activeTurn: {
            turnId: "turn-plan",
            agentSessionId: "session-1",
            origin: "user_prompt",
            phase: "waiting",
            startedAtUnixMs: 10,
            updatedAtUnixMs: 20
          },
          pendingInteractions: [
            {
              requestId: "request-exit-plan",
              agentSessionId: "session-1",
              turnId: "turn-plan",
              kind: "approval",
              status: "pending",
              toolName: "ExitPlanMode",
              input: {
                toolCall: { kind: "switch_mode" },
                options: [{ optionId: "acceptEdits", label: "Accept edits" }]
              },
              createdAtUnixMs: 20,
              updatedAtUnixMs: 20
            }
          ]
        })
      ]
    });

    const model = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      {
        workspaceId: "workspace-1",
        sessionMessagesById: {
          "session-1": [
            {
              workspaceId: "workspace-1",
              agentSessionId: "session-1",
              messageId: "user-plan",
              version: 1,
              turnId: "turn-plan",
              role: "user",
              kind: "message",
              payload: { text: "Implement the revised plan" },
              occurredAtUnixMs: 19
            }
          ]
        }
      }
    );

    expect(model.items[0]).toMatchObject({
      title: "Original request",
      status: "waiting",
      pendingPrompt: {
        kind: "exit-plan",
        requestId: "request-exit-plan"
      }
    });
  });

  it("uses message version to select the latest reply when timestamps match", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [session({ title: "Original request" })]
    });

    const model = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      {
        workspaceId: "workspace-1",
        sessionMessagesById: {
          "session-1": [
            {
              workspaceId: "workspace-1",
              agentSessionId: "session-1",
              messageId: "assistant-latest",
              version: 2,
              turnId: "turn-latest",
              role: "assistant",
              kind: "message",
              payload: { text: "Current reply" },
              occurredAtUnixMs: 20
            },
            {
              workspaceId: "workspace-1",
              agentSessionId: "session-1",
              messageId: "assistant-previous",
              version: 1,
              turnId: "turn-previous",
              role: "assistant",
              kind: "message",
              payload: { text: "Previous reply" },
              occurredAtUnixMs: 20
            }
          ]
        }
      }
    );

    expect(model.items[0]).toMatchObject({
      title: "Original request",
      lastAgentMessageSummary: "Current reply",
      digest: {
        primary: {
          summary: "Current reply"
        }
      }
    });
  });

  it("updates the recent summary only when a realtime reply becomes durable", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [session({ title: "Original request" })]
    });
    const previousReply = {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      messageId: "assistant-previous",
      version: 1,
      sequence: 1,
      turnId: "turn-previous",
      role: "assistant",
      kind: "message",
      payload: { text: "Previous reply" },
      occurredAtUnixMs: 10
    } as const;
    const streamingReply = {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      messageId: "assistant-current",
      version: 0,
      turnId: "turn-current",
      role: "assistant",
      kind: "message",
      status: "streaming",
      payload: { text: "Current reply in progress" },
      occurredAtUnixMs: 20
    } as const;

    const streamingModel = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      {
        workspaceId: "workspace-1",
        sessionMessagesById: {
          "session-1": [previousReply, streamingReply]
        }
      }
    );
    expect(streamingModel.items[0]?.lastAgentMessageSummary).toBe(
      "Previous reply"
    );

    const completedModel = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      {
        workspaceId: "workspace-1",
        sessionMessagesById: {
          "session-1": [
            previousReply,
            { ...streamingReply, sequence: 2, status: "completed", version: 2 }
          ]
        }
      }
    );
    expect(completedModel.items[0]).toMatchObject({
      title: "Original request",
      lastAgentMessageSummary: "Current reply in progress",
      digest: { primary: { summary: "Current reply in progress" } }
    });
  });

  it("omits hidden sessions from the message center model by default", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [session({ visible: false })]
    });

    const model = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      { workspaceId: "workspace-1", sessionMessagesById: {} }
    );

    expect(model.items).toHaveLength(0);
  });

  it("does not treat a provider-native id as a hidden-session opt-in", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [
        session({
          visible: false,
          providerSessionId: "provider-session-1"
        })
      ]
    });

    const model = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      { workspaceId: "workspace-1", sessionMessagesById: {} },
      { includeHiddenSessionIds: ["provider-session-1"] }
    );

    expect(model.items).toHaveLength(0);
  });

  it("keeps an allowlisted hidden delegate session and its pending prompt", () => {
    const engine = createEngine();
    engine.dispatch({
      type: "session/snapshotReceived",
      sessions: [
        session({
          visible: false,
          activeTurnId: "turn-1",
          activeTurn: {
            turnId: "turn-1",
            agentSessionId: "session-1",
            origin: "user_prompt",
            phase: "waiting",
            startedAtUnixMs: 10,
            updatedAtUnixMs: 21
          },
          pendingInteractions: [
            {
              requestId: "request-approval",
              agentSessionId: "session-1",
              turnId: "turn-1",
              kind: "approval",
              status: "pending",
              toolName: "Bash",
              input: {
                command: "pnpm test",
                options: [{ optionId: "allow", label: "Allow" }]
              },
              createdAtUnixMs: 21,
              updatedAtUnixMs: 21
            }
          ]
        })
      ]
    });

    const model = buildWorkspaceAgentMessageCenterModelFromEngine(
      selectWorkspaceAgentMessageCenterPresentation(engine.getSnapshot()),
      { workspaceId: "workspace-1", sessionMessagesById: {} },
      { includeHiddenSessionIds: ["session-1"] }
    );

    expect(model.items).toHaveLength(1);
    expect(model.items[0]).toMatchObject({
      agentSessionId: "session-1",
      needsAttentionKind: "permission",
      pendingInteractionTarget: {
        agentSessionId: "session-1",
        requestId: "request-approval",
        turnId: "turn-1"
      },
      pendingPrompt: {
        kind: "approval",
        requestId: "request-approval"
      }
    });
  });
});

function createEngine() {
  return createAgentSessionEngine({
    clock: { nowUnixMs: () => 1 },
    commandPort: createTestEngineCommandPort({ execute: async () => ({}) }),
    identity: { origin: "test", workspaceId: "workspace-1" },
    scheduler: { schedule: () => ({ cancel() {} }) }
  });
}

function session(
  overrides: Partial<AgentActivitySession> = {}
): AgentActivitySession {
  return normalizeAgentActivitySession({
    activeTurnId: null,
    latestTurnInteractions: [],
    pendingInteractions: [],
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    provider: "codex",
    cwd: "/workspace",
    title: "Canonical session",
    ...overrides
  });
}

function planMessage(turnId: string) {
  return {
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    messageId: `plan-${turnId}`,
    version: 1,
    turnId,
    role: "agent" as const,
    kind: "message" as const,
    payload: { messageKind: "plan" },
    occurredAtUnixMs: 19
  };
}

function planImplementationCapabilities() {
  return {
    imageInput: false,
    modelImageInputRequired: false,
    modelPlanBinding: false,
    modelSwitch: false,
    skills: false,
    compact: false,
    tokenUsage: false,
    rateLimits: false,
    planMode: true,
    interrupt: false,
    activeTurnGuidance: false,
    browserUse: false,
    computerUse: false,
    goalPause: false,
    planImplementation: true,
    permissionModeChangeDuringTurn: false,
    permissionModeChangeDeferred: false,
    review: false,
    resumeRunningTurn: false
  };
}

function createCompletedPlanEngine(input: {
  latestTurnId: string;
  planTurnId: string;
  capabilities: ReturnType<typeof planImplementationCapabilities>;
  pendingInteractions?: AgentActivitySession["pendingInteractions"];
}) {
  const engine = createEngine();
  engine.dispatch({
    type: "session/snapshotReceived",
    sessions: [
      session({
        latestTurn: {
          turnId: input.latestTurnId,
          agentSessionId: "session-1",
          origin: "user_prompt",
          phase: "settled",
          outcome: "completed",
          startedAtUnixMs: 10,
          settledAtUnixMs: 20,
          updatedAtUnixMs: 20
        },
        capabilities: input.capabilities,
        pendingInteractions: input.pendingInteractions ?? []
      })
    ]
  });
  engine.dispatch({
    type: "message/snapshotReceived",
    workspaceId: "workspace-1",
    messages: [planMessage(input.planTurnId)]
  });
  return engine;
}
