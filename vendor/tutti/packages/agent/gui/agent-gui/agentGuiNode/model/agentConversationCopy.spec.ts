import type { AgentActivityMessage } from "@tutti-os/agent-activity-core";
import { describe, expect, it, vi } from "vitest";
import {
  AGENT_CONVERSATION_COPY_MAX_EMBEDDED_IMAGE_BYTES,
  loadCompleteAgentConversationMessages,
  serializeAgentConversationForClipboard
} from "./agentConversationCopy";

const labels = {
  file: "File",
  image: "Image",
  mentionPrefix: "@",
  previousMessages: "{{count}} previous messages"
};

describe("loadCompleteAgentConversationMessages", () => {
  it("reads complete native history and returns canonical presentation order", async () => {
    const newest = message({
      messageId: "assistant-2",
      occurredAtUnixMs: 30,
      sequence: 3,
      version: 30
    });
    const middle = message({
      messageId: "user-2",
      occurredAtUnixMs: 20,
      sequence: 2,
      version: 20
    });
    const oldest = message({
      messageId: "user-1",
      occurredAtUnixMs: 10,
      sequence: 1,
      version: 10
    });
    const loadAll = vi.fn().mockResolvedValue([newest, middle, oldest]);

    const result = await loadCompleteAgentConversationMessages({
      agentSessionId: "session-1",
      runtime: { conversations: { loadAll } } as Parameters<
        typeof loadCompleteAgentConversationMessages
      >[0]["runtime"],
      workspaceId: "workspace-1"
    });

    expect(result.map((entry) => entry.messageId)).toEqual([
      "user-1",
      "user-2",
      "assistant-2"
    ]);
    expect(loadAll).toHaveBeenCalledExactlyOnceWith({
      workspaceId: "workspace-1",
      agentSessionId: "session-1"
    });
  });
});

describe("serializeAgentConversationForClipboard", () => {
  it("emits a lean transcript: full user input, interim narration collapsed, final reply plain", async () => {
    const messages = [
      message({
        messageId: "user-1",
        role: "user",
        sequence: 1,
        payload: {
          content: [
            { type: "text", text: "Please review this" },
            {
              type: "image",
              data: "QUFB",
              mimeType: "image/png",
              name: "shot.png"
            }
          ]
        }
      }),
      message({
        messageId: "assistant-1",
        role: "assistant",
        sequence: 2,
        payload: { content: "I will inspect the file first." }
      }),
      message({
        kind: "tool_call",
        messageId: "tool-1",
        role: "assistant",
        sequence: 3,
        payload: {
          toolName: "ImageGeneration",
          status: "completed",
          arguments: { prompt: "generate a diagram" }
        }
      }),
      message({
        messageId: "thinking-1",
        role: "assistant_thinking",
        sequence: 4,
        payload: { content: "hmm" }
      }),
      message({
        messageId: "assistant-2",
        role: "assistant",
        sequence: 5,
        payload: { content: "Found the issue." }
      }),
      message({
        messageId: "assistant-3",
        role: "assistant",
        sequence: 6,
        payload: { content: "Here is the complete answer." }
      })
    ];

    const transcript = await serializeAgentConversationForClipboard({
      labels,
      messages,
      title: "Design review"
    });

    // The plain side stays lean: an inline-data image degrades to its bold
    // label instead of carrying base64.
    expect(transcript.markdown).toBe(
      [
        "# Design review",
        "",
        "> Please review this",
        ">",
        "> **shot.png**",
        "",
        "<details><summary>2 previous messages</summary>",
        "",
        "> I will inspect the file first.",
        "",
        "> Found the issue.",
        "",
        "</details>",
        "",
        "Here is the complete answer."
      ].join("\n")
    );
    // The hydrated side is the same transcript with the image embedded.
    expect(transcript.hydratedMarkdown).toBe(
      [
        "# Design review",
        "",
        "> Please review this",
        ">",
        "> ![shot.png](<data:image/png;base64,QUFB>)",
        "",
        "<details><summary>2 previous messages</summary>",
        "",
        "> I will inspect the file first.",
        "",
        "> Found the issue.",
        "",
        "</details>",
        "",
        "Here is the complete answer."
      ].join("\n")
    );
    expect(transcript.omittedImages).toBe(0);
    expect(transcript.markdown).not.toContain("```json");
    expect(transcript.markdown).not.toContain("ImageGeneration");
    expect(transcript.markdown).not.toContain("hmm");
    expect(transcript.markdown).not.toContain("base64");
  });

  it("renders a single assistant reply plain without a details block", async () => {
    const messages = [
      message({
        messageId: "user-1",
        role: "user",
        sequence: 1,
        payload: { content: "What changed?" }
      }),
      message({
        messageId: "assistant-1",
        role: "assistant",
        sequence: 2,
        payload: { content: "Here is the complete answer." }
      })
    ];

    const transcript = await serializeAgentConversationForClipboard({
      labels,
      messages,
      title: "Session"
    });

    expect(transcript.markdown).toBe(
      "# Session\n\n> What changed?\n\nHere is the complete answer."
    );
    expect(transcript.hydratedMarkdown).toBe(transcript.markdown);
    expect(transcript.markdown).not.toContain("<details>");
  });

  it("keeps generated tool images and drops runtime system notices", async () => {
    const messages = [
      message({
        messageId: "user-1",
        role: "user",
        sequence: 1,
        payload: { content: "帮我生成一个咖喱鱼蛋的图" }
      }),
      message({
        messageId: "notice-1",
        role: "assistant",
        sequence: 2,
        payload: {
          kind: "agent_system_notice",
          noticeKind: "warning",
          severity: "warn",
          detail:
            "Skill descriptions were shortened to fit the 2% skills context budget"
        }
      }),
      message({
        messageId: "assistant-1",
        role: "assistant",
        sequence: 3,
        payload: {
          content:
            "我会用图像生成技能，直接创作一张热气腾腾、诱人的港式咖喱鱼蛋美食图。"
        }
      }),
      message({
        kind: "tool_call",
        messageId: "tool-1",
        role: "assistant",
        sequence: 4,
        payload: {
          toolName: "ImageGeneration",
          status: "completed",
          content: [
            {
              type: "content",
              content: {
                type: "image",
                uri: "/workspace/output.png",
                mimeType: "image/png"
              }
            }
          ]
        }
      })
    ];

    const transcript = await serializeAgentConversationForClipboard({
      labels,
      messages,
      title: "咖喱鱼蛋"
    });

    expect(transcript.markdown).toBe(
      [
        "# 咖喱鱼蛋",
        "",
        "> 帮我生成一个咖喱鱼蛋的图",
        "",
        "我会用图像生成技能，直接创作一张热气腾腾、诱人的港式咖喱鱼蛋美食图。",
        "",
        "![Image](</workspace/output.png>)"
      ].join("\n")
    );
    // Without a local-image reader the hydrated variant keeps the path link.
    expect(transcript.hydratedMarkdown).toBe(transcript.markdown);
    expect(transcript.omittedImages).toBe(0);
    expect(transcript.markdown).not.toContain("Skill descriptions");
    expect(transcript.markdown).not.toContain("<details>");
  });

  it("collapses interim narration but keeps tool images plain in order", async () => {
    const messages = [
      message({
        messageId: "user-1",
        role: "user",
        sequence: 1,
        payload: { content: "Generate an image" }
      }),
      message({
        messageId: "assistant-1",
        role: "assistant",
        sequence: 2,
        payload: { content: "Working on it." }
      }),
      message({
        kind: "tool_call",
        messageId: "tool-1",
        role: "assistant",
        sequence: 3,
        payload: {
          toolName: "ImageGeneration",
          status: "completed",
          content: [
            {
              type: "content",
              content: {
                type: "image",
                uri: "/workspace/result.png",
                mimeType: "image/png"
              }
            }
          ]
        }
      }),
      message({
        messageId: "assistant-2",
        role: "assistant",
        sequence: 4,
        payload: { content: "Here is your image." }
      })
    ];

    const transcript = await serializeAgentConversationForClipboard({
      labels,
      messages,
      title: "Session"
    });

    expect(transcript.markdown).toBe(
      [
        "# Session",
        "",
        "> Generate an image",
        "",
        "<details><summary>1 previous messages</summary>",
        "",
        "> Working on it.",
        "",
        "</details>",
        "",
        "![Image](</workspace/result.png>)",
        "",
        "Here is your image."
      ].join("\n")
    );
  });

  it("hydrates tool-message images through readAttachment", async () => {
    const readAttachment = vi.fn().mockResolvedValue({
      data: "aW1n",
      mimeType: "image/png"
    });
    const messages = [
      message({
        kind: "tool_call",
        messageId: "tool-1",
        role: "assistant",
        sequence: 1,
        payload: {
          toolName: "ImageGeneration",
          status: "completed",
          output: [
            {
              type: "image",
              attachmentId: "attachment-9",
              mimeType: "image/png"
            }
          ]
        }
      })
    ];

    const transcript = await serializeAgentConversationForClipboard({
      labels,
      messages,
      readAttachment,
      title: "Attachments"
    });

    expect(readAttachment).toHaveBeenCalledWith("attachment-9");
    expect(transcript.hydratedMarkdown).toContain(
      "![Image](<data:image/png;base64,aW1n>)"
    );
    expect(transcript.markdown).toContain(
      "![Image](<attachment:attachment-9>)"
    );
    expect(transcript.markdown).not.toContain("data:");
    expect(transcript.omittedImages).toBe(0);
  });

  it("keeps the attachment reference when the image exceeds the embed cap", async () => {
    // Smallest unpadded base64 payload whose binary size exceeds the cap.
    const oversized = "A".repeat(
      4 * Math.ceil((AGENT_CONVERSATION_COPY_MAX_EMBEDDED_IMAGE_BYTES + 1) / 3)
    );
    const readAttachment = vi.fn().mockResolvedValue({
      data: oversized,
      mimeType: "image/png"
    });
    const messages = [
      message({
        kind: "tool_call",
        messageId: "tool-1",
        role: "assistant",
        sequence: 1,
        payload: {
          toolName: "ImageGeneration",
          status: "completed",
          output: [
            {
              type: "image",
              attachmentId: "attachment-9",
              mimeType: "image/png"
            }
          ]
        }
      })
    ];

    const transcript = await serializeAgentConversationForClipboard({
      labels,
      messages,
      readAttachment,
      title: "Attachments"
    });

    expect(transcript.hydratedMarkdown).toContain(
      "![Image](<attachment:attachment-9>)"
    );
    expect(transcript.hydratedMarkdown).not.toContain("data:");
    expect(transcript.omittedImages).toBe(1);
  });

  it("hydrates local-path tool images through readLocalImage on the hydrated side only", async () => {
    const readLocalImage = vi.fn().mockResolvedValue({
      data: "QkJC",
      mimeType: "image/png"
    });
    const messages = [
      message({
        kind: "tool_call",
        messageId: "tool-1",
        role: "assistant",
        sequence: 1,
        payload: {
          toolName: "ImageGeneration",
          status: "completed",
          content: [
            {
              type: "content",
              content: {
                type: "image",
                uri: "/workspace/output.png",
                mimeType: "image/png"
              }
            }
          ]
        }
      })
    ];

    const transcript = await serializeAgentConversationForClipboard({
      labels,
      messages,
      readLocalImage,
      title: "Local image"
    });

    expect(readLocalImage).toHaveBeenCalledWith({
      mimeType: "image/png",
      path: "/workspace/output.png"
    });
    expect(transcript.hydratedMarkdown).toContain(
      "![Image](<data:image/png;base64,QkJC>)"
    );
    expect(transcript.markdown).toContain("![Image](</workspace/output.png>)");
    expect(transcript.markdown).not.toContain("data:");
    expect(transcript.omittedImages).toBe(0);
  });

  it("keeps the path reference on both sides when the local read fails", async () => {
    const readLocalImage = vi.fn().mockRejectedValue(new Error("read denied"));
    const messages = [
      message({
        kind: "tool_call",
        messageId: "tool-1",
        role: "assistant",
        sequence: 1,
        payload: {
          toolName: "ImageGeneration",
          status: "completed",
          content: [
            {
              type: "content",
              content: {
                type: "image",
                uri: "/workspace/output.png",
                mimeType: "image/png"
              }
            }
          ]
        }
      })
    ];

    const transcript = await serializeAgentConversationForClipboard({
      labels,
      messages,
      readLocalImage,
      title: "Local image"
    });

    expect(readLocalImage).toHaveBeenCalledTimes(1);
    expect(transcript.markdown).toContain("![Image](</workspace/output.png>)");
    expect(transcript.hydratedMarkdown).toContain(
      "![Image](</workspace/output.png>)"
    );
    expect(transcript.omittedImages).toBe(1);
  });

  it("keeps data-URI sources out of text/plain and caps them on the hydrated side", async () => {
    const oversized = "A".repeat(
      Math.ceil(
        ((AGENT_CONVERSATION_COPY_MAX_EMBEDDED_IMAGE_BYTES + 3) * 4) / 3
      )
    );
    const messages = [
      message({
        kind: "tool_call",
        messageId: "tool-1",
        role: "assistant",
        sequence: 1,
        payload: {
          toolName: "ImageGeneration",
          status: "completed",
          content: [
            {
              type: "image",
              uri: "data:image/png;base64,QUFB",
              mimeType: "image/png",
              name: "small.png"
            },
            {
              type: "image",
              uri: `data:image/png;base64,${oversized}`,
              mimeType: "image/png",
              name: "huge.png"
            }
          ]
        }
      })
    ];

    const transcript = await serializeAgentConversationForClipboard({
      labels,
      messages,
      title: "Data URI sources"
    });

    expect(transcript.markdown).not.toContain("base64");
    expect(transcript.markdown).toContain("**small.png**");
    expect(transcript.markdown).toContain("**huge.png**");
    expect(transcript.hydratedMarkdown).toContain(
      "![small.png](<data:image/png;base64,QUFB>)"
    );
    expect(transcript.hydratedMarkdown).toContain("**huge.png**");
    expect(transcript.hydratedMarkdown).not.toContain(oversized);
    expect(transcript.omittedImages).toBe(1);
  });

  it("hydrates user-message images through readAttachment into the blockquote", async () => {
    const readAttachment = vi.fn().mockResolvedValue({
      data: "aW1hZ2U=",
      mimeType: "image/png",
      name: "wireframe.png"
    });
    const messages = [
      message({
        messageId: "user-1",
        role: "user",
        sequence: 1,
        payload: {
          content: [
            { type: "text", text: "Please review this" },
            {
              type: "image",
              attachmentId: "attachment-1",
              mimeType: "image/png",
              name: "wireframe.png"
            }
          ]
        }
      })
    ];

    const transcript = await serializeAgentConversationForClipboard({
      labels,
      messages,
      readAttachment,
      title: "Design review"
    });

    expect(readAttachment).toHaveBeenCalledWith("attachment-1");
    expect(transcript.hydratedMarkdown).toContain(
      "> ![wireframe.png](<data:image/png;base64,aW1hZ2U=>)"
    );
    expect(transcript.markdown).toContain(
      "> ![wireframe.png](<attachment:attachment-1>)"
    );
  });
});

function message(
  overrides: Partial<AgentActivityMessage>
): AgentActivityMessage {
  return {
    agentSessionId: "session-1",
    kind: "message",
    messageId: "message-1",
    occurredAtUnixMs: 1,
    payload: { content: "content" },
    role: "assistant",
    turnId: "turn-1",
    version: 1,
    ...overrides
  };
}
