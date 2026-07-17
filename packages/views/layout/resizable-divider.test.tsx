import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("react-resizable-panels", () => ({
  Group: ({ children, ...props }: React.ComponentProps<"div">) => (
    <div {...props}>{children}</div>
  ),
  Panel: ({ children, ...props }: React.ComponentProps<"div">) => (
    <div {...props}>{children}</div>
  ),
  Separator: ({ children, ...props }: React.ComponentProps<"div">) => (
    <div {...props}>{children}</div>
  ),
}));

import { ResizableHandle } from "@multica/ui/components/ui/resizable";
import { resizeHandleVariants } from "@multica/ui/components/ui/resize-handle";

// The panel divider must be drawn by exactly one element. When a panel also
// painted its own border-r/border-l, the handle's hover and active states were
// tinting a line that was already there, so they read as no change at all.
describe("resizable divider ownership", () => {
  function renderHandle(className?: string) {
    const { container } = render(<ResizableHandle className={className} />);
    return container.querySelector<HTMLElement>("[data-slot='resizable-handle']")!;
  }

  it("renders the resting divider rule itself", () => {
    expect(renderHandle()).toHaveClass("after:bg-border");
  });

  it("darkens on hover and while dragging", () => {
    const handle = renderHandle();
    expect(handle).toHaveClass("hover:after:bg-foreground/15");
    expect(handle).toHaveClass("data-[separator=active]:after:bg-foreground/25");
  });

  it("lets a caller drop the resting rule without losing the hover hint", () => {
    const handle = renderHandle("after:bg-transparent");
    // tailwind-merge keeps the caller's override as the winning background...
    expect(handle.className).toContain("after:bg-transparent");
    expect(handle.className).not.toContain("after:bg-border");
    // ...while the hover state is a different variant and survives.
    expect(handle).toHaveClass("hover:after:bg-foreground/15");
  });

  // The whole point of the sweep: the panel divider and the hand-written
  // handles read their look from one definition. If someone re-hardcodes the
  // tokens in resizable.tsx, these drift apart and this fails.
  it("takes its look from the shared variants, not a local copy", () => {
    const shared = resizeHandleVariants({
      axis: "x",
      cursor: "none",
      indicator: "rule",
      hitArea: "overlay",
    });
    const handle = renderHandle();
    for (const cls of shared.split(" ").filter(Boolean)) {
      expect(handle).toHaveClass(cls);
    }
  });

  it("leaves the cursor to the library", () => {
    expect(renderHandle().className).not.toMatch(/(^|\s|:)cursor-/);
  });

  it("uses the shared 8px grab zone", () => {
    expect(renderHandle()).toHaveClass("before:w-2");
  });
});
