// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { AIImpactControlRoom } from "./ai-impact-control-room";

afterEach(cleanup);

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
});
