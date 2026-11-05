import {
  act,
  fireEvent,
  render,
  screen,
  waitFor
} from "@testing-library/react";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  resetAgentHostApiForTests,
  setAgentHostApiForTests
} from "../../../agentActivityHost";
import type { ConversationServices } from "../../../conversationServices";
import { AgentEnvPanelActionProvider } from "../../agentEnv";
import type {
  AgentMessageContentVM,
  AgentMessageRowVM
} from "../contracts/agentMessageRowVM";
import type { AgentToolCallVM } from "../contracts/agentToolCallVM";
import type { AgentToolGroupRowVM } from "../contracts/agentToolGroupRowVM";
import { AgentMessageBlock } from "./AgentMessageBlock";
import { AgentTranscriptItemContent as AgentTranscriptItemView } from "./AgentTranscriptItemView";

const mockState = vi.hoisted(() => ({
  markdownOnLinkClicks: [] as Array<((href: string) => void) | undefined>,
  markdownStreamingFlags: [] as Array<boolean | undefined>,
  toolGroupOnLinkClicks: [] as Array<((href: string) => void) | undefined>
}));
const agentActivityStyles = readFileSync(
  resolve(process.cwd(), "app/renderer/agentactivity.css"),
  "utf8"
);
const agentActivityStyleElement = document.createElement("style");
agentActivityStyleElement.textContent = agentActivityStyles;

afterEach(() => {
  agentActivityStyleElement.remove();
});

vi.mock("../../../i18n/index", () => ({
  getActiveUiLanguage: () => "en",
  useTranslation: () => ({
    t: (key: string) => key
  }),
  translate: (key: string) => key
}));

vi.mock("../../AgentMessageMarkdown", () => ({
  AgentMessageMarkdown: ({
    content,
    onLinkClick,
    streaming
  }: {
    content: string;
    onLinkClick?: (href: string) => void;
    streaming?: boolean;
  }) => {
    mockState.markdownOnLinkClicks.push(onLinkClick);
    mockState.markdownStreamingFlags.push(streaming);
    return <div>{content}</div>;
  }
}));

vi.mock("../../AgentRichTextReadonly", () => ({
  AgentRichTextReadonly: ({
    className,
    onLinkClick,
    value
  }: {
    className?: string;
    onLinkClick?: (href: string) => void;
    value?: string;
  }) => {
    mockState.markdownOnLinkClicks.push(onLinkClick);
    return <div className={className}>{value ?? "User message"}</div>;
  }
}));

vi.mock("./AgentToolGroupRow", () => ({
  AgentToolGroupRow: ({
    onLinkClick
  }: {
    onLinkClick?: (href: string) => void;
  }) => {
    mockState.toolGroupOnLinkClicks.push(onLinkClick);
    return <div>Tool group</div>;
  }
}));

beforeEach(() => {
  mockState.markdownOnLinkClicks.length = 0;
  mockState.markdownStreamingFlags.length = 0;
  mockState.toolGroupOnLinkClicks.length = 0;
});

describe("AgentTranscriptItemView render stability", () => {
  it("keeps tool link handlers stable when an unchanged tool row is reprojected", () => {
    const onLinkAction = vi.fn();
    const labels = transcriptLabels();
    const { rerender } = render(
      <AgentTranscriptItemView
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={toolGroupRow()}
        labels={labels}
        onLinkAction={onLinkAction}
      />
    );

    rerender(
      <AgentTranscriptItemView
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={toolGroupRow()}
        labels={labels}
        onLinkAction={onLinkAction}
      />
    );

    expect(mockState.toolGroupOnLinkClicks).toHaveLength(2);
    expect(mockState.toolGroupOnLinkClicks[1]).toBe(
      mockState.toolGroupOnLinkClicks[0]
    );
  });

  it("keeps assistant markdown link handlers stable when an unchanged message row is reprojected", () => {
    const onLinkAction = vi.fn();
    const { rerender } = render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={assistantMessageRow()}
        onLinkAction={onLinkAction}
        thinkingLabel="Thought process"
      />
    );

    rerender(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={assistantMessageRow()}
        onLinkAction={onLinkAction}
        thinkingLabel="Thought process"
      />
    );

    expect(mockState.markdownOnLinkClicks).toHaveLength(2);
    expect(mockState.markdownOnLinkClicks[1]).toBe(
      mockState.markdownOnLinkClicks[0]
    );
  });

  it("routes a clicked app artifact reference from a sent user message to the owning app", () => {
    const onLinkAction = vi.fn();
    render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={userMessageRow({
          kind: "message-content",
          id: "user-app-reference-1",
          turnId: "turn-1",
          body: "[@AI Canvas](mention://workspace-reference/ai-canvas?source=app&workspaceId=workspace-1)",
          occurredAtUnixMs: 1
        })}
        onLinkAction={onLinkAction}
        thinkingLabel="Thought process"
      />
    );

    mockState.markdownOnLinkClicks.at(-1)?.(
      "mention://workspace-reference/ai-canvas?source=app&workspaceId=workspace-1"
    );

    expect(onLinkAction).toHaveBeenCalledWith({
      type: "open-workspace-app",
      workspaceId: "workspace-1",
      appId: "ai-canvas",
      source: "agent-markdown"
    });
  });

  it("toasts when a sent composer-file mention can no longer be resolved", () => {
    const onLinkAction = vi.fn();
    const hostToastError = vi.fn();
    setAgentHostApiForTests({
      toast: { error: hostToastError }
    } as never);

    try {
      render(
        <AgentMessageBlock
          workspaceRoot="/workspace/demo"
          basePath="/workspace/demo"
          row={userMessageRow({
            kind: "message-content",
            id: "user-composer-file-1",
            turnId: "turn-1",
            body: "[diagnostics.md](mention://composer-file/file-1?status=ready)",
            occurredAtUnixMs: 1
          })}
          onLinkAction={onLinkAction}
          thinkingLabel="Thought process"
        />
      );

      mockState.markdownOnLinkClicks.at(-1)?.(
        "mention://composer-file/file-1?status=ready"
      );

      expect(onLinkAction).not.toHaveBeenCalled();
      expect(hostToastError).toHaveBeenCalledWith(
        "agentHost.agentGui.composerFileOpenUnavailable"
      );
    } finally {
      resetAgentHostApiForTests();
    }
  });

  it("keeps markdown streaming for active turns and unsettled messages", () => {
    render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={assistantMessageRow({
          kind: "message-content",
          id: "assistant-working-1",
          turnId: "turn-1",
          body: "Streaming answer",
          statusKind: "working",
          occurredAtUnixMs: 1
        })}
        thinkingLabel="Thought process"
      />
    );

    render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={assistantMessageRow({
          kind: "message-content",
          id: "assistant-completed-1",
          turnId: "turn-1",
          body: "Completed answer",
          statusKind: "completed",
          occurredAtUnixMs: 1
        })}
        thinkingLabel="Thought process"
      />
    );

    render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={assistantMessageRow({
          kind: "message-content",
          id: "assistant-active-turn-1",
          turnId: "turn-1",
          body: "Tool completed, answer still growing",
          statusKind: "completed",
          occurredAtUnixMs: 1
        })}
        thinkingLabel="Thought process"
        isActiveTurn
      />
    );

    expect(mockState.markdownStreamingFlags.at(-3)).toBe(true);
    expect(mockState.markdownStreamingFlags.at(-2)).toBe(false);
    expect(mockState.markdownStreamingFlags.at(-1)).toBe(true);
  });

  it("keeps participant presentation off by default", () => {
    const { container } = render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={userMessageRow()}
        thinkingLabel="Thought process"
      />
    );

    const flow = document.querySelector(
      ".agent-gui-conversation__user-message-flow"
    );
    expect(flow).toBeInstanceOf(HTMLElement);
    expect(flow?.children).toHaveLength(1);
    const group = flow?.children.item(0);
    expect(group).toBeInstanceOf(HTMLElement);
    if (!(group instanceof HTMLElement)) {
      throw new Error("Expected user message group to render.");
    }
    const bubble = group.children.item(0);
    expect(bubble).toBeInstanceOf(HTMLElement);
    if (!(bubble instanceof HTMLElement)) {
      throw new Error("Expected user message bubble to render.");
    }
    expect(group).toHaveAttribute("data-agent-message-speaker", "user");
    expect(group).toHaveAttribute("data-agent-message-footer", "true");
    expect(group).toHaveTextContent("User asks for a fix");
    expect(
      group.querySelector(".agent-gui-conversation__message-copy-button")
    ).toBeInstanceOf(HTMLButtonElement);
    expect(
      container.querySelector("[data-agent-conversation-participant-avatar]")
    ).toBeNull();
    expect(
      container.querySelector(
        ".agent-gui-conversation__participant-message-layout"
      )
    ).toBeNull();
  });

  it("preserves the existing message DOM when participant presentation is disabled", () => {
    const { container } = render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={userMessageRow()}
        thinkingLabel="Thought process"
        participantPresentation={{ enabled: false }}
      />
    );

    expect(
      container.querySelector(
        ".agent-gui-conversation__participant-message-layout"
      )
    ).toBeNull();
    expect(
      container.querySelector("[data-agent-conversation-participant-avatar]")
    ).toBeNull();
    expect(
      container.querySelector(".agent-gui-conversation__user-message-flow")
        ?.children
    ).toHaveLength(1);
  });

  it("renders fixed participant avatar loading slots without hiding messages", () => {
    const participantPresentation = {
      enabled: true,
      status: "loading"
    } as const;
    const { container } = render(
      <>
        <AgentMessageBlock
          workspaceRoot="/workspace/demo"
          basePath="/workspace/demo"
          row={assistantMessageRow()}
          thinkingLabel="Thought process"
          participantPresentation={participantPresentation}
        />
        <AgentMessageBlock
          workspaceRoot="/workspace/demo"
          basePath="/workspace/demo"
          row={userMessageRow()}
          thinkingLabel="Thought process"
          participantPresentation={participantPresentation}
        />
      </>
    );

    const avatars = container.querySelectorAll(
      "[data-agent-conversation-participant-avatar]"
    );
    expect(avatars).toHaveLength(2);
    expect(avatars[0]).toHaveAttribute(
      "data-agent-conversation-participant-avatar",
      "assistant"
    );
    expect(avatars[1]).toHaveAttribute(
      "data-agent-conversation-participant-avatar",
      "user"
    );
    expect(avatars[0]).toHaveAttribute("data-avatar-state", "loading");
    expect(avatars[1]).toHaveAttribute("data-avatar-state", "loading");
    expect(screen.getByText(/Assistant answer/)).toBeInTheDocument();
    expect(screen.getByText("User asks for a fix")).toBeInTheDocument();
  });

  it("renders the avatar and sender name in a header row above the message", () => {
    const participantPresentation = {
      enabled: true,
      status: "ready",
      agent: { name: "Codex" },
      user: { name: "Alice" }
    } as const;
    const { container } = render(
      <>
        <AgentMessageBlock
          workspaceRoot="/workspace/demo"
          basePath="/workspace/demo"
          row={assistantMessageRow()}
          thinkingLabel="Thought process"
          participantPresentation={participantPresentation}
        />
        <AgentMessageBlock
          workspaceRoot="/workspace/demo"
          basePath="/workspace/demo"
          row={userMessageRow()}
          thinkingLabel="Thought process"
          participantPresentation={participantPresentation}
        />
      </>
    );

    const assistantLayout = container.querySelector(
      '.agent-gui-conversation__participant-message-layout[data-agent-message-speaker="assistant"]'
    );
    const userLayout = container.querySelector(
      '.agent-gui-conversation__participant-message-layout[data-agent-message-speaker="user"]'
    );
    const assistantHeader = container.querySelector(
      '[data-agent-conversation-participant-header="assistant"]'
    );
    expect(assistantHeader).toHaveClass(
      "agent-gui-conversation__participant-message-header"
    );
    expect(
      assistantHeader?.querySelector(
        '[data-agent-conversation-participant-avatar="assistant"]'
      )
    ).toHaveAttribute("aria-label", "Codex");
    expect(assistantHeader).toHaveTextContent("Codex");
    // The header row renders above the thinking/tool-call info and the
    // message content.
    expect(
      assistantHeader?.compareDocumentPosition(assistantLayout as Element)
    ).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    const userHeader = container.querySelector(
      '[data-agent-conversation-participant-header="user"]'
    );
    expect(userHeader).toHaveClass(
      "agent-gui-conversation__participant-message-header"
    );
    expect(
      userHeader?.querySelector(
        '[data-agent-conversation-participant-avatar="user"]'
      )
    ).toHaveAttribute("aria-label", "Alice");
    expect(userHeader).toHaveTextContent("Alice");
    expect(userHeader?.compareDocumentPosition(userLayout as Element)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING
    );
  });

  it("renders leading tool rows under the participant header above the message", () => {
    const participantPresentation = {
      enabled: true,
      status: "ready",
      agent: { name: "Codex" },
      user: { name: "Alice" }
    } as const;
    const leadingToolGroup: AgentToolGroupRowVM = {
      kind: "tool-group",
      id: "leading-tools-1",
      turnId: "turn-1",
      grouped: true,
      calls: [toolCall(), toolCall()],
      entries: [],
      occurredAtUnixMs: 1
    };
    const row: AgentMessageRowVM = {
      ...assistantMessageRow(),
      leadingToolRows: [leadingToolGroup]
    };
    const { container, getByText } = render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={row}
        thinkingLabel="Thought process"
        participantPresentation={participantPresentation}
      />
    );

    const header = container.querySelector(
      '[data-agent-conversation-participant-header="assistant"]'
    );
    const toolContent = getByText("Tool group");
    const messageContent = getByText(/Assistant answer/);
    expect(header).not.toBeNull();
    expect(header?.compareDocumentPosition(toolContent)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING
    );
    expect(toolContent.compareDocumentPosition(messageContent)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING
    );
  });

  it("copies user message text through the agent host clipboard", async () => {
    const writeText = vi.fn(async () => undefined);
    installAgentHostClipboard(writeText);

    render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={userMessageRow()}
        thinkingLabel="Thought process"
      />
    );

    fireEvent.click(
      screen.getByRole("button", {
        name: "agentHost.agentGui.copyMessage"
      })
    );

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("User asks for a fix");
    });
    expect(
      screen.getByRole("button", {
        name: "agentHost.agentGui.messageCopied"
      })
    ).toBeTruthy();
  });

  it("copies assistant markdown source through the agent host clipboard", async () => {
    const writeText = vi.fn(async () => undefined);
    installAgentHostClipboard(writeText);

    render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={assistantMessageRow({
          kind: "message-content",
          id: "assistant-copy-1",
          turnId: "turn-1",
          body: "Assistant **summary** with `code`",
          copyText: "Assistant **summary** with `code`",
          occurredAtUnixMs: 1
        })}
        thinkingLabel="Thought process"
      />
    );

    fireEvent.click(
      screen.getByRole("button", {
        name: "agentHost.agentGui.copyMessage"
      })
    );

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith(
        "Assistant **summary** with `code`"
      );
    });
  });

  it("loads user prompt image attachments from the activity runtime", async () => {
    const readSessionAttachment = vi.fn(async () => ({
      attachmentId: "attachment-1",
      data: "aW1hZ2U=",
      mimeType: "image/png" as const,
      name: "screen.png"
    }));
    Object.defineProperty(window, "agentGUIRuntime", {
      configurable: true,
      value: {
        attachments: { readSessionAttachment }
      } as Partial<ConversationServices>
    });

    render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={userMessageRow({
          kind: "message-content",
          id: "user-images-1",
          turnId: "turn-1",
          body: "",
          contentKind: "image-grid",
          images: [
            {
              id: "attachment-1",
              workspaceId: "room-1",
              agentSessionId: "session-1",
              attachmentId: "attachment-1",
              mimeType: "image/png",
              name: "screen.png"
            }
          ],
          occurredAtUnixMs: 1
        })}
        thinkingLabel="Thought process"
      />
    );

    await waitFor(() => {
      expect(readSessionAttachment).toHaveBeenCalledWith({
        workspaceId: "room-1",
        agentSessionId: "session-1",
        attachmentId: "attachment-1"
      });
    });
    const image = await screen.findByRole("img", { name: "screen.png" });
    expect(image).toHaveAttribute("src", "data:image/png;base64,aW1hZ2U=");
    fireEvent.load(image);
    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "common.expandImage" })
      ).toBeTruthy();
    });

    delete (window as { agentGUIRuntime?: unknown }).agentGUIRuntime;
  });

  it("offers retry for an ordinary prompt image read failure", async () => {
    const readSessionAttachment = vi.fn(async () => {
      throw new Error("attachment not found");
    });
    Object.defineProperty(window, "agentGUIRuntime", {
      configurable: true,
      value: {
        attachments: { readSessionAttachment }
      } as Partial<ConversationServices>
    });

    render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={userMessageRow({
          kind: "message-content",
          id: "user-images-read-failed",
          turnId: "turn-1",
          body: "",
          contentKind: "image-grid",
          images: [
            {
              id: "read-failed-image",
              workspaceId: "room-1",
              agentSessionId: "session-1",
              attachmentId: "attachment-read-failed",
              mimeType: "image/png",
              name: "screen.png"
            }
          ],
          occurredAtUnixMs: 1
        })}
        thinkingLabel="Thought process"
      />
    );

    expect(
      await screen.findByText("agentHost.agentGui.imageLoadFailed")
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "agentHost.agentGui.retryImage" })
    ).toBeInTheDocument();

    delete (window as { agentGUIRuntime?: unknown }).agentGUIRuntime;
  });

  it("does not offer retry when the prompt image belongs to another VM", async () => {
    const readSessionAttachment = vi.fn(async () => {
      throw Object.assign(new Error("attachment is on another VM"), {
        code: "agent_session_attachment_vm_unavailable"
      });
    });
    Object.defineProperty(window, "agentGUIRuntime", {
      configurable: true,
      value: {
        attachments: { readSessionAttachment }
      } as Partial<ConversationServices>
    });

    render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={userMessageRow({
          kind: "message-content",
          id: "user-images-vm-unavailable",
          turnId: "turn-1",
          body: "",
          contentKind: "image-grid",
          images: [
            {
              id: "vm-unavailable-image",
              workspaceId: "room-1",
              agentSessionId: "session-1",
              attachmentId: "attachment-from-another-vm",
              mimeType: "image/png",
              name: "screen.png"
            }
          ],
          occurredAtUnixMs: 1
        })}
        thinkingLabel="Thought process"
      />
    );

    expect(
      await screen.findByText("agentHost.agentGui.imageTemporarilyUnavailable")
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "agentHost.agentGui.retryImage" })
    ).toBeNull();

    delete (window as { agentGUIRuntime?: unknown }).agentGUIRuntime;
  });

  it("keeps a loaded optimistic image mounted when its durable attachment arrives", async () => {
    const readPromptAsset = vi.fn(async () => ({
      attachmentId: "prompt-asset-1",
      data: "b3B0aW1pc3RpYw==",
      mimeType: "image/png" as const,
      name: "screen.png",
      path: "/prompt-assets/screen.png"
    }));
    const readSessionAttachment = vi.fn(async () => ({
      attachmentId: "attachment-1",
      data: "ZHVyYWJsZQ==",
      mimeType: "image/png" as const,
      name: "screen.png"
    }));
    Object.defineProperty(window, "agentGUIRuntime", {
      configurable: true,
      value: {
        attachments: { readPromptAsset, readSessionAttachment }
      } as Partial<ConversationServices>
    });
    const stableImageId = "client-submit:user:submit-1:image:0";
    const optimisticMessage: AgentMessageContentVM = {
      kind: "message-content",
      id: "client-submit:user:submit-1:images:0",
      turnId: "turn-1",
      body: "",
      presentationKind: "content",
      contentKind: "image-grid",
      images: [
        {
          id: stableImageId,
          workspaceId: "room-1",
          agentSessionId: "session-1",
          mimeType: "image/png",
          name: "screen.png",
          path: "/prompt-assets/screen.png"
        }
      ],
      occurredAtUnixMs: 1
    };
    const durableMessage: AgentMessageContentVM = {
      ...optimisticMessage,
      images: [
        {
          id: stableImageId,
          workspaceId: "room-1",
          agentSessionId: "session-1",
          attachmentId: "attachment-1",
          mimeType: "image/png",
          name: "screen.png"
        }
      ],
      occurredAtUnixMs: 2
    };
    const { rerender } = render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={userMessageRow(optimisticMessage)}
        thinkingLabel="Thought process"
      />
    );

    const optimisticImage = await screen.findByRole("img", {
      name: "screen.png"
    });
    expect(optimisticImage).toHaveAttribute(
      "src",
      "data:image/png;base64,b3B0aW1pc3RpYw=="
    );
    fireEvent.load(optimisticImage);
    await waitFor(() => {
      expect(
        screen.queryByTestId("agent-gui-message-image-loading")
      ).toBeNull();
    });

    rerender(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={userMessageRow(durableMessage)}
        thinkingLabel="Thought process"
      />
    );

    expect(screen.getByRole("img", { name: "screen.png" })).toBe(
      optimisticImage
    );
    expect(screen.queryByTestId("agent-gui-message-image-loading")).toBeNull();
    expect(readPromptAsset).toHaveBeenCalledTimes(1);
    expect(readSessionAttachment).not.toHaveBeenCalled();

    delete (window as { agentGUIRuntime?: unknown }).agentGUIRuntime;
  });

  it("reserves a loading slot until a remote HTTPS image loads", async () => {
    document.head.append(agentActivityStyleElement);
    const readPromptAsset = vi.fn();
    Object.defineProperty(window, "agentGUIRuntime", {
      configurable: true,
      value: {
        attachments: { readPromptAsset }
      } as Partial<ConversationServices>
    });

    render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={userMessageRow({
          kind: "message-content",
          id: "user-images-url-1",
          turnId: "turn-1",
          body: "",
          contentKind: "image-grid",
          images: [
            {
              id: "remote-image-1",
              workspaceId: "room-1",
              agentSessionId: "session-1",
              mimeType: "image/png",
              name: "screen.png",
              url: "https://objects.example.test/signed/screen.png"
            }
          ],
          occurredAtUnixMs: 1
        })}
        thinkingLabel="Thought process"
      />
    );

    const image = screen.getByRole("img", { name: "screen.png" });
    expect(image).toHaveAttribute(
      "src",
      "https://objects.example.test/signed/screen.png"
    );
    expect(
      screen.getByTestId("agent-gui-message-image-loading")
    ).toBeInTheDocument();
    expect(image.closest("[data-image-state]")).toHaveAttribute(
      "data-image-state",
      "loading"
    );
    const imageSlot = image.closest("[data-image-state]");
    expect(imageSlot).not.toBeNull();
    expect(window.getComputedStyle(imageSlot as Element).height).toBe("80px");

    fireEvent.load(image);

    await waitFor(() => {
      expect(
        screen.queryByTestId("agent-gui-message-image-loading")
      ).toBeNull();
      expect(image.closest("[data-image-state]")).toHaveAttribute(
        "data-image-state",
        "loaded"
      );
      expect(window.getComputedStyle(imageSlot as Element).height).toBe("80px");
    });
    expect(readPromptAsset).not.toHaveBeenCalled();

    delete (window as { agentGUIRuntime?: unknown }).agentGUIRuntime;
  });

  it("shows a failure placeholder and retries a remote image with the same URL", async () => {
    const signedUrl = "https://objects.example.test/signed/screen.png";

    render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={userMessageRow({
          kind: "message-content",
          id: "user-images-url-failed",
          turnId: "turn-1",
          body: "",
          contentKind: "image-grid",
          images: [
            {
              id: "remote-image-failed",
              workspaceId: "room-1",
              agentSessionId: "session-1",
              mimeType: "image/png",
              name: "screen.png",
              url: signedUrl
            }
          ],
          occurredAtUnixMs: 1
        })}
        thinkingLabel="Thought process"
      />
    );

    const image = screen.getByRole("img", { name: "screen.png" });
    expect(
      screen.getByTestId("agent-gui-message-image-loading")
    ).toBeInTheDocument();
    fireEvent.error(image);

    expect(
      await screen.findByTestId("agent-gui-message-image-failed")
    ).toBeInTheDocument();
    expect(
      screen.getByText("agentHost.agentGui.imageLoadFailed")
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "agentHost.agentGui.retryImage" })
    ).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: "agentHost.agentGui.retryImage" })
    );
    const retriedImage = await screen.findByRole("img", {
      name: "screen.png"
    });
    expect(retriedImage).not.toBe(image);
    expect(retriedImage).toHaveAttribute("src", signedUrl);
    expect(
      screen.getByTestId("agent-gui-message-image-loading")
    ).toBeInTheDocument();
    fireEvent.load(retriedImage);

    expect(screen.queryByTestId("agent-gui-message-image-failed")).toBeNull();
    expect(screen.queryByTestId("agent-gui-message-image-loading")).toBeNull();
  });

  it("does not offer retry when a prompt image is unavailable in this host", async () => {
    delete (window as { agentGUIRuntime?: unknown }).agentGUIRuntime;

    render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={userMessageRow({
          kind: "message-content",
          id: "user-images-unavailable",
          turnId: "turn-1",
          body: "",
          contentKind: "image-grid",
          images: [
            {
              id: "unavailable-image",
              workspaceId: "room-1",
              agentSessionId: "session-1",
              attachmentId: "attachment-from-another-host",
              mimeType: "image/png",
              name: "screen.png"
            }
          ],
          occurredAtUnixMs: 1
        })}
        thinkingLabel="Thought process"
      />
    );

    expect(
      await screen.findByTestId("agent-gui-message-image-failed")
    ).toBeInTheDocument();
    expect(
      screen.getByText("agentHost.agentGui.imageTemporarilyUnavailable")
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "agentHost.agentGui.retryImage" })
    ).toBeNull();
  });

  it("starts a fresh load when a failed remote image URL changes", async () => {
    const firstUrl = "https://objects.example.test/signed/screen-v1.png";
    const secondUrl = "https://objects.example.test/signed/screen-v2.png";
    const renderMessage = (url: string) => (
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={userMessageRow({
          kind: "message-content",
          id: "user-images-url-refreshed",
          turnId: "turn-1",
          body: "",
          contentKind: "image-grid",
          images: [
            {
              id: "remote-image-refreshed",
              workspaceId: "room-1",
              agentSessionId: "session-1",
              mimeType: "image/png",
              name: "screen.png",
              url
            }
          ],
          occurredAtUnixMs: 1
        })}
        thinkingLabel="Thought process"
      />
    );
    const { rerender } = render(renderMessage(firstUrl));

    const firstImage = screen.getByRole("img", { name: "screen.png" });
    fireEvent.error(firstImage);
    expect(
      await screen.findByTestId("agent-gui-message-image-failed")
    ).toBeInTheDocument();

    rerender(renderMessage(secondUrl));

    const refreshedImage = await screen.findByRole("img", {
      name: "screen.png"
    });
    expect(refreshedImage).not.toBe(firstImage);
    expect(refreshedImage).toHaveAttribute("src", secondUrl);
    expect(screen.queryByTestId("agent-gui-message-image-failed")).toBeNull();
    expect(
      screen.getByTestId("agent-gui-message-image-loading")
    ).toBeInTheDocument();

    fireEvent.load(refreshedImage);

    expect(screen.queryByTestId("agent-gui-message-image-loading")).toBeNull();
  });

  it("shows a loading spinner while a user prompt image is being read", async () => {
    const readController: {
      resolve: (value: {
        attachmentId: string;
        data: string;
        mimeType: "image/png";
        name: string;
      }) => void;
    } = {
      resolve: () => {
        throw new Error("readSessionAttachment was not called");
      }
    };
    const readSessionAttachment = vi.fn(
      () =>
        new Promise<{
          attachmentId: string;
          data: string;
          mimeType: "image/png";
          name: string;
        }>((resolvePromise) => {
          readController.resolve = resolvePromise;
        })
    );
    Object.defineProperty(window, "agentGUIRuntime", {
      configurable: true,
      value: {
        attachments: { readSessionAttachment }
      } as Partial<ConversationServices>
    });

    render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={userMessageRow({
          kind: "message-content",
          id: "user-images-loading",
          turnId: "turn-1",
          body: "",
          contentKind: "image-grid",
          images: [
            {
              id: "attachment-loading",
              workspaceId: "room-1",
              agentSessionId: "session-1",
              attachmentId: "attachment-loading",
              mimeType: "image/png",
              name: "screen.png"
            }
          ],
          occurredAtUnixMs: 1
        })}
        thinkingLabel="Thought process"
      />
    );

    expect(
      await screen.findByTestId("agent-gui-message-image-loading")
    ).toBeTruthy();
    expect(screen.queryByRole("img", { name: "screen.png" })).toBeNull();

    readController.resolve({
      attachmentId: "attachment-loading",
      data: "aW1hZ2U=",
      mimeType: "image/png",
      name: "screen.png"
    });

    const image = await screen.findByRole("img", { name: "screen.png" });
    expect(image).toHaveAttribute("src", "data:image/png;base64,aW1hZ2U=");
    expect(
      screen.getByTestId("agent-gui-message-image-loading")
    ).toBeInTheDocument();

    fireEvent.load(image);

    await waitFor(() => {
      expect(
        screen.queryByTestId("agent-gui-message-image-loading")
      ).toBeNull();
    });

    delete (window as { agentGUIRuntime?: unknown }).agentGUIRuntime;
  });

  it("shows local agent sign-in guidance for auth errors", () => {
    const onOpenAgentEnvPanel = vi.fn();
    const { getByText } = render(
      <AgentEnvPanelActionProvider openPanel={onOpenAgentEnvPanel}>
        <AgentMessageBlock
          workspaceRoot="/workspace/demo"
          basePath="/workspace/demo"
          row={assistantMessageRow(claudeCodeAuthErrorMessage())}
          thinkingLabel="Thought process"
        />
      </AgentEnvPanelActionProvider>
    );

    expect(
      getByText("agentHost.agentGui.visibleErrorAuthRequired")
    ).toBeTruthy();
    expect(
      getByText("agentHost.agentGui.visibleErrorAuthRequiredLocalAgentHint")
    ).toBeTruthy();
    fireEvent.click(getByText("agentHost.agentGui.visibleErrorActionRelogin"));
    expect(onOpenAgentEnvPanel).toHaveBeenCalledWith({
      provider: "claude-code",
      focus: "auth"
    });
  });

  it("renders transport retry notice details without the generic title", () => {
    const { getByText, queryByText } = render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={assistantMessageRow({
          kind: "message-content",
          id: "assistant-notice-1",
          turnId: "turn-1",
          body: "Codex connection interrupted. Reconnecting...",
          occurredAtUnixMs: 1,
          systemNotice: {
            noticeKind: "transport_retry",
            severity: "warning",
            title: "Codex connection interrupted. Reconnecting...",
            detail:
              "Handled error during turn: Reconnecting... 1/5 Some(ResponseStreamDisconnected { http_status_code: None })",
            retryable: true
          }
        })}
        thinkingLabel="Thought process"
      />
    );

    expect(getByText("Reconnecting... 1/5")).toBeTruthy();
    const notice = getByText("Reconnecting... 1/5");
    expect(notice.tagName).toBe("DIV");
    expect(queryByText("agentHost.agentGui.visibleErrorDetails")).toBeNull();
    expect(
      queryByText("Codex connection interrupted. Reconnecting...")
    ).toBeNull();
    expect(
      queryByText("agentHost.agentGui.systemNoticeTransportRetry")
    ).toBeNull();
  });

  it.each([
    [
      "plan_implementation_pending_confirmation",
      "agentHost.agentGui.systemNoticePlanImplementationPendingConfirmation"
    ],
    [
      "plan_implementation_completed",
      "agentHost.agentGui.systemNoticePlanImplementationCompleted"
    ]
  ])(
    "renders semantic plan notice %s without transport text",
    (noticeKind, key) => {
      const { getByText } = render(
        <AgentMessageBlock
          workspaceRoot="/workspace/demo"
          basePath="/workspace/demo"
          row={assistantMessageRow({
            kind: "message-content",
            id: `assistant-${noticeKind}`,
            turnId: "turn-1",
            body: "",
            occurredAtUnixMs: 1,
            systemNotice: {
              noticeKind,
              severity: "info",
              title: null,
              detail: null,
              retryable: false
            }
          })}
          thinkingLabel="Thought process"
        />
      );
      expect(getByText(key)).toBeTruthy();
    }
  );

  it("renders transport fallback notices with the localized label", () => {
    const { getByRole, getByText } = render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={assistantMessageRow({
          kind: "message-content",
          id: "assistant-transport-fallback",
          turnId: "turn-1",
          body: "Falling back from WebSockets to HTTPS transport.",
          occurredAtUnixMs: 1,
          systemNotice: {
            noticeKind: "transport_fallback",
            severity: "warning",
            title: "Falling back from WebSockets to HTTPS transport.",
            detail:
              "stream disconnected before completion: websocket closed by server before response.completed",
            retryable: true
          }
        })}
        thinkingLabel="Thought process"
      />
    );

    expect(getByRole("status")).toBeTruthy();
    expect(
      getByText("agentHost.agentGui.systemNoticeTransportFallback")
    ).toBeTruthy();
  });

  it("renders fallback warning notices with their title", () => {
    const { getByRole, getByText } = render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={assistantMessageRow({
          kind: "message-content",
          id: "assistant-warning-fallback",
          turnId: "turn-1",
          body: "Falling back from WebSockets to HTTPS transport.",
          occurredAtUnixMs: 1,
          systemNotice: {
            noticeKind: "warning",
            severity: "warning",
            title: "Falling back from WebSockets to HTTPS transport.",
            detail:
              "stream disconnected before completion: websocket closed by server before response.completed",
            retryable: true
          }
        })}
        thinkingLabel="Thought process"
      />
    );

    expect(getByRole("status")).toBeTruthy();
    expect(
      getByText("Falling back from WebSockets to HTTPS transport.")
    ).toBeTruthy();
  });

  it("renders context compaction notices as an inline divider", () => {
    const { getByRole, getByText, queryByText } = render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={assistantMessageRow({
          kind: "message-content",
          id: "assistant-notice-compaction",
          turnId: "turn-1",
          body: "Context compacted.",
          occurredAtUnixMs: 1,
          systemNotice: {
            noticeKind: "system_notice",
            severity: null,
            command: "compact",
            commandStatus: "completed",
            title: "Context compacted.",
            detail: "",
            retryable: null
          }
        })}
        thinkingLabel="Thought process"
      />
    );

    const notice = getByRole("status");
    expect(notice.tagName).toBe("DIV");
    const dividers = notice.querySelectorAll('span[aria-hidden="true"]');
    expect(dividers).toHaveLength(2);
    expect(
      getByText("agentHost.agentGui.contextCompactionCompleted")
    ).toBeTruthy();
    expect(queryByText("agentHost.agentGui.systemNoticeDefault")).toBeNull();
  });

  it("renders in-progress compaction notices as a divider with a ticking timer", () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(66_000);
      const { getByRole, getByText } = render(
        <AgentMessageBlock
          workspaceRoot="/workspace/demo"
          basePath="/workspace/demo"
          row={assistantMessageRow({
            kind: "message-content",
            id: "assistant-notice-compacting",
            turnId: "turn-1",
            body: "Compacting context.",
            occurredAtUnixMs: 61_000,
            systemNotice: {
              noticeKind: "system_notice",
              severity: null,
              command: "compact",
              commandStatus: "running",
              title: "Compacting context.",
              detail: "",
              retryable: null
            }
          })}
          thinkingLabel="Thought process"
        />
      );

      const notice = getByRole("status");
      const dividers = notice.querySelectorAll('span[aria-hidden="true"]');
      expect(dividers).toHaveLength(2);
      expect(
        getByText(/agentHost\.agentGui\.contextCompactionInProgress/)
      ).toBeTruthy();
      expect(getByText(/· 5s/)).toBeTruthy();
      act(() => {
        vi.advanceTimersByTime(3_000);
      });
      expect(getByText(/· 8s/)).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  });

  it("renders interrupted compaction notices with a wrapping detail below the divider", () => {
    const { getByRole, getByText } = render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={assistantMessageRow({
          kind: "message-content",
          id: "assistant-notice-compaction-interrupted",
          turnId: "turn-1",
          body: "Context compaction interrupted.",
          occurredAtUnixMs: 1,
          systemNotice: {
            noticeKind: "system_notice",
            severity: null,
            command: "compact",
            commandStatus: "failed",
            title: "Provider compact failure",
            detail: "Not enough messages to compact.",
            retryable: null
          }
        })}
        thinkingLabel="Thought process"
      />
    );

    const notice = getByRole("status");
    expect(notice.querySelectorAll('span[aria-hidden="true"]')).toHaveLength(2);
    expect(notice.textContent).toContain(
      "agentHost.agentGui.contextCompactionInterrupted"
    );
    expect(notice.textContent).toContain("Not enough messages to compact.");
    const detail = getByText("Not enough messages to compact.");
    expect(detail.className).toContain("break-words");
    expect(detail.className).not.toContain("whitespace-nowrap");
  });

  it("renders context overflow as an error with new-conversation handoff guidance", () => {
    const { getByRole } = render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={assistantMessageRow({
          kind: "message-content",
          id: "assistant-notice-context-handoff",
          turnId: "turn-1",
          body: "Context compaction interrupted.",
          occurredAtUnixMs: 1,
          systemNotice: {
            noticeKind: "context_handoff_required",
            semanticKind: "context-handoff-required",
            severity: "error",
            command: "compact",
            commandStatus: "failed",
            title: "Provider compact failure",
            detail: "Maximum context length exceeded.",
            retryable: false
          }
        })}
        thinkingLabel="Thought process"
      />
    );

    const notice = getByRole("alert");
    expect(notice.textContent).toContain(
      "agentHost.agentGui.contextHandoffRequired"
    );
    expect(notice.textContent).toContain(
      "agentHost.agentGui.contextHandoffRequiredDetail"
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: "agentHost.agentGui.visibleErrorDetails"
      })
    );
    expect(screen.getByText("Maximum context length exceeded.")).toBeTruthy();
  });

  it("does not let a legacy title override canonical compact status", () => {
    const { getByText, queryByText } = render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={assistantMessageRow({
          kind: "message-content",
          id: "assistant-notice-conflicting-compaction",
          turnId: "turn-1",
          body: "Context compacted.",
          occurredAtUnixMs: 1,
          systemNotice: {
            noticeKind: "system_notice",
            severity: null,
            command: "compact",
            commandStatus: "failed",
            title: "Context compacted.",
            detail: "",
            retryable: null
          }
        })}
        thinkingLabel="Thought process"
      />
    );

    expect(
      getByText("agentHost.agentGui.contextCompactionInterrupted")
    ).toBeTruthy();
    expect(
      queryByText("agentHost.agentGui.contextCompactionCompleted")
    ).toBeNull();
  });

  it("renders plan-tagged assistant messages as a dedicated plan card", () => {
    const { getByTestId } = render(
      <AgentMessageBlock
        workspaceRoot="/workspace/demo"
        basePath="/workspace/demo"
        row={assistantMessageRow({
          kind: "message-content",
          id: "assistant-plan-1",
          turnId: "turn-1",
          body: "# Repo health plan\n1. inspect\n2. fix",
          contentKind: "plan",
          occurredAtUnixMs: 1
        })}
        thinkingLabel="Thought process"
      />
    );

    expect(getByTestId("agent-plan-card")).toBeTruthy();
    expect(getByTestId("agent-plan-card-title").textContent).toBe(
      "agentHost.agentGui.planCardTitle"
    );
    expect(getByTestId("agent-plan-card").textContent).toContain(
      "Repo health plan"
    );
  });
});

function transcriptLabels() {
  return {
    thinkingLabel: "Thought process",
    toolCallsLabel: (count: number) => `Tool calls (${count})`,
    processing: "Planning next moves",
    turnSummary: "Changed files"
  };
}

type TestAgentMessageContentVM = Omit<
  AgentMessageContentVM,
  "presentationKind"
> & {
  presentationKind?: AgentMessageContentVM["presentationKind"];
};

function assistantMessageRow(
  message: TestAgentMessageContentVM = {
    kind: "message-content",
    id: "assistant-1",
    turnId: "turn-1",
    body: "Assistant answer with [README](/workspace/demo/README.md)",
    occurredAtUnixMs: 1
  }
): AgentMessageRowVM {
  return {
    kind: "message",
    id: "message-row-1",
    turnId: "turn-1",
    speaker: "assistant",
    messages: [withDefaultPresentationKind(message)],
    thinking: [],
    occurredAtUnixMs: 1
  };
}

function userMessageRow(
  message?: TestAgentMessageContentVM
): AgentMessageRowVM {
  return {
    kind: "message",
    id: "message-row-user-1",
    turnId: "turn-1",
    speaker: "user",
    messages: [
      withDefaultPresentationKind(
        message ?? {
          kind: "message-content",
          id: "user-1",
          turnId: "turn-1",
          body: "User asks for a fix",
          copyText: "User asks for a fix",
          occurredAtUnixMs: 1
        }
      )
    ],
    thinking: [],
    occurredAtUnixMs: 1
  };
}

function withDefaultPresentationKind(
  message: TestAgentMessageContentVM
): AgentMessageContentVM {
  return {
    ...message,
    presentationKind: message.presentationKind ?? "content"
  };
}

function installAgentHostClipboard(
  writeText: (text: string) => Promise<void>
): void {
  (
    window as unknown as {
      agentHostApi?: unknown;
    }
  ).agentHostApi = {
    clipboard: { writeText },
    filesystem: {},
    workspace: {}
  };
}

function claudeCodeAuthErrorMessage(): AgentMessageContentVM {
  return {
    kind: "message-content",
    id: "assistant-auth-1",
    turnId: "turn-1",
    body: "Claude Code needs authentication.",
    presentationKind: "content",
    occurredAtUnixMs: 1,
    visibleError: {
      code: "auth_required",
      phase: "start",
      provider: "claude-code",
      detail: "Failed to authenticate.",
      retryable: false
    }
  };
}

function toolGroupRow(): AgentToolGroupRowVM {
  const call = toolCall();
  return {
    kind: "tool-group",
    id: "tools-1",
    turnId: "turn-1",
    grouped: false,
    calls: [call],
    entries: [
      {
        kind: "tool-call",
        call
      }
    ],
    occurredAtUnixMs: 1
  };
}

function toolCall(): AgentToolCallVM {
  return {
    kind: "tool-call",
    id: "call-1",
    turnId: "turn-1",
    name: "Read File",
    toolName: "read_file",
    callType: "tool",
    status: "Completed",
    statusKind: "completed",
    summary: "/workspace/demo/README.md",
    compactSummary: "/workspace/demo/README.md",
    payload: null,
    input: null,
    output: null,
    error: null,
    metadata: null,
    locations: null,
    rendererKind: "read",
    approval: null,
    planMode: null,
    askUserQuestion: null,
    task: null,
    occurredAtUnixMs: 1
  };
}
