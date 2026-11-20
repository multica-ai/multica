import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { PanelBody, PanelScrollFadeContext } from "./panel-nav";

// jsdom drops `mask-image`, so the fade stands in as a style it keeps.
vi.mock("@multica/ui/hooks/use-scroll-fade", () => ({
  useScrollFade: () => ({ opacity: 0.5 }),
}));

function scroller() {
  return screen.getByText("rows").parentElement!;
}

describe("PanelBody scroll fade", () => {
  it("applies the scroll fade by default", () => {
    render(
      <PanelBody>
        <span>rows</span>
      </PanelBody>,
    );
    expect(scroller()).toHaveStyle({ opacity: "0.5" });
  });

  it("drops the scroll fade when the context turns it off", () => {
    render(
      <PanelScrollFadeContext.Provider value={false}>
        <PanelBody>
          <span>rows</span>
        </PanelBody>
      </PanelScrollFadeContext.Provider>,
    );
    expect(scroller()).not.toHaveAttribute("style");
  });
});
