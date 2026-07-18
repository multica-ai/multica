import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ModeConfigEditor, validateModeConfig } from "./mode-settings-tab";
import type { ModeConfig } from "./mode-config";

const config: ModeConfig = {
  mode: "plan", version: "3", instruction: "Plan first", model: "gpt-5.4",
  thinking_level: "high", timeout_minutes: 30, max_turns: 20,
  allows_write: false, allowed_tools: ["graphify"], data_sources: ["Company Brain"],
  approval_policy: "require", workflow_id: "workflow-1", eval_skill_ids: ["skill-1"],
};

describe("ModeConfigEditor", () => {
  afterEach(() => cleanup());

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

  it("describes invalid runtime limits and blocks publishing until fixed", () => {
    const onPublish = vi.fn();
    const invalidConfig = { ...config, timeout_minutes: 0, max_turns: 201 };

    expect(validateModeConfig(invalidConfig)).toEqual({
      timeout_minutes: "Use between 1 and 1,440 minutes.",
      max_turns: "Use between 1 and 200 turns.",
    });

    render(
      <ModeConfigEditor
        config={invalidConfig}
        onChange={() => undefined}
        onSave={() => undefined}
        onPublish={onPublish}
        canManage
      />,
    );

    expect(screen.getByText("Use between 1 and 1,440 minutes.")).toBeInTheDocument();
    expect(screen.getByText("Use between 1 and 200 turns.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Publish version" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Publish version" }));
    expect(onPublish).not.toHaveBeenCalled();
  });

  it("uses human labels for thinking and catalog choices", () => {
    render(
      <ModeConfigEditor
        config={{ ...config, thinking_level: "medium" }}
        onChange={() => undefined}
        onSave={() => undefined}
        onPublish={() => undefined}
        canManage
        toolChoices={[{ id: "graphify", name: "Graphify", description: "Find code relationships." }]}
        dataSourceChoices={[{ id: "company-brain", name: "Company Brain", description: "Search company knowledge." }]}
      />,
    );

    expect(screen.getByText("Balanced")).toBeInTheDocument();
    expect(screen.getByLabelText("Search required evaluations")).toBeInTheDocument();
    expect(screen.getByText("Graphify")).toBeInTheDocument();
    expect(screen.getAllByText("Company Brain").length).toBeGreaterThan(0);
    expect(screen.queryByPlaceholderText("One tool key per line")).not.toBeInTheDocument();
  });
});
