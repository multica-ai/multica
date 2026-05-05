import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { RestrictedRef } from "./restricted-ref";

describe("RestrictedRef", () => {
  it("renders the generic Restricted label and aria-label", () => {
    render(<RestrictedRef />);
    const el = screen.getByTestId("restricted-ref");
    expect(el).toBeInTheDocument();
    expect(el).toHaveAttribute("aria-label", "Restricted item");
    expect(el.textContent?.trim()).toBe("Restricted");
  });

  it("does NOT leak any identifier or title", () => {
    render(<RestrictedRef />);
    const el = screen.getByTestId("restricted-ref");
    // Strict text content match — no other text leaked
    expect(el.textContent).toBe("Restricted");
  });

  it("supports a block variant", () => {
    render(<RestrictedRef variant="block" />);
    expect(screen.getByTestId("restricted-ref").className).toContain("px-2");
  });
});
