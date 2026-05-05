import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { RestrictedDot } from "./restricted-dot";

describe("RestrictedDot", () => {
  it("renders the 6px restricted indicator with aria-label", () => {
    render(<RestrictedDot />);
    const el = screen.getByTestId("restricted-dot");
    expect(el).toBeInTheDocument();
    expect(el).toHaveAttribute("aria-label", "Restricted project");
  });

  it("merges a className override", () => {
    render(<RestrictedDot className="custom-mr" />);
    expect(screen.getByTestId("restricted-dot").className).toContain(
      "custom-mr",
    );
  });
});
