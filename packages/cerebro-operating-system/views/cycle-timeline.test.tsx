// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";

import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { CycleTimeline } from "./cycle-timeline";

describe("CycleTimeline", () => {
  it("renders the projected schedule with the current cycle marked", () => {
    render(<CycleTimeline cadenceUnit="week" cadenceCount={1} label="Cycles" today="2026-01-15" />);
    expect(screen.getByRole("region", { name: "Cycles timeline" })).toBeInTheDocument();
    expect(screen.getByText("Current")).toBeInTheDocument();
    expect(screen.getByText("Previous")).toBeInTheDocument();
    expect(screen.getByText("Next")).toBeInTheDocument();
    expect(screen.getByText("Jan 15, 2026")).toBeInTheDocument();
    expect(screen.getByText("Jan 22, 2026")).toBeInTheDocument();
  });

  it("renders nothing when there is no recurring cadence", () => {
    const { container } = render(<CycleTimeline cadenceUnit="manual" cadenceCount={1} label="Cycles" today="2026-01-15" />);
    expect(container).toBeEmptyDOMElement();
  });
});
