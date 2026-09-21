import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Sparkline } from "./sparkline";

describe("activity sparkline outcomes", () => {
  it("keeps cancellations neutral while preserving total height", () => {
    const { container } = render(
      <Sparkline
        buckets={[{ total: 10, failed: 1, completed: 1 }]}
        width={20}
        height={101}
      />,
    );
    const group = container.querySelector("g")!;
    const bars = [...group.querySelectorAll("rect")];
    expect(bars).toHaveLength(3);
    expect(group.querySelector('rect[fill="var(--color-brand)"]')).toHaveAttribute("height", "10");
    expect(group.querySelector('rect[fill="var(--color-destructive)"]')).toHaveAttribute("height", "10");
    expect(group.querySelector('rect[fill="var(--color-muted-foreground)"]')).toHaveAttribute("height", "80");
    expect(bars.reduce((sum, bar) => sum + Number(bar.getAttribute("height")), 0)).toBe(100);
  });
});
