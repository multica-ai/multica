import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ModeConfigEditor, validateModeConfig } from "./mode-settings-tab";
import type { ModeConfig } from "./mode-config";

const config: ModeConfig = {
  mode: "plan", version: "3", instruction: "Plan first", model: "gpt-5.4",
  thinking_level: "high", timeout_minutes: 30, max_turns: 20,
  allows_write: false,
  allowed_tools: ["graphify"], data_sources: ["Company Brain"],
  approval_policy: "require", eval_ids: ["7e767171-6a19-41d5-b3e6-edfdc97eedf7"],
};

describe("ModeConfigEditor", () => {
  afterEach(() => cleanup());

  it("exposes every runtime control and publishes the edited prompt", () => {
    const onChange = vi.fn();
    const onPublish = vi.fn();
    render(<ModeConfigEditor config={config} onChange={onChange} onSave={() => undefined} onPublish={onPublish} canManage />);

    expect(screen.getByLabelText("Instructions")).toHaveValue("Plan first");
    expect(screen.getByLabelText("Model")).toHaveValue("gpt-5.4");
    expect(screen.getByText("Evaluations for this Mode")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Core behavior" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Access and approvals" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Evaluations" })).toBeInTheDocument();
    expect(screen.queryByText("Workflow")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Instructions"), { target: { value: "Use the new Plan contract" } });
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ instruction: "Use the new Plan contract" }));
    fireEvent.click(screen.getByRole("button", { name: "Publish version" }));
    expect(onPublish).toHaveBeenCalledOnce();
  });

  // FIR-4047: a Mode carries evaluations from the workspace eval catalog, not
  // extra skills. The server runs them against the issue when the session ends.
  it("picks evaluations from the workspace catalog and never offers extra skills", () => {
    const onChange = vi.fn();
    render(
      <ModeConfigEditor
        config={{ ...config, eval_ids: [] }}
        onChange={onChange}
        onSave={() => undefined}
        onPublish={() => undefined}
        canManage
        evals={[{ id: "7e767171-6a19-41d5-b3e6-edfdc97eedf7", name: "Plan completeness", description: "active" }]}
      />,
    );

    expect(screen.queryByText("Extra skills")).not.toBeInTheDocument();
    expect(screen.queryByRole("switch", { name: "Allow saving plans and notes" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("checkbox"));
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ eval_ids: ["7e767171-6a19-41d5-b3e6-edfdc97eedf7"] }),
    );
  });

  it("allows unlimited or large runtime limits and blocks negative values", () => {
    const onPublish = vi.fn();
    const invalidConfig = { ...config, timeout_minutes: -1, max_turns: -1 };

    expect(validateModeConfig(invalidConfig)).toEqual({
      timeout_minutes: "Use 0 or a positive whole number.",
      max_turns: "Use 0 or a positive whole number.",
    });
    expect(validateModeConfig({ ...config, timeout_minutes: 0, max_turns: 0 })).toEqual({});
    expect(validateModeConfig({ ...config, timeout_minutes: 100_000, max_turns: 100_000 })).toEqual({});

    render(
      <ModeConfigEditor
        config={invalidConfig}
        onChange={() => undefined}
        onSave={() => undefined}
        onPublish={onPublish}
        canManage
      />,
    );

    expect(screen.getAllByText("Use 0 or a positive whole number.")).toHaveLength(2);
    expect(screen.getByRole("button", { name: "Publish version" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Publish version" }));
    expect(onPublish).not.toHaveBeenCalled();
  });

  it("shows zero as unlimited without an artificial upper limit", () => {
    render(
      <ModeConfigEditor
        config={{ ...config, timeout_minutes: 0, max_turns: 0 }}
        onChange={() => undefined}
        onSave={() => undefined}
        onPublish={() => undefined}
        canManage
      />,
    );

    expect(screen.getByLabelText("Timeout (minutes)")).toHaveValue(0);
    expect(screen.getByLabelText("Maximum turns")).toHaveValue(0);
    expect(screen.getByLabelText("Timeout (minutes)")).not.toHaveAttribute("max");
    expect(screen.getByLabelText("Maximum turns")).not.toHaveAttribute("max");
    expect(screen.getByText("Set 0 for no time limit.")).toBeInTheDocument();
    expect(screen.getByText("A turn is one agent response. Set 0 for no turn limit.")).toBeInTheDocument();
  });

  it("requires an explicit zero for an unlimited limit", () => {
    const onChange = vi.fn();
    render(
      <ModeConfigEditor
        config={config}
        onChange={onChange}
        onSave={() => undefined}
        onPublish={() => undefined}
        canManage
      />,
    );

    fireEvent.change(screen.getByLabelText("Timeout (minutes)"), { target: { value: "" } });
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ timeout_minutes: Number.NaN }));
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
    expect(screen.getByLabelText("Search evaluations")).toBeInTheDocument();
    expect(screen.getByText("Graphify")).toBeInTheDocument();
    expect(screen.getAllByText("Company Brain").length).toBeGreaterThan(0);
    expect(screen.queryByPlaceholderText("One tool key per line")).not.toBeInTheDocument();
  });
});
