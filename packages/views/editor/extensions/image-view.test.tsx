import React from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { NodeViewProps } from "@tiptap/react";
import { ImageView } from "./image-view";

const mobile = vi.hoisted(() => ({ value: true }));

vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => mobile.value,
}));
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFlagValue: () => false,
}));
vi.mock("@multica/cerebro-attachments/views", () => ({
  useGalleryImage: () => ({ enabled: false, open: vi.fn(), ref: vi.fn() }),
}));
vi.mock("@multica/cerebro-ui", () => ({
  AttachmentChip: () => null,
  ZoomableImage: () => null,
}));
vi.mock("../attachment-download-context", () => ({
  useAttachmentDownloadResolver: () => ({ openByUrl: vi.fn() }),
}));
vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (select: (translations: unknown) => string) =>
      select({
        image: {
          view: "View image",
          download: "Download",
          copy_link: "Copy link",
          size: "Size",
          small: "Small",
          medium: "Medium",
          full_width: "Full width",
          align_left: "Align left",
          align_center: "Align center",
          align_right: "Align right",
          move_to_bottom: "Move to bottom",
          delete: "Delete",
          link_copied: "Link copied",
          copy_link_failed: "Failed to copy link",
        },
      }),
  }),
}));
vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));
vi.mock("@tiptap/react", async (importOriginal) => {
  const original = await importOriginal<typeof import("@tiptap/react")>();
  return {
    ...original,
    NodeViewWrapper: React.forwardRef<HTMLElement, React.HTMLAttributes<HTMLElement> & { as?: string }>(
      function MockNodeViewWrapper({ as = "div", children, ...props }, ref) {
        return React.createElement(as, { ...props, ref }, children);
      },
    ),
  };
});

function renderImageView(selected = true) {
  const chain = {
    setNodeSelection: vi.fn(),
    setImageWidthPct: vi.fn(),
    setImageAlign: vi.fn(),
    run: vi.fn(() => true),
  };
  chain.setNodeSelection.mockReturnValue(chain);
  chain.setImageWidthPct.mockReturnValue(chain);
  chain.setImageAlign.mockReturnValue(chain);

  const props = {
    node: {
      attrs: {
        src: "https://cdn.example.com/image.png",
        alt: "Preview",
        title: null,
        uploading: false,
        widthPct: null,
        align: null,
        placement: "inline",
      },
    },
    editor: {
      isEditable: true,
      chain: vi.fn(() => chain),
    },
    selected,
    getPos: () => 0,
  } as unknown as NodeViewProps;

  render(<ImageView {...props} />);
  return { chain };
}

beforeEach(() => {
  vi.useFakeTimers();
  mobile.value = true;
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("ImageView phone controls", () => {
  it("uses presets instead of drag handles and stays inside a 390 px pane", () => {
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 390 });
    const { chain } = renderImageView(false);
    const figure = screen.getByRole("figure");

    expect(document.querySelector(".image-resize-handle")).not.toBeInTheDocument();
    expect(figure).toHaveStyle({ maxWidth: "100%" });

    fireEvent.touchStart(figure, {
      touches: [{ clientX: 195, clientY: 120 }],
    });
    act(() => vi.advanceTimersByTime(500));
    fireEvent.click(screen.getByRole("menuitem", { name: "Small" }));

    expect(chain.setNodeSelection).toHaveBeenCalledWith(0);
    expect(chain.setImageWidthPct).toHaveBeenCalledWith(33);
    expect(chain.run).toHaveBeenCalled();
  });

  it("keeps corner resize handles on desktop", () => {
    mobile.value = false;
    renderImageView();
    expect(document.querySelectorAll(".image-resize-handle")).toHaveLength(4);
  });
});
