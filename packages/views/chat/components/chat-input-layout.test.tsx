import { forwardRef, useImperativeHandle } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enChat from "../../locales/en/chat.json";

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat } };

vi.mock("../../editor", () => ({
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  FileDropOverlay: () => null,
  ContentEditor: forwardRef(function MockContentEditor(
    props: { placeholder?: string },
    ref: React.Ref<unknown>,
  ) {
    useImperativeHandle(ref, () => ({
      getMarkdown: () => "",
      clearContent: () => {},
      blur: () => {},
      focus: () => {},
      hasActiveUploads: () => false,
    }));

    return <textarea data-testid="editor" placeholder={props.placeholder} />;
  }),
}));

vi.mock("@multica/core/chat", () => {
  const state = {
    activeSessionId: null as string | null,
    selectedAgentId: "agent-1",
    inputDrafts: {} as Record<string, string>,
    inputDraftAttachments: {} as Record<string, unknown[]>,
    setInputDraft: vi.fn(),
    setInputDraftAttachments: vi.fn(),
    addInputDraftAttachment: vi.fn(),
    clearInputDraft: vi.fn(),
  };
  return {
    newSessionDraftKey: (agentId: string | null) => `__draft_new__:${agentId ?? ""}`,
    useChatStore: Object.assign(
      (selector?: (s: typeof state) => unknown) => (selector ? selector(state) : state),
      { getState: () => state },
    ),
  };
});

import { ChatInput } from "./chat-input";

describe("ChatInput layout", () => {
  it("uses a more compact shell on the full-page obitaPlus layout", () => {
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatInput onSend={vi.fn()} layout="page" agentName="Multica" />
      </I18nProvider>,
    );

    const shell = screen.getAllByTestId("editor").at(-1)?.parentElement?.parentElement;

    expect(shell).not.toBeNull();
    expect(shell?.className).toContain("mx-auto");
    expect(shell?.className).toContain("max-w-4xl");
    expect(shell?.className).not.toContain("max-w-none");
    expect(shell?.className).toContain("min-h-11");
    expect(shell?.className).toContain("max-h-28");
    expect(shell?.className).toContain("pb-7");
  });

  it("hides the bottom-left adornment in full-page no-agent mode", () => {
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatInput
          onSend={vi.fn()}
          layout="page"
          noAgent
          agentName="Multica"
          leftAdornment={<span data-testid="left-adornment">No agents</span>}
        />
      </I18nProvider>,
    );

    const shell = screen.getAllByTestId("editor").at(-1)?.parentElement?.parentElement;

    expect(screen.queryByTestId("left-adornment")).toBeNull();
    expect(shell?.className).toContain("mx-auto");
    expect(shell?.className).toContain("max-w-4xl");
    expect(shell?.className).not.toContain("max-w-none");
    expect(shell?.className).toContain("h-[120px]");
    expect(shell?.className).toContain("min-h-[120px]");
    expect(shell?.className).toContain("max-h-[120px]");
    expect(shell?.className).not.toContain("pb-7");
    expect(shell?.className).not.toContain("pb-4");
  });
});
