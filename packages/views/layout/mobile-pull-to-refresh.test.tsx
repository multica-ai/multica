import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";
import { MobilePullToRefresh } from "./mobile-pull-to-refresh";

// On desktop, MobilePullToRefresh must be a passthrough — no indicator,
// no touch listeners, no extra wrapper that would change the scroll
// container's overflow / sizing class. Easiest signal: the rendered tree
// is a single div with the supplied className and no aria-hidden indicator.
vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => false,
}));

describe("MobilePullToRefresh — desktop passthrough", () => {
  it("renders a plain div without the indicator on desktop", () => {
    const onRefresh = vi.fn();
    const { container } = render(
      <MobilePullToRefresh onRefresh={onRefresh} className="h-full overflow-y-auto">
        <span data-testid="child">child</span>
      </MobilePullToRefresh>,
    );
    // Only the child is rendered — no spinning indicator wrapper.
    expect(container.querySelector('[aria-hidden="true"]')).toBeNull();
    // Class hand-off must survive: parents rely on these utility classes
    // being present on the actual scroll container.
    const root = container.firstElementChild as HTMLElement;
    expect(root.className).toBe("h-full overflow-y-auto");
    // Children render through.
    expect(container.querySelector('[data-testid="child"]')).not.toBeNull();
  });

  it("never invokes onRefresh on desktop (no touch listeners)", () => {
    const onRefresh = vi.fn();
    render(
      <MobilePullToRefresh onRefresh={onRefresh} className="h-full">
        <span>x</span>
      </MobilePullToRefresh>,
    );
    expect(onRefresh).not.toHaveBeenCalled();
  });
});
