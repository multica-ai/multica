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

// The panel divider must be drawn by exactly one element. When a panel also
// painted its own border-r/border-l, the handle's hover and active states were
// tinting a line that was already there, so they read as no change at all.
describe("resizable divider ownership", () => {
  function renderHandle(className?: string) {
    const { container } = render(<ResizableHandle className={className} />);
    return container.querySelector<HTMLElement>("[data-slot='resizable-handle']")!;
  }

  it("renders the resting divider rule itself", () => {
    expect(renderHandle()).toHaveClass("before:bg-border");
  });

  it("darkens on hover and while dragging", () => {
    const handle = renderHandle();
    expect(handle).toHaveClass("hover:before:bg-foreground/15");
    expect(handle).toHaveClass("data-[separator=active]:before:bg-foreground/25");
  });

  it("lets a caller drop the resting rule without losing the hover hint", () => {
    const handle = renderHandle("before:bg-transparent");
    // tailwind-merge keeps the caller's override as the winning background...
    expect(handle.className).toContain("before:bg-transparent");
    expect(handle.className).not.toContain("before:bg-border");
    // ...while the hover state is a different variant and survives.
    expect(handle).toHaveClass("hover:before:bg-foreground/15");
  });
});
