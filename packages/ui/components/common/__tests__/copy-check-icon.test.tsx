import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { CopyCheckIcon } from "../copy-check-icon";

afterEach(cleanup);

describe("CopyCheckIcon", () => {
  it("swaps from Copy to Check with text-success and motion-reduce animation guard", () => {
    const { container, rerender } = render(<CopyCheckIcon copied={false} />);
    const root = container.querySelector('[data-slot="copy-check-icon"]');
    expect(root).not.toBeNull();
    expect(root).not.toHaveAttribute("data-copied");
    expect(container.querySelector(".icon-copy")).not.toBeNull();
    expect(container.querySelector(".icon-check")).toBeNull();

    rerender(<CopyCheckIcon copied />);
    expect(root).toHaveAttribute("data-copied", "");
    expect(container.querySelector(".icon-copy")).toBeNull();
    const check = container.querySelector(".icon-check");
    expect(check).not.toBeNull();
    expect(check!.getAttribute("class")).toContain("text-success");
    expect(check!.getAttribute("class")).toContain("zoom-in-50");
    expect(check!.getAttribute("class")).toContain("motion-reduce:animate-none!");

    rerender(<CopyCheckIcon copied={false} />);
    expect(root).not.toHaveAttribute("data-copied");
    expect(container.querySelector(".icon-check")).toBeNull();
    const copy = container.querySelector(".icon-copy");
    expect(copy).not.toBeNull();
    expect(copy!.getAttribute("class")).toContain("zoom-in-50");
    expect(copy!.getAttribute("class")).toContain("motion-reduce:animate-none!");
  });
});
