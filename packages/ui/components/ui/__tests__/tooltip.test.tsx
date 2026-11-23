import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { Tooltip, TooltipContent, TooltipTrigger } from "../tooltip";

afterEach(cleanup);

// jsdom runs no CSS animations, so these check the popup's classes. In a
// browser, a closing tooltip that turns instant mid-exit stayed on screen when
// the instant rule removed the exit animation (MUL-9998).
describe("TooltipContent", () => {
  function popupClasses() {
    render(
      <Tooltip open>
        <TooltipTrigger>Trigger</TooltipTrigger>
        <TooltipContent>Tip</TooltipContent>
      </Tooltip>,
    );
    const popup = screen.getByText("Tip").closest("[data-slot='tooltip-content']");
    return (popup?.getAttribute("class") ?? "").split(/\s+/);
  }

  it("ends an instant tooltip's animation instead of removing it", () => {
    const classes = popupClasses();
    expect(classes).toContain("data-[instant]:animation-duration-0!");
    expect(classes.filter((c) => c.startsWith("data-[instant]:animate-"))).toEqual([]);
  });

  it("has no CSS transitions for the unmount to wait on", () => {
    expect(popupClasses()).toContain("transition-none");
  });

  it("disables hoverable popup by default so dense vertical triggers stay reachable", () => {
    render(
      <Tooltip open>
        <TooltipTrigger>Trigger</TooltipTrigger>
        <TooltipContent>Tip</TooltipContent>
      </Tooltip>,
    );
    const popup = screen.getByText("Tip").closest("[data-slot='tooltip-content']");
    expect(popup?.parentElement).toHaveStyle({ pointerEvents: "none" });
  });

  it("allows callers to opt back into a hoverable popup", () => {
    render(
      <Tooltip open disableHoverablePopup={false}>
        <TooltipTrigger>Trigger</TooltipTrigger>
        <TooltipContent>Tip</TooltipContent>
      </Tooltip>,
    );
    const popup = screen.getByText("Tip").closest("[data-slot='tooltip-content']");
    expect(popup?.parentElement).not.toHaveStyle({ pointerEvents: "none" });
  });
});
