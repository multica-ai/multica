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
  function renderHandle(
    props?: Partial<React.ComponentProps<typeof ResizableHandle>>,
  ) {
    const { container } = render(<ResizableHandle {...props} />);
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

  // A collapsed sidebar has no panel on the far side, so the resting rule has
  // to go while the grab hint stays.
  //
  // The assertion deliberately names no pseudo-element: the last regression
  // was exactly that. The rule moved from ::before to ::after and the two
  // callers kept overriding ::before via className, so they silently stopped
  // hiding anything. `rule` is a prop now — which pseudo draws the line is the
  // component's business, not the caller's.
  it("drops the resting rule when there is nothing to divide", () => {
    const handle = renderHandle({ rule: false });
    expect(handle.className).not.toMatch(/:bg-border/);
    expect(handle).toHaveClass("hover:after:bg-foreground/15");
    expect(handle).toHaveClass("data-[separator=active]:after:bg-foreground/25");
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
