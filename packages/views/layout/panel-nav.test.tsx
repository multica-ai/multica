import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { DEFAULT_APPEARANCE, useAppearanceStore } from "@multica/core/appearance";
import { PanelBody, PanelDisclosure, PanelScrollFadeContext } from "./panel-nav";

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

describe("PanelDisclosure", () => {
  afterEach(() => {
    useAppearanceStore.setState({ ...DEFAULT_APPEARANCE });
  });

  it("collapses and unmounts content after exit animation", async () => {
    const { rerender } = render(
      <PanelDisclosure open id="sec-1">
        <span>section body</span>
      </PanelDisclosure>,
    );
    expect(screen.getByText("section body")).toBeInTheDocument();

    rerender(
      <PanelDisclosure open={false} id="sec-1">
        <span>section body</span>
      </PanelDisclosure>,
    );
    await waitFor(() => expect(screen.queryByText("section body")).toBeNull());
  });

  it("clips only while animating, so a resting section keeps its rows' focus outlines", async () => {
    const { rerender } = render(
      <PanelDisclosure open id="sec-3">
        <span>clip body</span>
      </PanelDisclosure>,
    );
    const body = () => document.getElementById("sec-3")!;
    expect(body().style.overflow).toBe("visible");

    rerender(
      <PanelDisclosure open={false} id="sec-3">
        <span>clip body</span>
      </PanelDisclosure>,
    );
    await waitFor(() => expect(document.getElementById("sec-3")).toBeNull());

    rerender(
      <PanelDisclosure open id="sec-3">
        <span>clip body</span>
      </PanelDisclosure>,
    );
    // jsdom never applies Motion's discrete `transitionEnd`, so the return
    // to `visible` after the enter animation is only observable in a browser.
    expect(body().style.overflow).toBe("hidden");
  });

  it("unmounts immediately in the same frame when reduced motion is enabled", () => {
    useAppearanceStore.setState({ ...DEFAULT_APPEARANCE, reduceMotion: true });
    const { rerender } = render(
      <PanelDisclosure open id="sec-2">
        <span>reduced body</span>
      </PanelDisclosure>,
    );
    expect(screen.getByText("reduced body")).toBeInTheDocument();

    rerender(
      <PanelDisclosure open={false} id="sec-2">
        <span>reduced body</span>
      </PanelDisclosure>,
    );
    expect(screen.queryByText("reduced body")).toBeNull();
  });
});
