import { describe, expect, it } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ChatTimelineItem } from "@multica/core/chat";
import enChat from "../../../locales/en/chat.json";
import { BashToolBody } from "./bash";

const RESOURCES = { en: { chat: enChat } };

function renderBash(item: ChatTimelineItem) {
  return render(
    <I18nProvider locale="en" resources={RESOURCES}>
      <BashToolBody item={item} />
    </I18nProvider>,
  );
}

function bashItem(overrides: Partial<ChatTimelineItem> = {}): ChatTimelineItem {
  return { seq: 1, type: "tool_use", tool: "Bash", input: { command: "ls" }, status: "done", ...overrides };
}

describe("BashToolBody", () => {
  it("shows the last output lines without expanding (zero-click preview)", () => {
    renderBash(bashItem({ output: "line1\nline2\nline3\nline4" }));
    // Last 3 lines are visible in the collapsed preview.
    expect(screen.getByText(/line2\s+line3\s+line4/)).toBeInTheDocument();
  });

  it("renders the failure preview in the error color", () => {
    renderBash(bashItem({ status: "error", is_error: true, output: "command not found" }));
    const pre = screen.getByText("command not found");
    expect(pre.className).toContain("text-destructive");
  });

  it("exposes a focusable copy button when expanded", () => {
    renderBash(bashItem({ output: "some output" }));
    // Expand the pane.
    act(() => {
      screen.getByText("some output").closest("button")?.click();
    });
    const copy = screen.getByLabelText("Copy output");
    expect(copy).toBeInTheDocument();
    expect(copy.tagName).toBe("BUTTON");
  });

  it("renders nothing when there is no output", () => {
    const { container } = renderBash(bashItem({ output: undefined }));
    expect(container).toBeEmptyDOMElement();
  });
});
