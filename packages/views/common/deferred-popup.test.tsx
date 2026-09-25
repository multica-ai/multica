import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { DeferredPopup } from "./deferred-popup";

function popupContent(open: boolean) {
  return (
    <span data-testid="mounted-popup" data-open={String(open)}>
      Popup mounted
    </span>
  );
}

describe("DeferredPopup", () => {
  it("opens its default trigger and cancels an enclosing link's native navigation", () => {
    render(
      <a href="/issues/issue-1">
        <DeferredPopup trigger={<span>Open picker</span>}>
          {popupContent}
        </DeferredPopup>
      </a>,
    );

    expect(fireEvent.click(screen.getByText("Open picker"))).toBe(false);
    expect(screen.getByTestId("mounted-popup")).toHaveAttribute("data-open", "true");
  });

  it("does the same for a custom trigger element", () => {
    render(
      <a href="/issues/issue-1">
        <DeferredPopup triggerRender={<button type="button">Open priority</button>}>
          {popupContent}
        </DeferredPopup>
      </a>,
    );

    expect(fireEvent.click(screen.getByText("Open priority"))).toBe(false);
    expect(screen.getByTestId("mounted-popup")).toHaveAttribute("data-open", "true");
  });
});
