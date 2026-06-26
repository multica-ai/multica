import { type ReactNode } from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";

// Simulate a CLOSED tooltip: Base UI portals TooltipContent (and mounts its
// children) only while open, so a closed tooltip renders no content. This is
// what makes the lazy `content()` computation actually deferred in production.
vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render: r }: { render: ReactNode }) => <>{r}</>,
  TooltipContent: () => null,
}));

import { HoverTooltip } from "./instant-tooltip";

describe("HoverTooltip", () => {
  it("does not compute the tooltip text until the tooltip mounts (lazy)", () => {
    const content = vi.fn(() => "full timestamp");
    render(<HoverTooltip content={content}>visible text</HoverTooltip>);
    // The visible trigger renders eagerly...
    expect(screen.getByText("visible text")).toBeTruthy();
    // ...but the expensive tooltip string lives inside TooltipContent, which is
    // unmounted while closed — so compute must NOT run on first paint. Guards
    // against regressing to an eager `content()` call in the parent render body.
    expect(content).not.toHaveBeenCalled();
  });

  it("renders only the trigger (no tooltip, no compute) when disabled", () => {
    const content = vi.fn(() => "full timestamp");
    render(
      <HoverTooltip content={content} enabled={false}>
        plain text
      </HoverTooltip>,
    );
    expect(screen.getByText("plain text")).toBeTruthy();
    expect(content).not.toHaveBeenCalled();
  });
});
