// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ActivityHeatmap } from "./runs-control-room";

afterEach(cleanup);

describe("ActivityHeatmap", () => {
  it("keeps sparse activity in fixed-width time buckets", () => {
    render(
      <ActivityHeatmap
        result={{ columns: ["time", "runs"], rows: [{ time: "2026-07-13T18:00:00Z", runs: 1 }] }}
        onFilter={vi.fn()}
      />,
    );

    expect(screen.getByRole("grid", { name: "Activity by time bucket" }).style.gridTemplateColumns).toBe(
      "repeat(42, minmax(0, 1fr))",
    );
  });
});
