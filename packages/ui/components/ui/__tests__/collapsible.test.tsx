import { createRef } from "react";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { Collapsible, CollapsibleContent } from "../collapsible";

afterEach(cleanup);

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
});
