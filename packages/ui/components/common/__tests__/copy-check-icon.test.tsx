import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { CopyCheckIcon } from "../copy-check-icon";

describe("CopyCheckIcon", () => {
  it("swaps between Copy and Check and animates only after toggling", () => {
    const { container, rerender } = render(<CopyCheckIcon copied={false} />);
    const initialCopy = container.querySelector(".icon-copy");
    expect(initialCopy).not.toBeNull();
    expect(container.querySelector(".icon-check")).toBeNull();
    expect(initialCopy).toHaveAttribute("data-copied", "false");
    expect(initialCopy!.getAttribute("class")).not.toContain("animate-in");

    rerender(<CopyCheckIcon copied />);
    const check = container.querySelector(".icon-check");
    expect(check).not.toBeNull();
    expect(container.querySelector(".icon-copy")).toBeNull();
    expect(check).toHaveAttribute("data-copied", "true");
    expect(check!.getAttribute("class")).toContain("animate-in");
    expect(check!.getAttribute("class")).toContain("text-success");

    rerender(<CopyCheckIcon copied={false} />);
    const restoredCopy = container.querySelector(".icon-copy");
    expect(restoredCopy).not.toBeNull();
    expect(restoredCopy!.getAttribute("class")).toContain("animate-in");
  });
});
