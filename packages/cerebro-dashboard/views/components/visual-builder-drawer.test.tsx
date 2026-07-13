// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { VisualBuilderDrawer } from "./visual-builder-drawer";

const catalog = {
  populations: ["all" as const],
  metrics: ["runs" as const, "cost_cents" as const],
  dimensions: ["project" as const, "person" as const, "time" as const],
  grains: ["none" as const, "day" as const],
  operators: ["in" as const],
};

afterEach(cleanup);

describe("VisualBuilderDrawer", () => {
  it("builds the mockup visual definition and closes from the rail", () => {
    const onClose = vi.fn();
    const onSave = vi.fn();
    render(<VisualBuilderDrawer catalog={catalog} onClose={onClose} onSave={onSave} />);

    expect(screen.getByRole("complementary", { name: "New visual" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Line chart" }));
    fireEvent.change(screen.getByLabelText("Metric"), { target: { value: "runs" } });
    fireEvent.change(screen.getByLabelText("Dimension"), { target: { value: "project" } });
    fireEvent.change(screen.getByLabelText("Breakdown"), { target: { value: "person" } });
    fireEvent.change(screen.getByLabelText("Grain"), { target: { value: "day" } });
    fireEvent.click(screen.getByRole("button", { name: "Add visual to Dashboard" }));

    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({
      title: "Project runs by person",
      kind: "bars",
      presentation: "line",
      metrics: ["runs"],
      dimensions: ["project", "person"],
      grain: "day",
    }));

    fireEvent.click(screen.getByRole("button", { name: "Close new visual" }));
    expect(onClose).toHaveBeenCalledOnce();
  });
});
