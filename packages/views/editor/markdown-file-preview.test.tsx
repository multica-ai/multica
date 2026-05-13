import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, waitFor } from "@testing-library/react";
import { MarkdownFilePreviewButton } from "./markdown-file-preview";

const { previewAttachmentMarkdownMock } = vi.hoisted(() => ({
  previewAttachmentMarkdownMock: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    previewAttachmentMarkdown: previewAttachmentMarkdownMock,
  },
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        file_card: {
          preview: "Preview test.md",
          preview_loading: "Loading preview…",
          preview_failed: "Preview failed",
          close_preview: "Close preview",
          enter_full_screen: "Enter full screen",
          exit_full_screen: "Exit full screen",
        },
      }),
  }),
}));

describe("MarkdownFilePreviewButton", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("uses the same dark backdrop as the official attachment preview modal", async () => {
    previewAttachmentMarkdownMock.mockResolvedValueOnce("# hello");

    render(
      <MarkdownFilePreviewButton
        href="https://cdn.example.test/test.md"
        filename="test.md"
        renderContent={(content) => <div>{content}</div>}
      />,
    );

    fireEvent.click(document.querySelector("button")!);

    await waitFor(() => {
      expect(previewAttachmentMarkdownMock).toHaveBeenCalledWith(
        "https://cdn.example.test/test.md",
      );
    });

    const overlay = document.querySelector('[data-slot="dialog-overlay"]');
    expect(overlay).toBeTruthy();
    expect(overlay?.className).toContain("bg-black/80");
  });
});
