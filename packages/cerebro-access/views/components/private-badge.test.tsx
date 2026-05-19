import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { PrivateBadge } from "./private-badge";

describe("PrivateBadge", () => {
  it("renders the Private label and is accessible", () => {
    render(<PrivateBadge />);
    const badge = screen.getByTestId("private-badge");
    expect(badge).toHaveAttribute("aria-label", "Private");
    expect(badge).toHaveTextContent("Private");
  });

  it("renders the small size variant when size='sm'", () => {
    render(<PrivateBadge size="sm" />);
    const badge = screen.getByTestId("private-badge");
    expect(badge.className).toContain("h-4");
  });
});
