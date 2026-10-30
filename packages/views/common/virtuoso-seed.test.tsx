import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { VirtuosoSeed } from "./virtuoso-seed";

describe("VirtuosoSeed scroll-height reservation", () => {
  const data = Array.from({ length: 100 }, (_, index) => index);
  const props = {
    data,
    computeItemKey: (index: number) => index,
    itemContent: (_index: number, value: number) => <div data-row>{value}</div>,
  };

  it.each([
    [36, "2520px"],
    ["var(--issue-row-height)", "calc(70 * var(--issue-row-height))"],
  ])("reserves unmounted rows using %s", (height, expected) => {
    const { container } = render(<VirtuosoSeed {...props} estimatedItemHeight={height} />);
    expect(container.querySelectorAll("[data-row]")).toHaveLength(30);
    expect(container.querySelector<HTMLElement>("[aria-hidden]")?.style.height).toBe(expected);
  });

  it("does not reserve space beyond the data for a short list", () => {
    const { container } = render(<VirtuosoSeed {...props} data={[1, 2]} estimatedItemHeight="var(--issue-row-height)" />);
    expect(container.querySelectorAll("[data-row]")).toHaveLength(2);
    expect(container.querySelector("[aria-hidden]")).toBeNull();
  });
});
