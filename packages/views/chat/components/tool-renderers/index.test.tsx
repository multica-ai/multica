import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ChatTimelineItem } from "@multica/core/chat";
import enChat from "../../../locales/en/chat.json";
import { ToolCard, toolRenderers } from "./index";

const RESOURCES = { en: { chat: enChat } };

function renderCard(item: ChatTimelineItem) {
  return render(
    <I18nProvider locale="en" resources={RESOURCES}>
      <ToolCard item={item} />
    </I18nProvider>,
  );
}

function toolUse(overrides: Partial<ChatTimelineItem> = {}): ChatTimelineItem {
  return { seq: 1, type: "tool_use", tool: "Bash", input: { command: "ls" }, status: "done", ...overrides };
}

describe("ToolCard / tool renderer dispatch", () => {
  it("renders the tool name and input summary in the header (zero-click)", () => {
    renderCard(toolUse({ input: { command: "go test ./..." } }));
    expect(screen.getByText("Bash")).toBeInTheDocument();
    expect(screen.getByText("go test ./...")).toBeInTheDocument();
  });

  it("shows an Error chip with text (not color alone) for a failed tool", () => {
    renderCard(toolUse({ status: "error", is_error: true, output: "boom" }));
    expect(screen.getByText("Error")).toBeInTheDocument();
  });

  it("shows a Running indicator with text for an unresolved tool", () => {
    renderCard(toolUse({ status: "running", output: undefined }));
    expect(screen.getByText("Running")).toBeInTheDocument();
  });

  it("routes an unmapped tool to the generic fallback body", () => {
    // A tool with no registered renderer still renders its raw output.
    renderCard(toolUse({ tool: "SomeUnmappedTool", output: "generic output here" }));
    expect(screen.getByText("SomeUnmappedTool")).toBeInTheDocument();
    expect(screen.getByText(/generic output here/)).toBeInTheDocument();
  });

  it("routes a registered tool to its purpose-built renderer", () => {
    const marker = "purpose-built-marker";
    const original = toolRenderers.faketool;
    toolRenderers.faketool = () => <span>{marker}</span>;
    try {
      renderCard(toolUse({ tool: "FakeTool", output: "raw" }));
      expect(screen.getByText(marker)).toBeInTheDocument();
      // The generic raw output must NOT render when a custom renderer wins.
      expect(screen.queryByText("raw")).not.toBeInTheDocument();
    } finally {
      if (original) toolRenderers.faketool = original;
      else delete toolRenderers.faketool;
    }
  });
});
