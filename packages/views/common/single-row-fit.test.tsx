import { act, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useSingleRowFit } from "./single-row-fit";

function Harness({ contentKey = "one" }: { contentKey?: string }) {
  const [tick, setTick] = useState(0);
  const { containerRef, measureRef, fitCount } = useSingleRowFit({
    count: 2,
    gap: 4,
    reserve: 36,
    contentKey,
  });
  return (
    <>
      <button type="button" onClick={() => setTick((value) => value + 1)}>
        tick {tick}
      </button>
      <div ref={containerRef} data-testid="container">
        <div ref={measureRef}>
          <span />
          <span />
        </div>
        <output>{fitCount}</output>
      </div>
    </>
  );
}

describe("useSingleRowFit", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("does not remeasure on unrelated commits", () => {
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(400);
    const layoutEffectMeasure = vi
      .spyOn(HTMLElement.prototype, "offsetWidth", "get")
      .mockReturnValue(80);

    render(<Harness />);
    const initialReads = layoutEffectMeasure.mock.calls.length;
    act(() => {
      screen.getByRole("button", { name: "tick 0" }).click();
    });
    act(() => {
      screen.getByRole("button", { name: "tick 1" }).click();
    });

    expect(layoutEffectMeasure.mock.calls.length).toBe(initialReads);
  });

  it("remeasures when mirror content changes", () => {
    const offsetWidth = vi
      .spyOn(HTMLElement.prototype, "offsetWidth", "get")
      .mockReturnValue(80);
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(400);

    const { rerender } = render(<Harness contentKey="one" />);
    const initialReads = offsetWidth.mock.calls.length;
    rerender(<Harness contentKey="two" />);

    expect(offsetWidth.mock.calls.length).toBeGreaterThan(initialReads);
  });
});
