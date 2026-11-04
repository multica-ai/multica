import { describe, expect, it } from "vitest";
import {
  normalizeAgentActivitySession,
  type AgentActivityMessage,
  type AgentActivitySnapshot,
  type AgentActivitySession
} from "@tutti-os/agent-activity-core";
import type { WorkspaceAgentActivityCard } from "../../workspaceAgentActivityListViewModel";
import { mergeWorkspaceAgentActivityDurableAndOverlayMessages } from "../../workspaceAgentMessageOverlay";
import {
  projectAgentActivitySessionToConversationVM,
  projectWorkspaceAgentMessagesToConversationVM,
  projectWorkspaceAgentMessagesToTimelineItems
} from "./workspaceAgentMessageProjection";

describe("projectWorkspaceAgentMessagesToConversationVM", () => {
  it("derives the selected Session and its messages from one activity snapshot", () => {
    const selectedSession = session();
    const selectedMessage = message({
      messageId: "snapshot-message",
      version: 1,
      role: "user",
      kind: "text",
      payload: { text: "Snapshot-owned message" }
    });
    const activitySnapshot: AgentActivitySnapshot = {
      workspaceId: "workspace-1",
      sessions: [selectedSession],
      presences: []
    };

    const conversation = projectAgentActivitySessionToConversationVM({
      activitySnapshot,
      agentSessionId: selectedSession.agentSessionId,
      messages: [selectedMessage]
    });
    const userRow = conversation?.rows.find(
      (row) => row.kind === "message" && row.speaker === "user"
    );

    expect(userRow?.kind === "message" ? userRow.messages[0]?.body : null).toBe(
      "Snapshot-owned message"
    );
    expect(
      projectAgentActivitySessionToConversationVM({
        activitySnapshot,
        agentSessionId: "missing-session"
      })
    ).toBeNull();
  });

  it("preserves message whitespace through the Mobile conversation projection", () => {
    const selectedSession = session();
    const selectedMessage = message({
      messageId: "streaming-whitespace",
      version: 1,
      role: "assistant",
      kind: "text",
      payload: { text: "\nHello  \n" }
    });
    const activitySnapshot: AgentActivitySnapshot = {
      workspaceId: "workspace-1",
      sessions: [selectedSession],
      presences: []
    };

    const conversation = projectAgentActivitySessionToConversationVM({
      activitySnapshot,
      agentSessionId: selectedSession.agentSessionId,
      messages: [selectedMessage]
    });
    const assistantRow = conversation?.rows.find(
      (row) => row.kind === "message" && row.speaker === "assistant"
    );

    expect(
      assistantRow?.kind === "message" ? assistantRow.messages[0]?.body : null
    ).toBe("\nHello  \n");
  });

  it("filters supplied Turns to the selected Session", () => {
    const selectedSession = session();
    const selectedMessage = message({
      messageId: "selected-message",
      payload: { text: "Selected Session message" },
      role: "user"
    });
    const activitySnapshot: AgentActivitySnapshot = {
      workspaceId: "workspace-1",
      sessions: [selectedSession],
      presences: []
    };

    const conversation = projectAgentActivitySessionToConversationVM({
      activitySnapshot,
      agentSessionId: selectedSession.agentSessionId,
      messages: [selectedMessage],
      sessionTurns: [
        {
          agentSessionId: "session-2",
          error: { message: "Foreign Turn failure" },
          origin: "user_prompt",
          outcome: "failed",
          phase: "settled",
          settledAtUnixMs: 10,
          startedAtUnixMs: 1,
          turnId: "foreign-turn",
          updatedAtUnixMs: 10
        }
      ]
    });

    expect(conversation?.sourceDetail.sessionTurns).toEqual([]);
    expect(
      conversation?.rows.flatMap((row) =>
        row.kind === "message"
          ? row.messages.map((message) => message.body)
          : []
      )
    ).not.toContain("Foreign Turn failure");
  });

  it("prefers canonical browser element prompt content over a lossy provider echo", () => {
    const prompt =
      "[@a](mention://browser-element/browser-element%3A1?path=%2Ftmp%2Fa.txt&tag=a&workspaceId=workspace-1) " +
      "[@div](mention://browser-element/browser-element%3A2?path=%2Ftmp%2Fdiv.txt&tag=div&workspaceId=workspace-1) " +
      "这里说的什么";
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "user-browser-elements",
          version: 1,
          role: "user",
          kind: "text",
          payload: {
            text: "@<a> @<div> 这里说的什么",
            content: [{ type: "text", text: prompt }]
          }
        })
      ]
    });

    const userRow = conversation.rows.find(
      (row) => row.kind === "message" && row.speaker === "user"
    );
    expect(userRow?.kind === "message" ? userRow.messages[0]?.body : null).toBe(
      prompt
    );
  });

  it("renders the browser element display prompt instead of staged snapshot files", () => {
    const displayPrompt =
      "[@a](mention://browser-element/browser-element%3A1?path=%2Ftmp%2Fa.txt&tag=a&workspaceId=workspace-1) " +
      "[@div](mention://browser-element/browser-element%3A2?path=%2Ftmp%2Fdiv.txt&tag=div&workspaceId=workspace-1) 22222222";
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "user-browser-element-files",
          version: 1,
          role: "user",
          kind: "text",
          payload: {
            displayPrompt,
            text: "pasted-text-1.txt",
            content: [
              {
                type: "file",
                kind: "pasted-text",
                name: "pasted-text-1.txt",
                path: "/tmp/a.txt"
              },
              {
                type: "file",
                kind: "pasted-text",
                name: "pasted-text-2.txt",
                path: "/tmp/div.txt"
              }
            ]
          }
        })
      ]
    });

    const userRow = conversation.rows.find(
      (row) => row.kind === "message" && row.speaker === "user"
    );
    expect(userRow?.kind === "message" ? userRow.messages[0]?.body : null).toBe(
      displayPrompt
    );
  });

  it("sorts message rows by stable message version instead of source-local ids", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "assistant-1",
          version: 2,
          role: "assistant",
          kind: "text",
          payload: { text: "Second by id" }
        }),
        message({
          messageId: "user-1",
          version: 1,
          role: "user",
          kind: "text",
          payload: { text: "First by id" }
        })
      ]
    });

    const rows = conversation.rows.filter((row) => row.kind === "message");
    expect(rows[0]?.kind === "message" ? rows[0].speaker : null).toBe("user");
    expect(rows[0]?.kind === "message" ? rows[0].messages[0]?.body : null).toBe(
      "First by id"
    );
    expect(rows[1]?.kind === "message" ? rows[1].speaker : null).toBe(
      "assistant"
    );
    expect(rows[1]?.kind === "message" ? rows[1].messages[0]?.body : null).toBe(
      "Second by id"
    );
  });

  it("keeps status panel order aligned with stream order when snapshot ids differ", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session({ effectiveStatus: "working" }),
      messages: [
        message({
          messageId: "assistant-intro",
          version: 1,
          role: "assistant",
          kind: "text",
          payload: { text: "I will inspect the repo." }
        }),
        message({
          messageId: "reasoning-1",
          version: 5,
          role: "assistant",
          kind: "reasoning",
          status: "completed",
          payload: { text: "Thinking after the active tool update." }
        }),
        message({
          messageId: "tool-1",
          version: 2,
          role: "assistant",
          kind: "tool_call",
          status: "completed",
          payload: { callId: "list-1", title: "List" }
        }),
        message({
          messageId: "tool-2",
          version: 3,
          role: "assistant",
          kind: "tool_call",
          status: "completed",
          payload: { callId: "read-1", title: "Read" }
        }),
        message({
          messageId: "tool-3",
          version: 4,
          role: "assistant",
          kind: "tool_call",
          status: "running",
          payload: { callId: "fetch-1", title: "读取网页" }
        })
      ]
    });

    expect(
      conversation.rows.map((row) => {
        if (row.kind === "message") {
          if (row.messages.length > 0) {
            return row.messages[0]?.body;
          }
          return row.thinking[0]?.body;
        }
        if (row.kind === "tool-group") {
          return row.calls.map((call) => call.name).join(",");
        }
        return row.kind;
      })
    ).toEqual([
      "I will inspect the repo.",
      "List,Read file,读取网页",
      "Thinking after the active tool update."
    ]);
  });

  it("orders late completed tool calls by start time before the final assistant answer", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      messages: [
        message({
          messageId: "user-1",
          version: 1,
          role: "user",
          kind: "text",
          payload: { text: "Inspect AI Canvas" },
          occurredAtUnixMs: 100,
          startedAtUnixMs: 100
        }),
        message({
          messageId: "toolcall:search-1",
          version: 4,
          role: "assistant",
          kind: "tool_call",
          status: "completed",
          payload: {
            callId: "search-1",
            title: "Bash",
            toolName: "Bash",
            input: { command: "rg 陶瓷家具与冲浪 /Users/Sun" }
          },
          startedAtUnixMs: 110,
          occurredAtUnixMs: 400,
          completedAtUnixMs: 400
        }),
        message({
          messageId: "assistant-final",
          version: 3,
          role: "assistant",
          kind: "text",
          payload: { text: "项目里有图片和视频。" },
          occurredAtUnixMs: 300,
          startedAtUnixMs: 300
        })
      ]
    });

    expect(
      conversation.rows.map((row) => {
        if (row.kind === "message") {
          return `${row.speaker}:${row.messages[0]?.body}`;
        }
        if (row.kind === "tool-group") {
          return `tool:${row.calls[0]?.toolName}`;
        }
        return row.kind;
      })
    ).toEqual([
      "user:Inspect AI Canvas",
      "tool:Bash",
      "assistant:项目里有图片和视频。"
    ]);
  });

  it("keeps a streaming assistant message before a later tool call by durable sequence", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      messages: [
        message({
          messageId: "assistant-intro",
          sequence: 2,
          version: 8,
          role: "assistant",
          kind: "text",
          status: "completed",
          payload: { text: "I will start the agents." },
          createdAtUnixMs: 100,
          occurredAtUnixMs: 201
        }),
        message({
          messageId: "tool-1",
          sequence: 3,
          version: 11,
          role: "assistant",
          kind: "tool_call",
          status: "completed",
          payload: {
            callId: "task-1",
            title: "Task",
            toolName: "Task"
          },
          createdAtUnixMs: 202,
          startedAtUnixMs: 200,
          occurredAtUnixMs: 300,
          completedAtUnixMs: 300
        })
      ]
    });

    expect(
      conversation.rows.map((row) => {
        if (row.kind === "message") {
          return `assistant:${row.messages[0]?.body}`;
        }
        if (row.kind === "tool-group") {
          return `tool:${row.calls[0]?.toolName}`;
        }
        return row.kind;
      })
    ).toEqual(["assistant:I will start the agents.", "tool:Agent"]);
  });

  it("keeps an optimistic prompt after durable messages with later timestamps", () => {
    const durableAssistant = message({
      messageId: "assistant-a",
      occurredAtUnixMs: 300,
      payload: { text: "Assistant A" },
      role: "assistant",
      sequence: 2,
      turnId: "turn-a",
      version: 2
    });
    const optimisticImagePrompt = message({
      messageId: "client-submit:user:submit-b",
      occurredAtUnixMs: 200,
      payload: {
        __agentGuiOptimisticPrompt: true,
        clientSubmitId: "submit-b",
        content: [
          {
            type: "image",
            data: "aW1hZ2UtYQ==",
            mimeType: "image/png",
            name: "image-a.png"
          }
        ],
        text: "[Image]"
      },
      role: "user",
      startedAtUnixMs: 200,
      turnId: "pending:submit-b",
      version: 0
    });
    const mergedMessages = mergeWorkspaceAgentActivityDurableAndOverlayMessages(
      {
        durableMessages: [durableAssistant],
        localMessages: [optimisticImagePrompt]
      }
    );

    expect(mergedMessages.map((item) => item.messageId)).toEqual([
      "assistant-a",
      "client-submit:user:submit-b"
    ]);
    expect(
      projectWorkspaceAgentMessagesToTimelineItems(mergedMessages).map(
        (item) => item.eventId
      )
    ).toEqual(["assistant-a", "client-submit:user:submit-b"]);
  });

  it("projects text, reasoning, errors, and unknown kinds conservatively", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session({ effectiveStatus: "failed", turnPhase: "failed" }),
      messages: [
        message({
          messageId: "user-1",
          role: "user",
          kind: "text",
          payload: { text: "Inspect this" }
        }),
        message({
          messageId: "thinking-1",
          role: "assistant",
          kind: "reasoning",
          payload: { text: "Need to inspect first." }
        }),
        message({
          messageId: "assistant-1",
          role: "assistant",
          kind: "text",
          payload: { text: "I found the issue." }
        }),
        message({
          messageId: "error-1",
          role: "assistant",
          kind: "error",
          status: "failed",
          payload: { title: "Agent failed", text: "Config invalid" }
        }),
        message({
          messageId: "unknown-1",
          role: "assistant",
          kind: "provider_notice",
          payload: { title: "Provider notice", text: "Notice text" }
        })
      ]
    });

    const messageRows = conversation.rows.filter(
      (
        row
      ): row is Extract<
        (typeof conversation.rows)[number],
        { kind: "message" }
      > => row.kind === "message"
    );
    expect(messageRows.map((row) => row.speaker)).toEqual([
      "user",
      "assistant",
      "assistant",
      "assistant"
    ]);
    expect(
      messageRows.flatMap((row) => row.thinking.map((item) => item.body))
    ).toContain("Need to inspect first.");
    expect(
      messageRows.flatMap((row) => row.messages.map((item) => item.body))
    ).toContain("Agent failed\n\nConfig invalid");
    expect(
      messageRows.flatMap((row) => row.messages.map((item) => item.body))
    ).toContain("Provider notice\n\nNotice text");
  });

  it("projects the plan Issue reverse link as a friendly assistant mention, not an error banner", () => {
    const mention =
      "[@修复登录问题](mention://workspace-issue/tutti-mode-plan-wf-1?topicId=topic-1&workspaceId=workspace-1)";
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session({
        effectiveStatus: "completed",
        turnPhase: "completed"
      }),
      messages: [
        message({
          messageId: "plan-issue:tutti-mode-plan-wf-1",
          version: 1,
          turnId: undefined,
          role: "assistant",
          kind: "session_audit",
          status: "completed",
          payload: { content: mention },
          occurredAtUnixMs: 100
        })
      ]
    });

    const assistantRow = conversation.rows.find(
      (row) => row.kind === "message" && row.speaker === "assistant"
    );
    const item =
      assistantRow?.kind === "message" ? assistantRow.messages[0] : null;
    expect(item?.contentKind).toBe("tutti-plan-issue-link");
    expect(item?.planIssueLink).toEqual({
      issueId: "tutti-mode-plan-wf-1",
      title: "修复登录问题",
      mentionMarkdown: mention
    });
    expect(item?.body).toBe(mention);
    expect(item?.systemNotice ?? null).toBeNull();
    expect(item?.visibleError ?? null).toBeNull();
  });

  it("degrades a malformed plan Issue link body to plain assistant markdown", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session({
        effectiveStatus: "completed",
        turnPhase: "completed"
      }),
      messages: [
        message({
          messageId: "plan-issue:tutti-mode-plan-wf-2",
          version: 1,
          turnId: undefined,
          role: "assistant",
          kind: "session_audit",
          status: "completed",
          payload: { content: "Issue created without a mention link" },
          occurredAtUnixMs: 100
        })
      ]
    });

    const assistantRow = conversation.rows.find(
      (row) => row.kind === "message" && row.speaker === "assistant"
    );
    const item =
      assistantRow?.kind === "message" ? assistantRow.messages[0] : null;
    expect(item?.body).toBe("Issue created without a mention link");
    expect(item?.contentKind).toBeUndefined();
    expect(item?.planIssueLink ?? null).toBeNull();
    expect(item?.systemNotice ?? null).toBeNull();
    expect(item?.visibleError ?? null).toBeNull();
  });

  it("keeps non plan-issue assistant session audits on the notice fallback", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session({
        effectiveStatus: "completed",
        turnPhase: "completed"
      }),
      messages: [
        message({
          messageId: "session-audit:other",
          version: 1,
          turnId: undefined,
          role: "assistant",
          kind: "session_audit",
          status: "completed",
          payload: { content: "Some other session annotation" },
          occurredAtUnixMs: 100
        })
      ]
    });

    const assistantRow = conversation.rows.find(
      (row) => row.kind === "message" && row.speaker === "assistant"
    );
    const item =
      assistantRow?.kind === "message" ? assistantRow.messages[0] : null;
    expect(item?.systemNotice).toEqual(
      expect.objectContaining({
        noticeKind: "session_audit",
        severity: "info"
      })
    );
  });

  it("preserves a turnless runtime failure audit as a visible error", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session({ effectiveStatus: "failed", turnPhase: "failed" }),
      messages: [
        message({
          messageId: "visible-error:session-failed-1",
          version: 1,
          turnId: undefined,
          role: "assistant",
          kind: "session_audit",
          status: "failed",
          payload: {
            kind: "agent_visible_error",
            severity: "error",
            phase: "start",
            code: "provider_stream_disconnected",
            provider: "claude-code",
            retryable: true,
            content: "Claude Code connection was interrupted.",
            text: "Claude Code connection was interrupted.",
            detail: "sidecar stream disconnected"
          },
          occurredAtUnixMs: 100
        })
      ]
    });

    const assistantRow = conversation.rows.find(
      (row) => row.kind === "message" && row.speaker === "assistant"
    );
    const item =
      assistantRow?.kind === "message" ? assistantRow.messages[0] : null;
    expect(item?.visibleError).toEqual({
      code: "provider_stream_disconnected",
      phase: "start",
      provider: "claude-code",
      detail: "sidecar stream disconnected",
      retryable: true
    });
    expect(item?.systemNotice ?? null).toBeNull();
  });

  it("keeps turnless session audits in chronological conversation order", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session({
        effectiveStatus: "completed",
        turnPhase: "completed"
      }),
      messages: [
        message({
          messageId: "goal-control:first",
          version: 1,
          turnId: undefined,
          role: "user",
          kind: "session_audit",
          payload: { text: "/goal first" },
          occurredAtUnixMs: 100
        }),
        message({
          messageId: "assistant-first",
          version: 2,
          turnId: "goal-turn-1",
          role: "assistant",
          kind: "text",
          payload: { text: "First goal result" },
          occurredAtUnixMs: 200
        }),
        message({
          messageId: "goal-control:second",
          version: 3,
          turnId: undefined,
          role: "user",
          kind: "session_audit",
          payload: { text: "/goal second" },
          occurredAtUnixMs: 300
        }),
        message({
          messageId: "assistant-second",
          version: 4,
          turnId: "goal-turn-2",
          role: "assistant",
          kind: "text",
          payload: { text: "Second goal result" },
          occurredAtUnixMs: 400
        })
      ]
    });

    expect(
      conversation.rows
        .filter((row) => row.kind === "message")
        .map((row) => `${row.speaker}:${row.messages[0]?.body}`)
    ).toEqual([
      "user:/goal first",
      "assistant:First goal result",
      "user:/goal second",
      "assistant:Second goal result"
    ]);
  });

  it("projects only the latest text snapshot for a stable message id", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "assistant-stream-1",
          version: 1,
          role: "assistant",
          kind: "text",
          payload: { text: "I'll " }
        }),
        message({
          messageId: "assistant-stream-1",
          id: 1,
          version: 2,
          role: "assistant",
          kind: "text",
          payload: { text: "I'll check " }
        }),
        message({
          messageId: "assistant-stream-1",
          id: 1,
          version: 3,
          role: "assistant",
          kind: "text",
          payload: { text: "I'll check the repo." }
        }),
        message({
          messageId: "assistant-stream-2",
          id: 2,
          version: 4,
          role: "assistant",
          kind: "text",
          payload: { text: "Distinct message." }
        })
      ]
    });

    const bodies = conversation.rows
      .filter(
        (
          row
        ): row is Extract<
          (typeof conversation.rows)[number],
          { kind: "message" }
        > => row.kind === "message"
      )
      .flatMap((row) => row.messages.map((item) => item.body));
    expect(bodies).toEqual(["I'll check the repo.\nDistinct message."]);
  });

  it("projects only the latest reasoning snapshot for a stable message id", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "reasoning-stream-1",
          id: 1,
          version: 1,
          role: "assistant",
          kind: "reasoning",
          payload: { text: "Looking " }
        }),
        message({
          messageId: "reasoning-stream-1",
          id: 1,
          version: 2,
          role: "assistant",
          kind: "reasoning",
          payload: { text: "Looking through files." }
        })
      ]
    });

    const thinkingBodies = conversation.rows
      .filter(
        (
          row
        ): row is Extract<
          (typeof conversation.rows)[number],
          { kind: "message" }
        > => row.kind === "message"
      )
      .flatMap((row) => row.thinking.map((item) => item.body));
    expect(thinkingBodies).toEqual(["Looking through files."]);
  });

  it("projects matching tool_call updates into one stable action row", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      messages: [
        message({
          messageId: "tool-1",
          id: 1,
          version: 1,
          role: "assistant",
          kind: "tool_call",
          status: "running",
          payload: {
            callId: "read-1",
            title: "Read",
            toolName: "Read",
            input: { path: "/workspace/demo/README.md" }
          }
        }),
        message({
          messageId: "tool-1",
          id: 1,
          version: 2,
          role: "assistant",
          kind: "tool_call",
          status: "completed",
          payload: {
            callId: "read-1",
            title: "Read",
            output: { text: "README contents" }
          }
        })
      ]
    });

    const toolRows = conversation.rows.filter(
      (row) => row.kind === "tool-group"
    );
    expect(toolRows).toHaveLength(1);
    const call = toolRows[0]?.calls[0];
    expect(call?.id).toBe("call:read-1");
    expect(call?.statusKind).toBe("completed");
    expect(call?.input).toEqual({ path: "/workspace/demo/README.md" });
    expect(call?.output).toEqual({ text: "README contents" });
  });

  it("projects durable AskUserQuestion tool_call messages as pending interactive prompts", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session({
        effectiveStatus: "working",
        turnPhase: "working"
      }),
      messages: [
        message({
          messageId: "toolcall:call-ask",
          id: 1,
          version: 1,
          role: "assistant",
          kind: "tool_call",
          status: "running",
          payload: {
            callId: "call-ask",
            callType: "interactive",
            title: "AskUserQuestion",
            toolName: "AskUserQuestion",
            status: "streaming",
            input: {
              requestId: "request-ask",
              toolName: "AskUserQuestion",
              questions: [
                {
                  id: "favorite-color",
                  header: "Color",
                  question: "What's your favorite color?",
                  options: [
                    {
                      label: "Green",
                      description: "Pick green"
                    }
                  ]
                }
              ]
            },
            metadata: {
              adapter: "claude-agent-sdk",
              callType: "interactive",
              interactiveKind: "ask-user",
              toolName: "AskUserQuestion"
            }
          }
        })
      ]
    });

    expect("pendingInteractivePrompt" in conversation).toBe(false);
  });

  it("does not use opaque call ids as tool names", () => {
    const opaqueCallId = "call_SMAI3q45S9s5TwOqO8R7ZMdU";
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      messages: [
        message({
          messageId: `toolcall:${opaqueCallId}`,
          id: 1,
          role: "assistant",
          kind: "tool_call",
          status: "completed",
          payload: {
            callId: opaqueCallId,
            title: opaqueCallId,
            callType: "tool",
            toolName: "Bash",
            input: { command: "pwd" }
          }
        })
      ]
    });

    const toolRows = conversation.rows.filter(
      (row) => row.kind === "tool-group"
    );
    const call = toolRows[0]?.calls[0];
    expect(call?.toolName).toBe("Bash");
    expect(call?.name).not.toContain("SMAI3q45");
  });

  it("normalizes subagent tool variants into agent tool names", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      messages: [
        message({
          messageId: "toolcall:delegate-1",
          id: 1,
          role: "assistant",
          kind: "tool_call",
          status: "completed",
          payload: {
            callId: "delegate-1",
            title: "delegate_task",
            callType: "tool",
            toolName: "Task",
            input: { task: "review this change" }
          }
        })
      ]
    });

    const toolRows = conversation.rows.filter(
      (row) => row.kind === "tool-group"
    );
    expect(toolRows[0]?.calls[0]?.toolName).toBe("Agent");
  });

  it("drops source-less skills warnings but preserves explicit non-runtime matches", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "user-1",
          id: 1,
          version: 1,
          role: "user",
          kind: "text",
          payload: { text: "你好" }
        }),
        message({
          messageId: "notice-1",
          id: 2,
          version: 2,
          role: "assistant",
          kind: "text",
          payload: {
            kind: "agent_system_notice",
            noticeKind: "warning",
            severity: "warning",
            title: "Codex warning",
            detail:
              "Skill descriptions were shortened to fit the skills context budget. Codex can still see every skill, but some descriptions are shorter.",
            text: "Codex warning",
            content: "Codex warning",
            contentMode: "snapshot"
          }
        }),
        message({
          messageId: "notice-2",
          id: 3,
          version: 3,
          role: "assistant",
          kind: "text",
          payload: {
            kind: "agent_system_notice",
            noticeKind: "warning",
            severity: "warning",
            title: "Codex warning",
            source: "user",
            detail:
              "Skill descriptions were shortened to fit the skills context budget.",
            text: "Codex warning",
            content: "Codex warning",
            contentMode: "snapshot"
          }
        }),
        message({
          messageId: "assistant-1",
          id: 4,
          version: 4,
          role: "assistant",
          kind: "text",
          payload: {
            text: "你好。有什么需要我在这个 workspace 里处理?"
          }
        })
      ]
    });

    const assistantRows = conversation.rows.filter(
      (
        row
      ): row is Extract<
        (typeof conversation.rows)[number],
        { kind: "message" }
      > => row.kind === "message" && row.speaker === "assistant"
    );

    expect(assistantRows).toHaveLength(2);
    expect(assistantRows[0]?.messages[0]?.systemNotice).toEqual({
      noticeKind: "warning",
      severity: "warning",
      title: "Codex warning",
      source: "user",
      detail:
        "Skill descriptions were shortened to fit the skills context budget.",
      retryable: null
    });
    expect(assistantRows[1]?.messages[0]?.systemNotice).toBeNull();
    expect(assistantRows[1]?.messages[0]?.body).toBe(
      "你好。有什么需要我在这个 workspace 里处理?"
    );
  });

  it("keeps fallback metadata warnings when runtime metadata is absent", () => {
    const metadataWarning =
      "Model metadata for `minimax/minimax-m2.5` not found. Defaulting to fallback metadata; this can degrade performance and cause issues.";
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "notice-1",
          id: 1,
          version: 1,
          role: "assistant",
          kind: "text",
          payload: {
            kind: "agent_system_notice",
            noticeKind: "warning",
            severity: "warning",
            title: metadataWarning,
            detail: metadataWarning,
            text: "Codex warning",
            content: "Codex warning",
            contentMode: "snapshot"
          }
        })
      ]
    });

    const warning = conversation.rows.find(
      (row) => row.kind === "message" && row.speaker === "assistant"
    );
    expect(warning).toMatchObject({
      kind: "message",
      messages: [
        {
          systemNotice: expect.objectContaining({
            noticeKind: "warning",
            detail: metadataWarning
          })
        }
      ]
    });
  });

  it("renders an image-only optimistic prompt without synthetic text", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "client-submit:user:submit-image-1",
          id: 0,
          role: "user",
          kind: "text",
          payload: {
            __agentGuiOptimisticPrompt: true,
            clientSubmitId: "submit-image-1",
            text: "",
            content: [
              {
                type: "image",
                mimeType: "image/png",
                path: "/prompt-assets/screen.png",
                name: "screen.png"
              }
            ]
          }
        })
      ]
    });

    const userRow = conversation.rows.find(
      (row) => row.kind === "message" && row.speaker === "user"
    );

    expect(userRow?.kind === "message" ? userRow.messages : null).toEqual([
      expect.objectContaining({
        body: "",
        contentKind: "image-grid",
        images: [
          expect.objectContaining({
            id: "client-submit:user:submit-image-1:image:0",
            mimeType: "image/png",
            name: "screen.png",
            path: "/prompt-assets/screen.png"
          })
        ]
      })
    ]);

    const durableConversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "client-submit:user:submit-image-1",
          id: 1,
          version: 1,
          role: "user",
          kind: "text",
          payload: {
            clientSubmitId: "submit-image-1",
            text: "[Image]",
            content: [
              {
                type: "image",
                mimeType: "image/png",
                attachmentId: "attachment-1",
                name: "screen.png"
              }
            ]
          }
        })
      ]
    });
    const durableUserRow = durableConversation.rows.find(
      (row) => row.kind === "message" && row.speaker === "user"
    );

    expect(
      durableUserRow?.kind === "message"
        ? durableUserRow.messages[0]?.images?.[0]
        : null
    ).toMatchObject({
      id: "client-submit:user:submit-image-1:image:0",
      attachmentId: "attachment-1"
    });
  });

  it("projects legacy compact text as a semantic notice and removes only its matching echo", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "compact-1",
          version: 1,
          status: "failed",
          payload: {
            source: "compact",
            text: "Compacting failed: Not enough messages to compact."
          }
        }),
        message({
          messageId: "assistant-echo",
          version: 2,
          payload: { text: "Not enough messages to compact." }
        }),
        message({
          messageId: "assistant-guidance",
          version: 3,
          payload: { text: "Add more messages and try again." }
        })
      ]
    });

    const assistantRows = conversation.rows.filter(
      (
        row
      ): row is Extract<
        (typeof conversation.rows)[number],
        { kind: "message" }
      > => row.kind === "message" && row.speaker === "assistant"
    );

    expect(assistantRows).toHaveLength(2);
    expect(assistantRows[0]?.messages[0]?.systemNotice).toEqual({
      noticeKind: "system_notice",
      severity: null,
      source: "compact",
      command: "compact",
      commandStatus: "failed",
      title: "Context compaction interrupted.",
      detail: "Not enough messages to compact.",
      retryable: null
    });
    expect(assistantRows[0]?.messages[0]?.presentationKind).toBe(
      "turn-boundary"
    );
    expect(assistantRows[1]?.messages[0]?.body).toBe(
      "Add more messages and try again."
    );
  });

  it("preserves matching assistant guidance after canceled compaction", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "compact-canceled",
          version: 1,
          status: "canceled",
          semantics: {
            noticeCommand: "compact",
            noticeCommandStatus: "canceled"
          },
          payload: {
            kind: "agent_system_notice",
            noticeKind: "system_notice",
            title: "Context compaction interrupted.",
            detail: "Try again after adding more context.",
            text: "Context compaction interrupted."
          }
        }),
        message({
          messageId: "assistant-guidance",
          version: 2,
          payload: { text: "Try again after adding more context." }
        })
      ]
    });

    const assistantRows = conversation.rows.filter(
      (
        row
      ): row is Extract<
        (typeof conversation.rows)[number],
        { kind: "message" }
      > => row.kind === "message" && row.speaker === "assistant"
    );

    expect(assistantRows).toHaveLength(2);
    expect(assistantRows[0]?.messages[0]?.systemNotice).toEqual(
      expect.objectContaining({
        command: "compact",
        commandStatus: "canceled"
      })
    );
    expect(assistantRows[1]?.messages[0]?.body).toBe(
      "Try again after adding more context."
    );
  });

  it("normalizes persisted Codex compaction notices before presentation", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "compaction:turn-compact",
          turnId: "turn-compact",
          version: 1,
          status: "completed",
          payload: {
            kind: "agent_system_notice",
            noticeKind: "system_notice",
            source: "runtime",
            title: "Context compacted.",
            text: "Context compacted."
          }
        })
      ]
    });

    const compactMessage = conversation.rows.flatMap((row) =>
      row.kind === "message" ? row.messages : []
    )[0];
    expect(compactMessage?.systemNotice).toEqual(
      expect.objectContaining({
        command: "compact",
        commandStatus: "completed"
      })
    );
    expect(compactMessage?.presentationKind).toBe("turn-boundary");
  });

  it("projects context overflow as a typed handoff-required notice", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "compact-handoff-required",
          turnId: "turn-compact",
          status: "failed",
          semantics: {
            noticeCommand: "compact",
            noticeCommandStatus: "failed"
          },
          payload: {
            kind: "agent_system_notice",
            noticeKind: "context_handoff_required",
            severity: "error",
            title: "Context compaction interrupted.",
            detail: "Maximum context length exceeded."
          }
        })
      ]
    });

    const projected = conversation.rows.flatMap((row) =>
      row.kind === "message" ? row.messages : []
    )[0];
    expect(projected?.systemNotice).toEqual(
      expect.objectContaining({
        semanticKind: "context-handoff-required",
        severity: "error"
      })
    );
  });

  it("prefers canonical message semantics over duplicated notice payload fields", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session({ effectiveStatus: "working" }),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "compact-running",
          version: 1,
          status: "working",
          semantics: {
            noticeCommand: "compact",
            noticeCommandStatus: "running"
          },
          payload: {
            kind: "agent_system_notice",
            noticeKind: "system_notice",
            noticeCommand: "review",
            noticeCommandStatus: "completed",
            title: "Compacting context.",
            text: "Compacting context."
          }
        })
      ]
    });

    const compactMessage = conversation.rows.flatMap((row) =>
      row.kind === "message" ? row.messages : []
    )[0];
    expect(compactMessage?.systemNotice).toEqual(
      expect.objectContaining({
        command: "compact",
        commandStatus: "running"
      })
    );
    expect(compactMessage?.presentationKind).toBe("specific-progress");
    expect(conversation.rows.some((row) => row.kind === "processing")).toBe(
      false
    );
  });

  it("renders displayPrompt instead of rich content text while preserving prompt images", () => {
    const conversation = projectWorkspaceAgentMessagesToConversationVM({
      activity: activity(),
      session: session(),
      workspaceRoot: "/workspace/demo",
      messages: [
        message({
          messageId: "user-1",
          id: 1,
          role: "user",
          kind: "text",
          payload: {
            displayPrompt: "Run Automation",
            text: "Run Automation",
            content: [
              { type: "text", text: "long automation prompt" },
              {
                type: "image",
                mimeType: "image/png",
                attachmentId: "attachment-1",
                name: "screen.png"
              }
            ]
          }
        })
      ]
    });

    const userRow = conversation.rows.find(
      (row) => row.kind === "message" && row.speaker === "user"
    );

    expect(
      userRow?.kind === "message" ? userRow.messages[0]?.contentKind : null
    ).toBe("image-grid");
    expect(
      userRow?.kind === "message" ? userRow.messages[0]?.images?.[0] : null
    ).toMatchObject({
      attachmentId: "attachment-1",
      mimeType: "image/png"
    });
    expect(userRow?.kind === "message" ? userRow.messages[1]?.body : null).toBe(
      "Run Automation"
    );
  });
});

function activity(
  overrides: Partial<WorkspaceAgentActivityCard> = {}
): WorkspaceAgentActivityCard {
  return {
    id: "activity-1",
    sessionId: "session-1",
    agentName: "Codex",
    agentProvider: "codex",
    status: "working",
    title: "Codex",
    latestActivitySummary: "Working",
    sortTimeUnixMs: 10,
    changedFiles: [],
    userId: "user-1",
    userName: "Taylor",
    userAvatarUrl: "",
    ...overrides
  };
}

function session(
  overrides: Partial<AgentActivitySession> & {
    effectiveStatus?: string;
    turnPhase?: string;
  } = {}
): AgentActivitySession {
  const { effectiveStatus, turnPhase, ...canonical } = overrides;
  const phase = turnPhase ?? effectiveStatus;
  const hasActiveTurn =
    phase === "submitted" ||
    phase === "running" ||
    phase === "working" ||
    phase === "waiting" ||
    phase === "settling";
  return normalizeAgentActivitySession({
    ...{
      activeTurnId: null,
      latestTurnInteractions: [],
      pendingInteractions: []
    },
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    userId: "user-1",
    provider: "codex",
    providerSessionId: "provider-session-1",
    cwd: "/workspace/demo",
    title: "Codex",
    createdAtUnixMs: 1,
    updatedAtUnixMs: 10,
    ...(hasActiveTurn
      ? {
          activeTurn: {
            agentSessionId: "session-1",
            outcome: null,
            origin: "user_prompt",
            phase: phase === "working" ? "running" : phase,
            settledAtUnixMs: null,
            startedAtUnixMs: 1,
            turnId: "turn-1",
            updatedAtUnixMs: 10
          },
          activeTurnId: "turn-1"
        }
      : {}),
    ...canonical
  });
}

it("hides tutti-plan issue dispatch delegate cards but keeps other collaborations", () => {
  const items = projectWorkspaceAgentMessagesToTimelineItems([
    message({
      messageId: "collab:cr-plan",
      version: 1,
      kind: "collaboration",
      turnId: "",
      payload: {
        mode: "delegate",
        triggerReason: "issue:tutti-mode-plan-wf-1/task:t1/run:r1"
      }
    }),
    message({
      messageId: "collab:cr-manual",
      version: 2,
      kind: "collaboration",
      turnId: "",
      payload: { mode: "consult", triggerReason: "user" }
    })
  ]);

  const collaborationItems = items.filter(
    (item) => item.itemType === "collaboration"
  );
  expect(collaborationItems).toHaveLength(1);
  expect(collaborationItems[0]?.eventId).toBe("collab:cr-manual");
});

function message(
  overrides: Partial<AgentActivityMessage> & { id?: number }
): AgentActivityMessage {
  const { id: _legacyStorageId, ...canonical } = overrides;
  const version = overrides.version ?? 1;
  return {
    agentSessionId: "session-1",
    messageId: "message-1",
    version,
    turnId: "turn-1",
    role: "assistant",
    kind: "text",
    payload: {},
    occurredAtUnixMs: 1,
    ...canonical
  };
}
