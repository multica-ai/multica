import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { RestrictedLock } from "./restricted-lock";

describe("RestrictedLock", () => {
  it("renders the lock indicator with aria-label", () => {
    render(<RestrictedLock />);
    const el = screen.getByTestId("restricted-lock");
    expect(el).toBeInTheDocument();
    expect(el).toHaveAttribute("aria-label", "Restricted project");
  });

  it("uses the destructive color so it reads as red", () => {
    render(<RestrictedLock />);
    expect(screen.getByTestId("restricted-lock").className).toContain(
      "text-destructive",
    );
  });

  it("merges a className override", () => {
    render(<RestrictedLock className="custom-mr" />);
    expect(screen.getByTestId("restricted-lock").className).toContain(
      "custom-mr",
    );
  });
});
