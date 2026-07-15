import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ModeConfigEditor } from "./mode-settings-tab";
import type { ModeConfig } from "./mode-config";

const config: ModeConfig = {
  mode: "plan", version: "3", instruction: "Plan first", model: "gpt-5.4",
  thinking_level: "high", timeout_minutes: 30, max_turns: 20,
  allows_write: false, allowed_tools: ["graphify"], data_sources: ["Company Brain"],
  approval_policy: "require", workflow_id: "workflow-1", eval_skill_ids: ["skill-1"],
};

describe("ModeConfigEditor", () => {
  it("exposes every runtime control and publishes the edited prompt", () => {
    const onChange = vi.fn();
    const onPublish = vi.fn();
    render(<ModeConfigEditor config={config} onChange={onChange} onSave={() => undefined} onPublish={onPublish} canManage />);

    expect(screen.getByLabelText("Instructions")).toHaveValue("Plan first");
    expect(screen.getByLabelText("Model")).toHaveValue("gpt-5.4");
    expect(screen.getByText("Required evaluations")).toBeInTheDocument();
    expect(screen.getByText("Workflow")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Instructions"), { target: { value: "Use the new Plan contract" } });
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ instruction: "Use the new Plan contract" }));
    fireEvent.click(screen.getByRole("button", { name: "Publish version" }));
    expect(onPublish).toHaveBeenCalledOnce();
  });
});
