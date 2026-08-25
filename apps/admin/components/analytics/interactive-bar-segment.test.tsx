import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { BarRectangleItem } from "recharts";
import { InteractiveBarSegment } from "./interactive-bar-segment";

const BAR_PROPS: BarRectangleItem = {
  value: [0, 3],
  payload: { bucketStart: "2026-08-20T00:00:00.123Z" },
  parentViewBox: { x: 0, y: 0, width: 100, height: 100 },
  tooltipPosition: { x: 0, y: 0 },
  stackedBarStart: 0,
  originalDataIndex: 0,
  x: 0,
  y: 0,
  width: 20,
  height: 30,
};

describe("InteractiveBarSegment", () => {
  it("opens the segment with pointer, Enter, and Space activation", () => {
    const onActivate = vi.fn();
    render(
      <svg>
        <InteractiveBarSegment {...BAR_PROPS} label="Auth errors" onActivate={onActivate} />
      </svg>,
    );

    const segment = screen.getByRole("button", { name: /Auth errors: 3 on .+\. Show workspace breakdown\./ });
    fireEvent.click(segment);
    fireEvent.keyDown(segment, { key: "Enter" });
    fireEvent.keyDown(segment, { key: " " });

    expect(onActivate).toHaveBeenCalledTimes(3);
    expect(onActivate).toHaveBeenLastCalledWith("2026-08-20T00:00:00.123Z");
  });

  it("does not expose zero-valued segments as controls", () => {
    render(
      <svg>
        <InteractiveBarSegment {...BAR_PROPS} value={[3, 3]} label="Auth errors" onActivate={vi.fn()} />
      </svg>,
    );

    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
