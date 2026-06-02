import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { HealthPill } from "./health-pill";

describe("HealthPill", () => {
  it("renders a label for a known health value", () => {
    render(<HealthPill health="at_risk" />);
    expect(screen.getByText(/at risk/i)).toBeInTheDocument();
  });
  it("renders a neutral fallback for an unknown value (enum drift)", () => {
    // @ts-expect-error testing drift
    render(<HealthPill health="exploded" />);
    expect(screen.getByText(/no update|unknown/i)).toBeInTheDocument();
  });
  it("renders 'No update' when health is null", () => {
    render(<HealthPill health={null} />);
    expect(screen.getByText(/no update/i)).toBeInTheDocument();
  });
});
