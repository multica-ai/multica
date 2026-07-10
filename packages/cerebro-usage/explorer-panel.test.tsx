// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { UsageExplorerPanel } from "./explorer-panel";

describe("UsageExplorerPanel", () => {
  it("applies facet and saving filters and opens run details", () => {
    const select=vi.fn();
    render(<UsageExplorerPanel data={{summary:{runs:1,tokens:30,actual_cost_cents:12,calculated_cost_runs:0,missing_cost_runs:0},facets:{model:[{value:"gpt-5",count:1}]},total:1,savings:[{type:"graphify",state:"measured",saved_cents:4,saved_units:20,affected_runs:1}],runs:[{id:"run-1",created_at:"2026-07-10T08:00:00Z",status:"completed",project:"Core",agent:"Lone",runtime:"Codex",model:"gpt-5",provider:"openai",trigger:"issue",input_tokens:10,output_tokens:20,cost_cents:12,cost_kind:"actual",duration_seconds:5,trace_url:"/traces/run-1"}]}} onSelect={select} />);
    fireEvent.click(screen.getByRole("button",{name:"Include model gpt-5"})); expect(select).toHaveBeenCalledWith("model","gpt-5","include");
    fireEvent.click(screen.getByRole("button",{name:"Filter by saving graphify"})); expect(select).toHaveBeenCalledWith("saving","graphify","include");
    fireEvent.click(screen.getByRole("button",{name:"Open run run-1"})); expect(screen.getByText("Run details")).toBeTruthy();
    expect(screen.getByRole("link",{name:"Open Trace"}).getAttribute("href")).toBe("/traces/run-1");
  });
});
