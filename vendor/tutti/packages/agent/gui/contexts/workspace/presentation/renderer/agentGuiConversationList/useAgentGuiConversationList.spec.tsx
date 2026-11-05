import { act, renderHook } from "@testing-library/react";
import { type AgentActivityMessage } from "@tutti-os/agent-activity-core";
import { observable, runInAction } from "mobx";
import { beforeEach, describe, expect, it } from "vitest";
import { createLocalAgentGUIAgentTarget } from "../../../../../agentTargets";
import { createTestRuntimeState } from "../../../../../shared/testing/createTestRuntimeState";
import { useAgentGuiConversationList } from "./useAgentGuiConversationList";

const committed = observable.map<string, readonly AgentActivityMessage[]>([], {
  deep: false
});
const transcript = {
  messages: (id: string) => committed.get(id) ?? [],
  hasOlderMessages: () => false,
  conversationIds: () => [...committed.keys()]
};
const workspaceLoad = { status: "ready" as const, errorMessage: null };
function commitMessages(messages: readonly AgentActivityMessage[]) {
  runInAction(() => {
    for (const message of messages) {
      committed.set(message.agentSessionId, [
        ...(committed.get(message.agentSessionId) ?? []),
        message
      ]);
    }
  });
}

describe("useAgentGuiConversationList", () => {
  beforeEach(() => runInAction(() => committed.clear()));

  it("projects a child interaction as user action on its root conversation", () => {
    const engine = createTestRuntimeState("workspace-1");
    const query = {
      workspaceId: "workspace-1",
      userId: "user-1",
      provider: "claude-code" as const,
      sessionOrigin: "test"
    };
    const { result } = renderHook(() =>
      useAgentGuiConversationList(engine, transcript, workspaceLoad, query)
    );

    act(() => {
      engine.sessions.upsertSessions([
        {
          activeTurn: {
            agentSessionId: "root",
            origin: "user_prompt",
            phase: "waiting",
            startedAtUnixMs: 1,
            turnId: "root-turn",
            updatedAtUnixMs: 2
          },
          activeTurnId: "root-turn",
          agentSessionId: "root",
          cwd: "/workspace",
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "claude-code",
          title: "Root conversation",
          updatedAtUnixMs: 2,
          workspaceId: "workspace-1"
        },
        {
          activeTurn: {
            agentSessionId: "child",
            origin: "provider_initiated",
            phase: "waiting",
            startedAtUnixMs: 1,
            turnId: "child-turn",
            updatedAtUnixMs: 2
          },
          activeTurnId: "child-turn",
          agentSessionId: "child",
          cwd: "/workspace",
          kind: "child",
          latestTurnInteractions: [],
          parentAgentSessionId: "root",
          parentToolCallId: "tool-call-1",
          parentTurnId: "root-turn",
          pendingInteractions: [
            {
              agentSessionId: "child",
              createdAtUnixMs: 2,
              input: { questions: [{ id: "question-1" }] },
              kind: "question",
              requestId: "request-1",
              status: "pending",
              toolName: "AskUserQuestion",
              turnId: "child-turn",
              updatedAtUnixMs: 2
            }
          ],
          provider: "claude-code",
          rootAgentSessionId: "root",
          rootTurnId: "root-turn",
          title: "Child task",
          updatedAtUnixMs: 2,
          workspaceId: "workspace-1"
        }
      ]);
    });

    expect(result.current?.conversations).toEqual([
      expect.objectContaining({
        id: "root",
        needsUserAction: true,
        status: "waiting"
      })
    ]);
  });

  it("projects a plain @ title from a matching mention-rich initial prompt", () => {
    const engine = createTestRuntimeState("workspace-1");
    const query = {
      workspaceId: "workspace-1",
      userId: "user-1",
      provider: "codex" as const,
      sessionOrigin: "test"
    };
    const displayPrompt =
      "[@Task](mention://workspace-issue/issue-1?workspaceId=workspace-1) 看看";
    const { result } = renderHook(() =>
      useAgentGuiConversationList(engine, transcript, workspaceLoad, query)
    );

    act(() => {
      engine.report({
        type: "activation/requested",
        agentSessionId: "session-1",
        agentTargetId: "local:codex",
        clientSubmitId: "submit-1",
        content: [{ type: "text", text: displayPrompt }],
        cwd: "/workspace",
        expiresAtUnixMs: 100,
        initialDisplayPrompt: displayPrompt,
        mode: "new",
        optimisticTitle: "@Task 看看",
        railSectionKey: "project:/workspace",
        requestedAtUnixMs: 1,
        requestId: "activation-1",
        workspaceId: "workspace-1"
      });
    });

    expect(result.current?.conversations[0]).toEqual(
      expect.objectContaining({
        railSectionKey: "project:/workspace",
        title: "@Task 看看",
        titleLeadingMentionKind: null
      })
    );
  });

  it("shows only conversation text for a browser-element activation", () => {
    const engine = createTestRuntimeState("workspace-1");
    const query = {
      workspaceId: "workspace-1",
      userId: "user-1",
      provider: "codex" as const,
      sessionOrigin: "test"
    };
    const displayPrompt =
      "[@<div>](mention://browser-element/browser-element%3A1?tag=div&workspaceId=workspace-1) 这里说的什么";
    const { result } = renderHook(() =>
      useAgentGuiConversationList(engine, transcript, workspaceLoad, query)
    );

    act(() => {
      engine.report({
        type: "activation/requested",
        agentSessionId: "session-1",
        agentTargetId: "local:codex",
        clientSubmitId: "submit-1",
        content: [{ type: "text", text: displayPrompt }],
        cwd: "/workspace",
        expiresAtUnixMs: 100,
        initialDisplayPrompt: displayPrompt,
        mode: "new",
        optimisticTitle: "@<div> 这里说的什么",
        requestedAtUnixMs: 1,
        requestId: "activation-1",
        workspaceId: "workspace-1"
      });
    });

    expect(result.current?.conversations[0]).toEqual(
      expect.objectContaining({
        title: "这里说的什么",
        titleLeadingMentionKind: null,
        titleFallback: null
      })
    );
  });

  it("shows only conversation text for a canonical browser-element session", () => {
    const engine = createTestRuntimeState("workspace-1");
    const query = {
      workspaceId: "workspace-1",
      userId: "user-1",
      provider: "codex" as const,
      sessionOrigin: "test"
    };
    const displayPrompt =
      "[@<div>](mention://browser-element/browser-element%3A1?tag=div&workspaceId=workspace-1) 这里说的什么";
    const { result } = renderHook(() =>
      useAgentGuiConversationList(engine, transcript, workspaceLoad, query)
    );

    act(() => {
      engine.sessions.upsertSessions([
        {
          activeTurnId: null,
          agentSessionId: "session-1",
          createdAtUnixMs: 1,
          cwd: "/workspace",
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "codex",
          title: "@<div> 这里说的什么",
          updatedAtUnixMs: 1,
          workspaceId: "workspace-1"
        }
      ]);
      commitMessages([
        {
          agentSessionId: "session-1",
          kind: "text",
          messageId: "message-1",
          occurredAtUnixMs: 1,
          payload: {
            displayPrompt,
            text: displayPrompt
          },
          role: "user",
          status: null,
          turnId: "turn-1",
          version: 1,
          workspaceId: "workspace-1"
        }
      ]);
    });

    expect(result.current?.conversations[0]).toEqual(
      expect.objectContaining({
        title: "这里说的什么",
        titleLeadingMentionKind: null,
        titleFallback: null
      })
    );
  });

  it("shows a historical session marker after its first message is loaded", () => {
    const engine = createTestRuntimeState("workspace-1");
    const query = {
      workspaceId: "workspace-1",
      userId: "user-1",
      provider: "codex" as const,
      sessionOrigin: "test"
    };
    const displayPrompt =
      "[@Previous chat](mention://agent-session/session-previous?workspaceId=workspace-1) 看看";
    const { result } = renderHook(() =>
      useAgentGuiConversationList(engine, transcript, workspaceLoad, query)
    );

    act(() => {
      engine.sessions.upsertSessions([
        {
          activeTurnId: null,
          agentSessionId: "session-1",
          createdAtUnixMs: 1,
          cwd: "/workspace",
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "codex",
          title: "@Previous chat 看看",
          updatedAtUnixMs: 1,
          workspaceId: "workspace-1"
        }
      ]);
    });

    expect(result.current?.conversations[0]).toEqual(
      expect.objectContaining({
        title: "@Previous chat 看看",
        titleLeadingMentionKind: null
      })
    );

    act(() => {
      commitMessages([
        {
          agentSessionId: "session-1",
          kind: "text",
          messageId: "message-1",
          occurredAtUnixMs: 1,
          payload: { displayPrompt, text: displayPrompt },
          role: "user",
          status: null,
          turnId: "turn-1",
          version: 1,
          workspaceId: "workspace-1"
        }
      ]);
    });

    expect(result.current?.conversations[0]).toEqual(
      expect.objectContaining({
        title: "@Previous chat 看看",
        titleLeadingMentionKind: null
      })
    );
  });

  it("keeps the projected list stable while an assistant message streams", () => {
    const engine = createTestRuntimeState("workspace-1");
    const query = {
      workspaceId: "workspace-1",
      userId: "user-1",
      provider: "codex" as const,
      sessionOrigin: "test"
    };
    const { result } = renderHook(() =>
      useAgentGuiConversationList(engine, transcript, workspaceLoad, query)
    );

    act(() => {
      engine.sessions.upsertSessions([
        {
          activeTurnId: null,
          agentSessionId: "session-1",
          createdAtUnixMs: 1,
          cwd: "/workspace",
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "codex",
          title: "@Task inspect",
          updatedAtUnixMs: 1,
          workspaceId: "workspace-1"
        }
      ]);
      const displayPrompt =
        "[@Task](mention://workspace-issue/issue-1?workspaceId=workspace-1) inspect";
      commitMessages([
        {
          agentSessionId: "session-1",
          kind: "text",
          messageId: "user-1",
          occurredAtUnixMs: 1,
          payload: { displayPrompt, text: displayPrompt },
          role: "user",
          status: null,
          turnId: "turn-1",
          version: 1,
          workspaceId: "workspace-1"
        }
      ]);
    });
    const beforeStreaming = result.current;

    act(() => {
      commitMessages([
        {
          agentSessionId: "session-1",
          kind: "text",
          messageId: "assistant-1",
          occurredAtUnixMs: 2,
          payload: { text: "partial response" },
          role: "assistant",
          status: "streaming",
          turnId: "turn-1",
          version: 2,
          workspaceId: "workspace-1"
        }
      ]);
    });

    expect(result.current).toBe(beforeStreaming);
  });

  it("keeps the optimistic activation title until canonical title arrives", () => {
    const engine = createTestRuntimeState("workspace-1");
    const query = {
      workspaceId: "workspace-1",
      userId: "user-1",
      provider: "codex" as const,
      sessionOrigin: "test"
    };
    const { result } = renderHook(() =>
      useAgentGuiConversationList(engine, transcript, workspaceLoad, query)
    );

    act(() => {
      engine.report({
        type: "activation/requested",
        agentSessionId: "session-1",
        agentTargetId: "local:codex",
        clientSubmitId: "submit-1",
        content: [{ type: "text", text: "test1" }],
        cwd: "/workspace",
        expiresAtUnixMs: 100,
        mode: "new",
        requestedAtUnixMs: 1,
        requestId: "activation-1",
        optimisticTitle: "test1",
        workspaceId: "workspace-1"
      });
    });

    expect(result.current?.conversations).toEqual([
      expect.objectContaining({
        id: "session-1",
        provider: "codex",
        status: "working",
        title: "test1",
        titleFallback: null
      })
    ]);

    act(() => {
      engine.sessions.upsertSessions([
        {
          activeTurnId: null,
          agentSessionId: "session-1",
          createdAtUnixMs: 2,
          cwd: "/workspace",
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "codex",
          title: "test1",
          updatedAtUnixMs: 2,
          workspaceId: "workspace-1"
        }
      ]);
    });

    expect(result.current?.conversations).toEqual([
      expect.objectContaining({
        id: "session-1",
        status: "ready",
        title: "test1",
        titleFallback: null
      })
    ]);
  });

  it("projects canonical sessions and pending activation records without a list store", () => {
    const engine = createTestRuntimeState("workspace-1");
    const query = {
      workspaceId: "workspace-1",
      userId: "user-1",
      provider: "codex" as const,
      sessionOrigin: "test"
    };
    const { result } = renderHook(() =>
      useAgentGuiConversationList(engine, transcript, workspaceLoad, query)
    );

    act(() => {
      engine.report({
        type: "activation/requested",
        agentSessionId: "session-1",
        agentTargetId: "local:codex",
        clientSubmitId: "submit-1",
        cwd: "/workspace",
        expiresAtUnixMs: 100,
        mode: "new",
        requestedAtUnixMs: 1,
        requestId: "activation-1",
        title: "Pending task",
        workspaceId: "workspace-1"
      });
    });
    expect(result.current?.conversations).toEqual([
      expect.objectContaining({
        id: "session-1",
        status: "ready",
        title: "Pending task"
      })
    ]);

    act(() => {
      engine.sessions.upsertSessions([
        {
          ...{
            activeTurnId: null,
            latestTurnInteractions: [],
            pendingInteractions: []
          },
          agentSessionId: "session-1",
          createdAtUnixMs: 2,
          cwd: "/workspace",
          provider: "codex",
          title: "Durable task",
          updatedAtUnixMs: 2,
          workspaceId: "workspace-1"
        }
      ]);
    });
    expect(result.current?.conversations).toEqual([
      expect.objectContaining({
        id: "session-1",
        status: "ready",
        title: "Durable task"
      })
    ]);

    act(() => {
      engine.sessions.upsertSessions([
        {
          activeTurn: {
            agentSessionId: "session-1",
            origin: "user_prompt",
            phase: "running",
            startedAtUnixMs: 3,
            turnId: "turn-1",
            updatedAtUnixMs: 3
          },
          activeTurnId: "turn-1",
          agentSessionId: "session-1",
          createdAtUnixMs: 2,
          cwd: "/workspace",
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "codex",
          title: "Durable task",
          updatedAtUnixMs: 3,
          workspaceId: "workspace-1"
        }
      ]);
    });
    expect(result.current?.conversations).toEqual([
      expect.objectContaining({
        id: "session-1",
        status: "working",
        title: "Durable task"
      })
    ]);
  });

  it("keeps concurrent pending activations ordered and replaces only the canonicalized row", () => {
    const engine = createTestRuntimeState("workspace-1");
    const query = {
      conversationFilter: { kind: "all" as const },
      workspaceId: "workspace-1",
      userId: "user-1",
      provider: "codex" as const,
      sessionOrigin: "test"
    };
    const targets = [
      createLocalAgentGUIAgentTarget("codex"),
      createLocalAgentGUIAgentTarget("claude-code")
    ];
    const { result } = renderHook(() =>
      useAgentGuiConversationList(
        engine,
        transcript,
        workspaceLoad,
        query,
        targets
      )
    );

    act(() => {
      engine.report({
        type: "activation/requested",
        agentSessionId: "session-1",
        agentTargetId: "local:codex",
        clientSubmitId: "submit-1",
        cwd: "/workspace",
        expiresAtUnixMs: 100,
        mode: "new",
        requestedAtUnixMs: 10,
        requestId: "activation-1",
        title: "First",
        workspaceId: "workspace-1"
      });
      engine.report({
        type: "activation/requested",
        agentSessionId: "session-2",
        agentTargetId: "local:claude-code",
        clientSubmitId: "submit-2",
        cwd: "/workspace",
        expiresAtUnixMs: 100,
        mode: "new",
        requestedAtUnixMs: 20,
        requestId: "activation-2",
        title: "Second",
        workspaceId: "workspace-1"
      });
    });

    expect(
      result.current?.conversations.map((conversation) => ({
        id: conversation.id,
        provider: conversation.provider,
        source: conversation.projectionSource
      }))
    ).toEqual([
      {
        id: "session-2",
        provider: "claude-code",
        source: "pending_activation"
      },
      {
        id: "session-1",
        provider: "codex",
        source: "pending_activation"
      }
    ]);

    act(() => {
      engine.sessions.upsertSessions([
        {
          activeTurnId: null,
          agentSessionId: "session-1",
          createdAtUnixMs: 30,
          cwd: "/workspace",
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "codex",
          title: "First durable",
          updatedAtUnixMs: 30,
          workspaceId: "workspace-1"
        }
      ]);
    });

    expect(
      result.current?.conversations.map((conversation) => ({
        id: conversation.id,
        source: conversation.projectionSource,
        title: conversation.title
      }))
    ).toEqual([
      { id: "session-1", source: undefined, title: "First durable" },
      {
        id: "session-2",
        source: "pending_activation",
        title: "Second"
      }
    ]);
  });

  it("orders canonical sessions by latest turn start instead of update timestamps", () => {
    const engine = createTestRuntimeState("workspace-1");
    const query = {
      conversationFilter: { kind: "all" as const },
      workspaceId: "workspace-1",
      userId: "user-1",
      provider: "codex" as const,
      sessionOrigin: "test"
    };
    const { result } = renderHook(() =>
      useAgentGuiConversationList(engine, transcript, workspaceLoad, query)
    );

    act(() => {
      engine.sessions.upsertSessions([
        {
          activeTurnId: null,
          agentSessionId: "older-start-newer-update",
          createdAtUnixMs: 1_000,
          cwd: "/workspace",
          latestTurn: {
            agentSessionId: "older-start-newer-update",
            origin: "user_prompt",
            phase: "running",
            startedAtUnixMs: 2_000,
            turnId: "turn-older",
            updatedAtUnixMs: 9_000
          },
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "codex",
          title: "Older turn",
          updatedAtUnixMs: 9_000,
          workspaceId: "workspace-1"
        },
        {
          activeTurnId: null,
          agentSessionId: "newer-start-older-update",
          createdAtUnixMs: 500,
          cwd: "/workspace",
          latestTurn: {
            agentSessionId: "newer-start-older-update",
            origin: "user_prompt",
            phase: "running",
            startedAtUnixMs: 3_000,
            turnId: "turn-newer",
            updatedAtUnixMs: 4_000
          },
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "codex",
          title: "Newer turn",
          updatedAtUnixMs: 4_000,
          workspaceId: "workspace-1"
        }
      ]);
    });

    expect(
      result.current?.conversations.map((conversation) => ({
        id: conversation.id,
        sortTimeUnixMs: conversation.sortTimeUnixMs
      }))
    ).toEqual([
      { id: "newer-start-older-update", sortTimeUnixMs: 3_000 },
      { id: "older-start-newer-update", sortTimeUnixMs: 2_000 }
    ]);
  });
});
