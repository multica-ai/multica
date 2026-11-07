// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ComposerDraftAttachments } from "./ComposerDraftAttachments";

describe("ComposerDraftAttachments", () => {
  it("groups mixed attachment types into one measured row", () => {
    render(
      <ComposerDraftAttachments
        draftImages={[
          {
            id: "image-1",
            name: "selection.png",
            mimeType: "image/png",
            previewUrl: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB"
          }
        ]}
        draftLargeTexts={[]}
        draftQuotes={[
          { type: "quote", id: "quote-1", text: "First selection" }
        ]}
        removeLabel="Remove reference"
        onRemoveImage={vi.fn()}
        onRemoveLargeText={vi.fn()}
        onExpandLargeText={vi.fn()}
        onRemoveQuotes={vi.fn()}
      />
    );

    const wrapper = screen.getByTestId("agent-gui-composer-attachment-drafts");
    expect(wrapper.children).toHaveLength(2);
    expect(wrapper).toContainElement(
      screen.getByTestId("agent-gui-composer-quote-drafts")
    );
    expect(wrapper).toContainElement(
      screen.getByTestId("agent-gui-composer-image-drafts")
    );
  });

  it("shows the selected text and source inline with a separate remove action", () => {
    const onRemoveQuotes = vi.fn();
    render(
      <ComposerDraftAttachments
        draftImages={[]}
        draftLargeTexts={[]}
        draftQuotes={[{ type: "quote", id: "quote-1", text: "First selection", sourceName: "Claude Code" }]}
        removeLabel="Remove reference"
        onRemoveImage={vi.fn()}
        onRemoveLargeText={vi.fn()}
        onExpandLargeText={vi.fn()}
        onRemoveQuotes={onRemoveQuotes}
      />
    );
    expect(screen.getByTestId("agent-gui-composer-quote-drafts")).toHaveTextContent("Quote · Claude Code");
    expect(screen.getByTestId("agent-gui-composer-quote-text")).toHaveTextContent("First selection");
    expect(screen.queryByRole("button", { name: "Expand quote" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Remove quote" }));
    expect(onRemoveQuotes).toHaveBeenCalledOnce();
  });
});
