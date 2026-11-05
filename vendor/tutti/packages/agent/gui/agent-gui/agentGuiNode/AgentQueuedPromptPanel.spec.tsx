import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  resetConversationServicesForTests,
  setConversationServicesForTests,
  type ConversationServices
} from "../../conversationServices";
import { AgentQueuedPromptPanelView as AgentQueuedPromptPanel } from "./AgentQueuedPromptPanel";

const labels = {
  queuedLabel: "Queued",
  queuePausedByUserLabel:
    "The queue is paused because you interrupted the current response.",
  sendQueuedPromptNext: "Send next",
  editQueuedPrompt: "Edit",
  deleteQueuedPrompt: "Delete",
  queuedPromptMoreActions: "More"
};

afterEach(() => {
  resetConversationServicesForTests();
});

function textQueuedPrompt(id: string, text: string, createdAtUnixMs = 1) {
  return {
    id,
    content: [{ type: "text" as const, text }],
    createdAtUnixMs
  };
}

describe("AgentQueuedPromptPanel", () => {
  it("switches between the paused and active queue labels while preserving actions", async () => {
    const onSendQueuedPromptNext = vi.fn();
    const onRemoveQueuedPrompt = vi.fn();
    const onEditQueuedPrompt = vi.fn();
    const createPanel = (queueStatus: "active" | "paused_by_user") => (
      <AgentQueuedPromptPanel
        queueStatus={queueStatus}
        queuedPrompts={[
          textQueuedPrompt("queued-1", "first queued prompt"),
          textQueuedPrompt("queued-2", "second queued prompt", 2)
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={onSendQueuedPromptNext}
        onRemoveQueuedPrompt={onRemoveQueuedPrompt}
        onEditQueuedPrompt={onEditQueuedPrompt}
      />
    );
    const { rerender } = render(createPanel("paused_by_user"));

    expect(screen.getByText(labels.queuePausedByUserLabel)).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    fireEvent.click(screen.getAllByRole("button", { name: "Send next" })[1]!);
    fireEvent.click(screen.getAllByRole("button", { name: "Delete" })[0]!);
    const moreButton = screen.getAllByRole("button", { name: "More" })[1]!;
    fireEvent.pointerDown(moreButton, { button: 0, ctrlKey: false });
    fireEvent.click(moreButton);
    const editItem = await screen.findByRole("menuitem", { name: "Edit" });
    fireEvent.pointerDown(editItem, { button: 0, ctrlKey: false });
    fireEvent.click(editItem);

    expect(onSendQueuedPromptNext).toHaveBeenCalledWith("queued-2");
    expect(onRemoveQueuedPrompt).toHaveBeenCalledWith("queued-1");
    expect(onEditQueuedPrompt).toHaveBeenCalledWith("queued-2");

    rerender(createPanel("active"));
    expect(screen.getByText(labels.queuedLabel)).toBeInTheDocument();
    expect(screen.queryByText(labels.queuePausedByUserLabel)).toBeNull();
  });

  it("shows an expand cue only when queued content can expand", () => {
    const { rerender } = render(
      <AgentQueuedPromptPanel
        queuedPrompts={[textQueuedPrompt("queued-1", "short prompt")]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={vi.fn()}
      />
    );

    expect(
      screen.queryByTestId("agent-gui-composer-queued-prompt-expand-cue")
    ).toBeNull();

    rerender(
      <AgentQueuedPromptPanel
        queuedPrompts={[
          textQueuedPrompt("queued-1", "short prompt"),
          textQueuedPrompt("queued-2", "second prompt", 2)
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={vi.fn()}
      />
    );

    expect(
      screen.getByTestId("agent-gui-composer-queued-prompt-expand-cue")
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("agent-gui-composer-queued-prompt-expand-cue")
    ).toHaveClass("lucide-chevron-right");
  });

  it("shows an expand cue for a single queued prompt only when rendered text overflows", () => {
    const scrollWidth = vi
      .spyOn(HTMLElement.prototype, "scrollWidth", "get")
      .mockReturnValue(160);
    const clientWidth = vi
      .spyOn(HTMLElement.prototype, "clientWidth", "get")
      .mockReturnValue(120);

    render(
      <AgentQueuedPromptPanel
        queuedPrompts={[
          textQueuedPrompt(
            "queued-1",
            "single rendered line that overflows its visible row"
          )
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={vi.fn()}
      />
    );

    expect(
      screen.getByTestId("agent-gui-composer-queued-prompt-expand-cue")
    ).toBeInTheDocument();

    scrollWidth.mockRestore();
    clientWidth.mockRestore();
  });

  it("shows one plain-text tooltip for a truncated queue row instead of mention tooltips", async () => {
    const scrollWidth = vi
      .spyOn(HTMLElement.prototype, "scrollWidth", "get")
      .mockImplementation(function (this: HTMLElement) {
        return this.classList.contains("tsh-agent-object-token__main")
          ? 240
          : 120;
      });
    const clientWidth = vi
      .spyOn(HTMLElement.prototype, "clientWidth", "get")
      .mockReturnValue(120);
    const { container } = render(
      <AgentQueuedPromptPanel
        queuedPrompts={[
          textQueuedPrompt(
            "queued-1",
            "Before [@long session](mention://agent-session/session-1?workspaceId=room-1) after **details**"
          )
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={vi.fn()}
      />
    );

    const mention = container.querySelector('[data-agent-file-mention="true"]');
    expect(mention).not.toBeNull();
    expect(screen.queryByRole("tooltip")).toBeNull();

    fireEvent.pointerMove(mention as Element, { pointerType: "mouse" });

    const tooltips = await screen.findAllByRole("tooltip");
    expect(tooltips).toHaveLength(1);
    expect(tooltips[0]).toHaveTextContent("Before @long session after details");

    scrollWidth.mockRestore();
    clientWidth.mockRestore();
  });

  it("renders queued mention prompts as entity tokens instead of raw markdown links", () => {
    const { container } = render(
      <AgentQueuedPromptPanel
        queuedPrompts={[
          textQueuedPrompt(
            "queued-1",
            "[@2046494774160003072 & Claude Code Claude Code](mention://agent-session/session-1?workspaceId=room-1)"
          )
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={vi.fn()}
      />
    );

    expect(screen.getByText("Queued")).toBeInTheDocument();
    const mention = container.querySelector('[data-agent-file-mention="true"]');
    expect(mention).toHaveAttribute("data-agent-mention-kind", "session");
    expect(mention).toHaveClass("tsh-agent-object-token");
    expect(mention).toHaveTextContent(
      "2046494774160003072 & Claude Code Claude Code"
    );
    expect(screen.queryByText(/mention:\/\/session/)).toBeNull();
  });

  it("renders a pasted-text-only queued prompt as a pasted-text chip", () => {
    const { container } = render(
      <AgentQueuedPromptPanel
        queuedPrompts={[
          {
            id: "queued-pasted",
            content: [
              {
                type: "file" as const,
                kind: "pasted-text",
                path: "/archive/aa/deadbeef.txt",
                name: "pasted-text-1.txt"
              }
            ],
            displayPrompt:
              "[@first pasted line](mention://pasted-text/id-1?path=%2Farchive%2Faa%2Fdeadbeef.txt&size=42)",
            createdAtUnixMs: 1
          }
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={vi.fn()}
      />
    );

    const mention = container.querySelector('[data-agent-file-mention="true"]');
    expect(mention).toHaveAttribute("data-agent-mention-kind", "pasted-text");
    expect(mention).toHaveClass("tsh-agent-object-token");
    expect(mention).toHaveTextContent("first pasted line");
    expect(screen.queryByText(/mention:\/\/pasted-text/)).toBeNull();
  });

  it("renders queued workspace app mentions when query params follow the markdown link", () => {
    const iconUrl = "tutti://workspace-apps/ai-media-canvas/icon.png";
    const { container } = render(
      <AgentQueuedPromptPanel
        queuedPrompts={[
          textQueuedPrompt(
            "queued-1",
            "local & Codex [@AI Media Canvas](mention://workspace-app/ai-media-canvas?workspaceId=workspace-1)"
          )
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={vi.fn()}
        workspaceAppIcons={[
          {
            appId: "ai-media-canvas",
            iconUrl,
            workspaceId: "workspace-1"
          }
        ]}
      />
    );

    const mention = container.querySelector('[data-agent-file-mention="true"]');
    expect(mention).toHaveAttribute("data-agent-mention-kind", "workspace-app");
    expect(mention).toHaveAttribute(
      "data-agent-mention-href",
      "mention://workspace-app/ai-media-canvas?workspaceId=workspace-1"
    );
    const image = mention?.querySelector(
      '[data-agent-mention-app-icon="true"] img'
    );
    expect(image).toHaveAttribute("src", iconUrl);
    expect(
      mention?.querySelector('[data-agent-mention-fallback-icon="true"]')
    ).not.toBeInTheDocument();
    expect(
      mention?.querySelectorAll(
        '[data-agent-mention-app-icon="true"] img, [data-agent-mention-app-icon="true"] svg'
      )
    ).toHaveLength(1);

    fireEvent.error(image!);

    expect(
      mention?.querySelector('[data-agent-mention-app-icon="true"] img')
    ).not.toBeInTheDocument();
    expect(
      mention?.querySelector('[data-agent-mention-fallback-icon="true"]')
    ).toBeInTheDocument();
    expect(
      mention?.querySelectorAll(
        '[data-agent-mention-app-icon="true"] img, [data-agent-mention-app-icon="true"] svg'
      )
    ).toHaveLength(1);
    expect(mention).toHaveTextContent("AI Media Canvas");
    expect(screen.queryByText(/mention:\/\/workspace-app/)).toBeNull();
  });

  it("renders queued local file links with spaced paths as file tokens", () => {
    const { container } = render(
      <AgentQueuedPromptPanel
        queuedPrompts={[
          textQueuedPrompt(
            "queued-1",
            "[@user](/Users/Sun/Documents/tutti/emoji 你好/user/) [@auth_api.py](/Users/Sun/Documents/tutti/emoji 你好/auth_api.py)"
          )
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={vi.fn()}
      />
    );

    const mentions = container.querySelectorAll(
      '[data-agent-file-mention="true"]'
    );
    expect(mentions).toHaveLength(2);
    expect(mentions[0]).toHaveClass("tsh-agent-object-token--file");
    expect(mentions[0]).toHaveTextContent("user");
    expect(mentions[1]).toHaveTextContent("auth_api.py");
    expect(screen.queryByText(/\]\(\/Users\/Sun\/Documents/)).toBeNull();
  });

  it("renders queued image prompt previews and routes edit", async () => {
    const onEditQueuedPrompt = vi.fn();
    const { container } = render(
      <AgentQueuedPromptPanel
        queuedPrompts={[
          {
            id: "queued-1",
            content: [
              { type: "text", text: "describe this" },
              {
                type: "image",
                mimeType: "image/png",
                data: "aW1hZ2U=",
                name: "panel.png"
              }
            ],
            createdAtUnixMs: 1
          }
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={onEditQueuedPrompt}
      />
    );

    expect(screen.getByText("describe this")).toBeInTheDocument();
    expect(
      container.querySelector(".agent-gui-node__composer-queued-prompt-image")
    ).toHaveAttribute("src", "data:image/png;base64,aW1hZ2U=");

    const moreButton = screen.getByRole("button", { name: "More" });
    fireEvent.pointerDown(moreButton, { button: 0, ctrlKey: false });
    fireEvent.click(moreButton);
    const editItem = await screen.findByRole("menuitem", { name: "Edit" });
    expect(editItem).not.toHaveAttribute("aria-disabled", "true");
    fireEvent.click(editItem);
    expect(onEditQueuedPrompt).toHaveBeenCalledWith("queued-1");
  });

  it("loads path-backed queued image prompt previews without rendering undefined data urls", async () => {
    const readPromptAsset = vi.fn(async () => ({
      data: "c3RhZ2VkLWltYWdl",
      mimeType: "image/png",
      name: "staged.png",
      path: "/agent-prompt-assets/staged.png"
    }));
    setConversationServicesForTests({
      attachments: {
        readPromptAsset
      }
    } as unknown as ConversationServices);

    const { container } = render(
      <AgentQueuedPromptPanel
        workspaceId="workspace-1"
        agentSessionId="session-1"
        queuedPrompts={[
          {
            id: "queued-1",
            content: [
              {
                type: "image",
                mimeType: "image/png",
                path: "/agent-prompt-assets/staged.png",
                name: "staged.png"
              }
            ],
            createdAtUnixMs: 1
          }
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={vi.fn()}
      />
    );

    expect(container.innerHTML).not.toContain("base64,undefined");

    await waitFor(() => {
      expect(readPromptAsset).toHaveBeenCalledWith({
        workspaceId: "workspace-1",
        agentSessionId: "session-1",
        mimeType: "image/png",
        name: "staged.png",
        path: "/agent-prompt-assets/staged.png"
      });
      expect(
        container.querySelector(".agent-gui-node__composer-queued-prompt-image")
      ).toHaveAttribute("src", "data:image/png;base64,c3RhZ2VkLWltYWdl");
    });
  });

  it("loads attachment-backed queued image prompt previews", async () => {
    const readSessionAttachment = vi.fn(async () => ({
      data: "YXR0YWNobWVudC1pbWFnZQ==",
      mimeType: "image/png",
      name: "attached.png"
    }));
    setConversationServicesForTests({
      attachments: {
        readSessionAttachment
      }
    } as unknown as ConversationServices);

    const { container } = render(
      <AgentQueuedPromptPanel
        workspaceId="workspace-1"
        agentSessionId="session-1"
        queuedPrompts={[
          {
            id: "queued-1",
            content: [
              {
                type: "image",
                mimeType: "image/png",
                attachmentId: "attachment-1",
                name: "attached.png"
              }
            ],
            createdAtUnixMs: 1
          }
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={vi.fn()}
      />
    );

    expect(container.innerHTML).not.toContain("base64,undefined");

    await waitFor(() => {
      expect(readSessionAttachment).toHaveBeenCalledWith({
        workspaceId: "workspace-1",
        agentSessionId: "session-1",
        attachmentId: "attachment-1"
      });
      expect(
        container.querySelector(".agent-gui-node__composer-queued-prompt-image")
      ).toHaveAttribute(
        "src",
        "data:image/png;base64,YXR0YWNobWVudC1pbWFnZQ=="
      );
    });
  });

  it("drops stale queued image reads after workspace and asset identity change", async () => {
    const resolvers = new Map<
      string,
      (asset: { data: string; mimeType: string }) => void
    >();
    const readPromptAsset = vi.fn(
      (input: { path: string; workspaceId: string }) =>
        new Promise<{ data: string; mimeType: string }>((resolve) => {
          resolvers.set(`${input.workspaceId}:${input.path}`, resolve);
        })
    );
    setConversationServicesForTests({
      attachments: {
        readPromptAsset
      }
    } as unknown as ConversationServices);
    const createPanel = (workspaceId: string, path: string) => (
      <AgentQueuedPromptPanel
        agentSessionId="session-1"
        drainingQueuedPromptId={null}
        labels={labels}
        queuedPrompts={[
          {
            id: "queued-1",
            content: [
              {
                type: "image",
                mimeType: "image/png",
                name: "queued.png",
                path
              }
            ],
            createdAtUnixMs: 1
          }
        ]}
        workspaceId={workspaceId}
        onEditQueuedPrompt={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onSendQueuedPromptNext={vi.fn()}
      />
    );
    const { container, rerender } = render(
      createPanel("workspace-1", "/assets/old.png")
    );
    await waitFor(() => expect(readPromptAsset).toHaveBeenCalledTimes(1));

    rerender(createPanel("workspace-2", "/assets/new.png"));
    await waitFor(() => expect(readPromptAsset).toHaveBeenCalledTimes(2));
    resolvers.get("workspace-1:/assets/old.png")?.({
      data: "b2xk",
      mimeType: "image/png"
    });
    await Promise.resolve();
    expect(container.innerHTML).not.toContain("base64,b2xk");

    resolvers.get("workspace-2:/assets/new.png")?.({
      data: "bmV3",
      mimeType: "image/png"
    });
    await waitFor(() =>
      expect(
        container.querySelector(".agent-gui-node__composer-queued-prompt-image")
      ).toHaveAttribute("src", "data:image/png;base64,bmV3")
    );
  });

  it("allows queued image prompt previews to zoom", () => {
    render(
      <AgentQueuedPromptPanel
        queuedPrompts={[
          {
            id: "queued-1",
            content: [
              {
                type: "image",
                mimeType: "image/png",
                data: "aW1hZ2U=",
                name: "panel.png"
              }
            ],
            createdAtUnixMs: 1
          }
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={vi.fn()}
      />
    );

    expect(
      screen.getByRole("button", { name: /Zoom image/ })
    ).toBeInTheDocument();
  });

  it("does not hydrate or fetch queued HTTPS image URLs in the owner", () => {
    const readSessionAttachment = vi.fn();
    const url = "https://bucket.example/image.webp?token=secret";
    setConversationServicesForTests({
      attachments: { readSessionAttachment }
    } as never);
    const { container } = render(
      <AgentQueuedPromptPanel
        queuedPrompts={[
          {
            id: "queued-url",
            content: [
              {
                type: "image",
                mimeType: "image/webp",
                url,
                attachmentId: "remote-image",
                name: "image.webp"
              }
            ],
            createdAtUnixMs: 1
          }
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={vi.fn()}
        agentSessionId="session-1"
        workspaceId="workspace-1"
      />
    );

    expect(
      container.querySelector(".agent-gui-node__composer-queued-prompt-image")
    ).not.toBeInTheDocument();
    expect(readSessionAttachment).not.toHaveBeenCalled();
  });

  it("emits queued mention link clicks without toggling the queue panel", () => {
    const onLinkClick = vi.fn();
    const { container } = render(
      <AgentQueuedPromptPanel
        queuedPrompts={[
          textQueuedPrompt(
            "queued-1",
            "[@2046494774160003072 & Claude Code Claude Code](mention://agent-session/session-1?workspaceId=room-1)"
          )
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={vi.fn()}
        onLinkClick={onLinkClick}
      />
    );

    const panel = container.querySelector("[data-expanded]");
    const mention = container.querySelector('[data-agent-file-mention="true"]');

    expect(panel).toHaveAttribute("data-expanded", "false");
    fireEvent.click(mention as Element);

    expect(onLinkClick).toHaveBeenCalledWith(
      "mention://agent-session/session-1?workspaceId=room-1"
    );
    expect(panel).toHaveAttribute("data-expanded", "false");
  });

  it("routes the edit action for a queued prompt", async () => {
    const onEditQueuedPrompt = vi.fn();
    render(
      <AgentQueuedPromptPanel
        queuedPrompts={[
          textQueuedPrompt("queued-1", "first queued prompt"),
          textQueuedPrompt("queued-2", "second queued prompt", 2)
        ]}
        drainingQueuedPromptId={null}
        labels={labels}
        onSendQueuedPromptNext={vi.fn()}
        onRemoveQueuedPrompt={vi.fn()}
        onEditQueuedPrompt={onEditQueuedPrompt}
      />
    );

    const panel = screen
      .getByTestId("agent-gui-composer-queued-prompt-queued-1")
      .closest("[data-expanded]");
    expect(panel).toHaveAttribute("data-expanded", "false");

    const moreButtons = screen.getAllByRole("button", { name: "More" });
    fireEvent.pointerDown(moreButtons[1]!, { button: 0, ctrlKey: false });
    fireEvent.click(moreButtons[1]!);
    expect(panel).toHaveAttribute("data-expanded", "false");
    const editItem = await screen.findByRole("menuitem", { name: "Edit" });
    fireEvent.pointerDown(editItem, { button: 0, ctrlKey: false });
    fireEvent.click(editItem);

    expect(onEditQueuedPrompt).toHaveBeenCalledWith("queued-2");
    expect(onEditQueuedPrompt).toHaveBeenCalledTimes(1);
  });
});
