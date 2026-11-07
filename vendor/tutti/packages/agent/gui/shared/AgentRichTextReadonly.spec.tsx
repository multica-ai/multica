import "@testing-library/jest-dom/vitest";
import { fireEvent, render, waitFor } from "@testing-library/react";
import { renderToReactElement } from "@tiptap/static-renderer";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AgentRichTextReadonly } from "./AgentRichTextReadonly";
import { createAgentCapabilityTokenExtension } from "../agent-gui/agentGuiNode/agentRichText/agentCapabilityTokenExtension";
import { createAgentRichTextReadonlyExtensions } from "../agent-gui/agentGuiNode/agentRichText/agentRichTextExtensions";
import { createAgentSkillTokenExtension } from "../agent-gui/agentGuiNode/agentRichText/agentSkillTokenExtension";
import {
  registerAgentCustomMentionKind,
  resetAgentCustomMentionKindsForTests
} from "./agentCustomMentionKinds";
import {
  parsedDocumentCacheStatsForTests,
  resetParsedDocumentCacheForTests
} from "./parsedDocumentCache";

afterEach(() => {
  resetAgentCustomMentionKindsForTests();
  resetParsedDocumentCacheForTests();
  vi.restoreAllMocks();
});

describe("AgentRichTextReadonly", () => {
  it("renders a submitted leading quote as quoted data above the question", () => {
    const { container } = render(<AgentRichTextReadonly value={"> First line\n> <script>untrusted text</script>\n\nWhy does this happen?"} />);
    expect(container.querySelector("blockquote")).toHaveTextContent("First line <script>untrusted text</script>");
    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector(".ProseMirror")).toHaveTextContent("Why does this happen?");
  });
  it("reuses the parsed document after a historical message remounts", () => {
    resetParsedDocumentCacheForTests();
    const first = render(
      <AgentRichTextReadonly
        value="Historical user message"
        documentCacheKey="message-1:version-1"
      />
    );
    first.unmount();

    render(
      <AgentRichTextReadonly
        value="Historical user message"
        documentCacheKey="message-1:version-1"
      />
    );

    expect(parsedDocumentCacheStatsForTests()).toMatchObject({
      entries: 1,
      hits: 1,
      misses: 1
    });
  });

  it("mounts a transcript window without creating editable views or reading DOM selection", () => {
    const getSelection = vi.spyOn(document, "getSelection");
    const { container } = render(
      <>
        {Array.from({ length: 30 }, (_, index) => (
          <AgentRichTextReadonly
            key={index}
            value={`Historical user message ${index}`}
          />
        ))}
      </>
    );

    expect(container.querySelectorAll(".ProseMirror")).toHaveLength(30);
    expect(container.querySelector("[contenteditable]")).toBeNull();
    expect(getSelection).not.toHaveBeenCalled();
  });

  it("delegates mention clicks without an EditorView", () => {
    const onLinkClick = vi.fn();
    const href = "mention://workspace-app/weather?workspaceId=workspace-1";
    const { container } = render(
      <AgentRichTextReadonly
        value={`Run [@Weather](${href})`}
        onLinkClick={onLinkClick}
      />
    );

    const mention = container.querySelector(
      '[data-agent-mention-kind="workspace-app"]'
    );
    expect(mention).not.toBeNull();
    fireEvent.click(mention!);
    expect(onLinkClick).toHaveBeenCalledOnce();
    expect(onLinkClick).toHaveBeenCalledWith(href);
  });

  it("delegates clicks from inside a mention pill", () => {
    const onLinkClick = vi.fn();
    const href = "mention://workspace-app/weather?workspaceId=workspace-1";
    const { getByText } = render(
      <AgentRichTextReadonly
        value={`Run [@Weather](${href})`}
        onLinkClick={onLinkClick}
      />
    );

    fireEvent.click(getByText("Weather"));

    expect(onLinkClick).toHaveBeenCalledOnce();
    expect(onLinkClick).toHaveBeenCalledWith(href);
  });

  it("renders historical browser-element hrefs whose entity id contains an unescaped colon", async () => {
    registerAgentCustomMentionKind({
      kind: "browser-element",
      present: (mention) => ({
        name: `<${mention.scope?.tag ?? mention.label}>`
      }),
      renderChip: ({ name }) => (
        <span data-agent-browser-element-chip="true">{name}</span>
      )
    });
    const first =
      "[@<a>](mention://browser-element/browser-element:6e42f79e-8c12-4b91-833e-279a8542b71d?path=%2Ftmp%2Fa.txt&tag=a&workspaceId=workspace-1)";
    const second =
      "[@<a>](mention://browser-element/browser-element:a665f014-eec4-4f0f-aeac-5a88d8acb69e?path=%2Ftmp%2Fb.txt&tag=a&workspaceId=workspace-1)";
    const { container } = render(
      <AgentRichTextReadonly value={`${first}\n${second}这里面内容是什么`} />
    );

    await waitFor(() =>
      expect(
        container.querySelectorAll("[data-agent-browser-element-chip=true]")
      ).toHaveLength(2)
    );
    expect(container.textContent?.replaceAll("\u200b", "")).toContain(
      "<a><a>这里面内容是什么"
    );
    expect(container).not.toHaveTextContent("mention://browser-element");
  });

  it("renders browser element mentions as registered chips beside the concrete prompt", async () => {
    registerAgentCustomMentionKind({
      kind: "browser-element",
      present: (mention) => ({
        name: `<${mention.scope?.tag ?? mention.label}>`
      }),
      renderChip: ({ name }) => (
        <span data-agent-browser-element-chip="true">{name}</span>
      )
    });
    const firstHref =
      "mention://browser-element/browser-element%3A1?path=%2Ftmp%2Fa.txt&tag=a&workspaceId=workspace-1";
    const secondHref =
      "mention://browser-element/browser-element%3A2?path=%2Ftmp%2Fdiv.txt&tag=div&workspaceId=workspace-1";
    const { container } = render(
      <AgentRichTextReadonly
        value={`[@<a>](${firstHref}) [@<div>](${secondHref}) 这里说的什么`}
      />
    );

    await waitFor(() =>
      expect(
        container.querySelectorAll("[data-agent-browser-element-chip=true]")
      ).toHaveLength(2)
    );

    const mentions = container.querySelectorAll(
      '[data-agent-custom-mention="true"]'
    );
    expect(mentions).toHaveLength(2);
    expect(mentions[0]).toHaveAttribute("data-agent-mention-href", firstHref);
    expect(mentions[1]).toHaveAttribute("data-agent-mention-href", secondHref);
    expect(container).toHaveTextContent("<a> <div> 这里说的什么");
    expect(container).not.toHaveTextContent("@<a>");
  });

  it("hydrates workspace app mention icons without putting icon data in the href", async () => {
    const iconUrl = "data:image/png;base64,weather";
    const { container } = render(
      <AgentRichTextReadonly
        value={
          "Run [@Weather](mention://workspace-app/weather?workspaceId=workspace-1)"
        }
        workspaceAppIcons={[
          {
            appId: "weather",
            workspaceId: "workspace-1",
            iconUrl
          }
        ]}
      />
    );

    await waitFor(() =>
      expect(
        container.querySelector('[data-agent-mention-kind="workspace-app"]')
      ).not.toBeNull()
    );

    const mention = container.querySelector(
      '[data-agent-mention-kind="workspace-app"]'
    );
    expect(mention).toHaveTextContent("Weather");
    expect(mention).toHaveAttribute(
      "data-agent-mention-href",
      "mention://workspace-app/weather?workspaceId=workspace-1"
    );
    expect(mention).toHaveAttribute("data-agent-mention-icon-url", iconUrl);
    expect(mention?.querySelector("img")).toHaveAttribute("src", iconUrl);
  });

  it("hydrates session mention icons from Agent Target presentation metadata", async () => {
    const iconUrl = "data:image/png;base64,gemini";
    const href =
      "mention://agent-session/session-1?agentTargetId=extension%3Agemini&workspaceId=workspace-1";
    const { container } = render(
      <AgentRichTextReadonly
        value={`Review [@Gemini session](${href})`}
        agentTargets={[
          {
            agentTargetId: "extension:gemini",
            iconUrl,
            name: "Gemini CLI",
            provider: "acp:gemini",
            workspaceId: "workspace-1"
          }
        ]}
      />
    );

    await waitFor(() =>
      expect(
        container.querySelector('[data-agent-mention-kind="session"] img')
      ).toHaveAttribute("src", iconUrl)
    );
    const mention = container.querySelector(
      '[data-agent-mention-kind="session"]'
    );
    expect(mention).toHaveTextContent("Gemini session");
    expect(mention).toHaveAttribute("data-agent-mention-href", href);
  });

  it("does not override mention wrapper width in readonly messages", async () => {
    const { container } = render(
      <AgentRichTextReadonly
        value={
          "Run [@Long Workspace App Name](mention://workspace-app/weather?workspaceId=workspace-1)"
        }
      />
    );

    await waitFor(() =>
      expect(
        container.querySelector('[data-agent-mention-kind="workspace-app"]')
      ).not.toBeNull()
    );

    const editor = container.querySelector(".ProseMirror");
    expect(editor).not.toHaveClass(
      "[&_[data-agent-file-mention=true]]:max-w-full"
    );
  });

  it("marks readonly messages that only contain one mention", async () => {
    const { container, rerender } = render(
      <AgentRichTextReadonly
        value={
          "[@Long Workspace App Name](mention://workspace-app/weather?workspaceId=workspace-1)"
        }
      />
    );

    await waitFor(() =>
      expect(
        container.querySelector('[data-agent-mention-kind="workspace-app"]')
      ).not.toBeNull()
    );

    expect(container.firstElementChild).toHaveAttribute(
      "data-agent-mention-only",
      "true"
    );

    rerender(
      <AgentRichTextReadonly
        value={
          "Run [@Long Workspace App Name](mention://workspace-app/weather?workspaceId=workspace-1)"
        }
      />
    );

    expect(container.firstElementChild).not.toHaveAttribute(
      "data-agent-mention-only"
    );
  });

  it("hydrates app workspace-reference mention icons from workspace app icons", async () => {
    const iconUrl = "data:image/png;base64,canvas";
    const { container } = render(
      <AgentRichTextReadonly
        value={
          "Use [@AI Canvas](mention://workspace-reference/ai-canvas?source=app&workspaceId=workspace-1)"
        }
        workspaceAppIcons={[
          {
            appId: "ai-canvas",
            workspaceId: "workspace-1",
            iconUrl
          }
        ]}
      />
    );

    await waitFor(() =>
      expect(
        container.querySelector(
          '[data-agent-mention-kind="workspace-reference"]'
        )
      ).not.toBeNull()
    );

    const mention = container.querySelector(
      '[data-agent-mention-kind="workspace-reference"]'
    );
    expect(mention).toHaveTextContent("AI Canvas");
    expect(mention).toHaveAttribute(
      "data-agent-mention-href",
      "mention://workspace-reference/ai-canvas?source=app&workspaceId=workspace-1"
    );
    expect(mention).toHaveAttribute("data-agent-mention-icon-url", iconUrl);
    expect(mention?.querySelector("img")).toHaveAttribute("src", iconUrl);
  });

  it("renders workspace app factory markdown as a mention token", async () => {
    const { container } = render(
      <AgentRichTextReadonly
        value={"[@Create App](mention://workspace-app-factory/create)"}
      />
    );

    await waitFor(() =>
      expect(
        container.querySelector(
          '[data-agent-mention-kind="workspace-app-factory"]'
        )
      ).not.toBeNull()
    );

    const mention = container.querySelector(
      '[data-agent-mention-kind="workspace-app-factory"]'
    );
    expect(mention).toHaveTextContent("Create App");
    expect(mention).toHaveAttribute(
      "data-agent-mention-href",
      "mention://workspace-app-factory/create"
    );
    expect(container).not.toHaveTextContent(
      "mention://workspace-app-factory/create"
    );
  });

  it("renders known skill triggers as skill tokens", async () => {
    const { container } = render(
      <AgentRichTextReadonly
        value="Use /caveman and /compact"
        availableSkills={[
          {
            name: "caveman",
            trigger: "$caveman",
            sourceKind: "personal"
          }
        ]}
      />
    );

    await waitFor(() =>
      expect(
        container.querySelector('[data-agent-skill-token="true"]')
      ).not.toBeNull()
    );

    const skillToken = container.querySelector(
      '[data-agent-skill-token="true"]'
    );
    expect(skillToken).toHaveTextContent("/caveman");
    expect(skillToken).toHaveAttribute("data-agent-skill-trigger", "/caveman");
    expect(container).toHaveTextContent("/compact");
  });

  it("renders readonly agent tokens without invalid React DOM properties", () => {
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => {});
    const extensions = createAgentRichTextReadonlyExtensions();

    for (const token of [
      {
        attrs: {
          capability: "browser",
          label: "Browser",
          name: "browser",
          trigger: "/browser"
        },
        dataAttribute: "data-agent-capability-token",
        extension: createAgentCapabilityTokenExtension(),
        type: "agentCapabilityToken"
      },
      {
        attrs: {
          label: "Caveman",
          name: "caveman",
          trigger: "/caveman"
        },
        dataAttribute: "data-agent-skill-token",
        extension: createAgentSkillTokenExtension(),
        type: "agentSkillToken"
      }
    ]) {
      const renderHTML = token.extension.config.renderHTML as
        | ((props: { HTMLAttributes: Record<string, unknown> }) => unknown)
        | undefined;
      const outputSpec = renderHTML?.({ HTMLAttributes: token.attrs });
      const outputAttributes = Array.isArray(outputSpec)
        ? (outputSpec[1] as Record<string, unknown>)
        : {};

      expect(outputAttributes.contentEditable).toBe("false");
      expect(outputAttributes.contenteditable).toBeUndefined();

      const content = renderToReactElement({
        content: {
          content: [
            {
              content: [{ attrs: token.attrs, type: token.type }],
              type: "paragraph"
            }
          ],
          type: "doc"
        },
        extensions
      });

      const rendered = render(content);

      expect(
        rendered.container.querySelector(`[${token.dataAttribute}="true"]`)
      ).toHaveAttribute("contenteditable", "false");
      rendered.unmount();
    }

    expect(consoleError.mock.calls.flat().join(" ")).not.toContain(
      "Invalid DOM property `contenteditable`"
    );
  });
});
