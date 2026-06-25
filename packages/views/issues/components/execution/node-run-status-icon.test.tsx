import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { NodeRunStatusIcon } from "./node-run-status-icon";

describe("NodeRunStatusIcon", () => {
  it("renders pending as empty circle", () => {
    render(<NodeRunStatusIcon status="pending" />);
    const icon = screen.getByTestId("status-icon-pending");
    expect(icon).toBeInTheDocument();
    expect(icon).toHaveClass("text-muted-foreground/40");
  });

  it("renders completed as green check", () => {
    render(<NodeRunStatusIcon status="completed" />);
    const icon = screen.getByTestId("status-icon");
    expect(icon).toHaveClass("text-green-500");
  });

  it("renders working as spinning loader", () => {
    render(<NodeRunStatusIcon status="working" />);
    const icon = screen.getByTestId("status-icon");
    expect(icon).toHaveClass("animate-spin");
    expect(icon).toHaveClass("text-blue-500");
  });

  it("renders critic_rework as orange RotateCcw", () => {
    render(<NodeRunStatusIcon status="critic_rework" />);
    const icon = screen.getByTestId("status-icon");
    expect(icon).toHaveClass("text-orange-500");
  });

  it("renders blocked as red AlertCircle", () => {
    render(<NodeRunStatusIcon status="blocked" />);
    const icon = screen.getByTestId("status-icon");
    expect(icon).toHaveClass("text-red-500");
  });

  it("falls back on unknown status", () => {
    // @ts-expect-error testing invalid status
    render(<NodeRunStatusIcon status="unknown_future_status" />);
    expect(screen.getByTestId("status-icon-fallback")).toBeInTheDocument();
  });
});
