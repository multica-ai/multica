// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { useDashboardStore } from "../../core/store";
import { AIImpactControlRoom } from "./ai-impact-control-room";

afterEach(() => {
  cleanup();
  useDashboardStore.getState().reset();
});

describe("AIImpactControlRoom", () => {
  it("renders the three API-backed views", () => {
    render(
      <AIImpactControlRoom
        overview={{
          families: [
            {
              family: "Outcome",
              evidence: [
                {
                  function_id: "function-1",
                  function_name: "Customer Service",
                  operating_loop_id: "loop-1",
                  operating_loop_name: "Resolve customer needs",
                  metric_id: "metric-1",
                  metric_name: "Needs solved",
                  metric_family: "Outcome",
                  metric_unit: "%",
                  metric_direction: "increase",
                  period_start: "2026-07-01T00:00:00Z",
                  period_end: "2026-07-22T00:00:00Z",
                  value: 82,
                  evidence_status: "Measured",
                  confidence: 0.91,
                  source: "assessment",
                  method: "sampled",
                },
              ],
            },
          ],
        }}
        functions={{
          functions: [
            {
              id: "function-1",
              name: "Customer Service",
              operating_loops: [
                { id: "loop-1", name: "Resolve customer needs", decision: "Scale" },
              ],
            },
          ],
        }}
        qualityRisk={{
          decisions: [
            {
              function_id: "function-1",
              function_name: "Customer Service",
              operating_loop_id: "loop-1",
              operating_loop_name: "Resolve customer needs",
              decision: "Scale",
            },
          ],
        }}
        isLoading={{ overview: false, functions: false, qualityRisk: false }}
      />,
    );

    expect(screen.getByText("Needs solved")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Functions" }));
    expect(screen.getByText("Resolve customer needs")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Quality & Risk" }));
    expect(screen.getByText("Scale")).toBeTruthy();
  });

  it("filters every AI Impact view from a row click and clears from the chip", () => {
    render(
      <AIImpactControlRoom
        overview={{ families: [] }}
        functions={{
          functions: [
            {
              id: "function-1",
              name: "Customer Service",
              operating_loops: [
                { id: "loop-1", name: "Resolve customer needs", decision: "Scale" },
              ],
            },
            {
              id: "function-2",
              name: "Finance",
              operating_loops: [
                { id: "loop-2", name: "Close the books", decision: "Observe" },
              ],
            },
          ],
        }}
        qualityRisk={{
          decisions: [
            {
              function_id: "function-1",
              function_name: "Customer Service",
              operating_loop_id: "loop-1",
              operating_loop_name: "Resolve customer needs",
              decision: "Scale",
            },
            {
              function_id: "function-2",
              function_name: "Finance",
              operating_loop_id: "loop-2",
              operating_loop_name: "Close the books",
              decision: "Observe",
            },
          ],
        }}
        isLoading={{ overview: false, functions: false, qualityRisk: false }}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Functions" }));
    expect(screen.getByText("Finance")).toBeTruthy();
    fireEvent.click(
      screen.getAllByRole("button", {
        name: "Filter AI Impact by Customer Service · Resolve customer needs",
      })[0]!,
    );
    expect(screen.queryByText("Finance")).toBeNull();
    expect(useDashboardStore.getState().aiImpactSelections.functionName).toBe("Customer Service");

    fireEvent.click(screen.getByRole("button", { name: "Quality & Risk" }));
    expect(screen.queryByText("Close the books")).toBeNull();
    expect(screen.getByText("Resolve customer needs")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "Remove Function filter Customer Service" }),
    );
    expect(useDashboardStore.getState().aiImpactSelections.functionName).toBeNull();
  });
});
