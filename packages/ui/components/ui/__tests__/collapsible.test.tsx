import { createRef } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "../collapsible";

afterEach(() => {
  cleanup();
  delete (Element.prototype as { getAnimations?: unknown }).getAnimations;
  delete (HTMLElement.prototype as { scrollHeight?: unknown }).scrollHeight;
});

// jsdom has no layout or animations, so Base UI would treat the panel as
// unanimated. An inline transition puts it on its CSS-transition path, a
// stubbed getAnimations() holds that transition open until the test ends it,
// and a non-zero scrollHeight gives it a height to close from.
const TRANSITION = { transitionDuration: "180ms", animationName: "none" };

function stubScrollHeight() {
  Object.defineProperty(HTMLElement.prototype, "scrollHeight", {
    configurable: true,
    get: () => 64,
  });
}

function holdTransitions() {
  let endTransition: () => void = () => {};
  const finished = new Promise<void>((resolve) => {
    endTransition = resolve;
  });
  Element.prototype.getAnimations = () =>
    [{ finished, pending: false, playState: "running" }] as unknown as Animation[];
  return endTransition;
}

async function frames(count = 4) {
  for (let i = 0; i < count; i++) {
    await act(async () => {
      flushRaf();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  }
}

describe("CollapsibleContent", () => {
  it("marks the panel settled only while its height is auto", async () => {
    render(
      <Collapsible open>
        <CollapsibleContent>Row</CollapsibleContent>
      </Collapsible>,
    );
    const panel = screen.getByText("Row");
    expect(panel).toHaveAttribute("data-slot", "collapsible-content");
    expect(panel).toHaveAttribute("data-settled");

    panel.style.setProperty("--collapsible-panel-height", "40px");
    await waitFor(() => expect(panel).not.toHaveAttribute("data-settled"));

    panel.style.setProperty("--collapsible-panel-height", "auto");
    await waitFor(() => expect(panel).toHaveAttribute("data-settled"));
  });

  it("forwards its ref to the panel", () => {
    const ref = createRef<HTMLDivElement>();
    const { rerender } = render(
      <Collapsible open>
        <CollapsibleContent ref={ref}>Row</CollapsibleContent>
      </Collapsible>,
    );
    expect(ref.current).toBe(screen.getByText("Row"));

    rerender(<Collapsible open={false}>{null}</Collapsible>);
    expect(ref.current).toBeNull();
  });

  it("leaves rows unreachable while it closes", async () => {
    stubScrollHeight();
    render(
      <Collapsible defaultOpen>
        <CollapsibleTrigger>Toggle</CollapsibleTrigger>
        <CollapsibleContent data-testid="panel" style={TRANSITION}>
          <button type="button">Row</button>
        </CollapsibleContent>
      </Collapsible>,
    );
    await frames();
    const panel = screen.getByTestId("panel");
    expect(panel).not.toHaveAttribute("inert");

    const endTransition = holdTransitions();
    fireEvent.click(screen.getByRole("button", { name: "Toggle" }));
    await frames();
    expect(panel).toHaveAttribute("data-ending-style");
    expect(panel).not.toHaveAttribute("data-settled");
    expect(panel).toHaveAttribute("inert");

    endTransition();
    await frames();
    expect(screen.queryByTestId("panel")).toBeNull();
  });
});
