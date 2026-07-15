// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { WorkflowBuilder } from "./workflow-builder";

describe("WorkflowBuilder", () => {
  it("renders a vertical trigger-to-step chain and emits the same JSON contract", async () => {
    const onChange = vi.fn();
    render(<WorkflowBuilder value={{ schema_version: "1", trigger: { id: "trigger", type: "manual", config: {} }, steps: [] }} onChange={onChange} />);
    expect(screen.getAllByText("When started manually")).toHaveLength(2);
    await userEvent.click(screen.getByRole("button", { name: "Add step" }));
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ steps: [expect.objectContaining({ type: "registry.read" })] }));
  });

  it("edits step fields and tests them with visible sample data", async () => {
    const onChange = vi.fn();
    const onTestStep = vi.fn();
    render(<WorkflowBuilder value={{ schema_version: "1", trigger: { id: "trigger", type: "manual", config: {} }, steps: [{ id: "read", type: "registry.read", config: {} }] }} onChange={onChange} onTestStep={onTestStep} />);

    await userEvent.click(screen.getByRole("button", { name: "Configure Read registry data" }));
    await userEvent.type(screen.getByLabelText("Resource ID"), "products");
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({
      steps: [expect.objectContaining({ config: { resource_id: "products" } })],
    }));

    fireEvent.change(screen.getByLabelText("Sample data"), { target: { value: '{"sku":"123"}' } });
    await userEvent.click(screen.getByRole("button", { name: "Test step" }));
    expect(onTestStep).toHaveBeenCalledWith("read", { sku: "123" });
  });

  it("shows understandable configuration fields for filter and view steps", async () => {
    render(<WorkflowBuilder value={{ schema_version: "1", trigger: { id: "trigger", type: "manual", config: {} }, steps: [
      { id: "filter", type: "filter", config: { field: "read.count", operator: "gt", value: 0 } },
      { id: "view", type: "view.show_and_wait", config: { view_id: "approve" } },
    ] }} onChange={vi.fn()} />);
    await userEvent.click(screen.getByRole("button", { name: "Configure Continue only if…" }));
    expect(screen.getByLabelText("Source field")).toHaveValue("read.count");
    expect(screen.getByLabelText("Condition")).toHaveValue("gt");
    await userEvent.click(screen.getByRole("button", { name: "Configure Show a view and wait" }));
    expect(screen.getByLabelText("View ID")).toHaveValue("approve");
  });

  it("configures schedule, webhook, data-event, manual, and chat triggers", async () => {
    const onChange = vi.fn();
    const { rerender, container } = render(<WorkflowBuilder value={{ schema_version: "1", trigger: { id: "trigger", type: "schedule", config: { cron: "0 8 * * *" } }, steps: [] }} onChange={onChange} />);
    const { getByLabelText } = within(container);
    expect(getByLabelText("Schedule (UTC)")).toHaveValue("0 8 * * *");
    await userEvent.selectOptions(getByLabelText("Start when"), "data_event");
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ trigger: expect.objectContaining({ type: "data_event", config: {} }) }));

    rerender(<WorkflowBuilder value={{ schema_version: "1", trigger: { id: "trigger", type: "data_event", config: { resource_id: "products" } }, steps: [] }} onChange={onChange} />);
    expect(getByLabelText("Registry resource ID")).toHaveValue("products");

    rerender(<WorkflowBuilder value={{ schema_version: "1", trigger: { id: "trigger", type: "webhook", config: { token: "secret" } }, steps: [] }} onChange={onChange} />);
    expect(getByLabelText("Webhook token")).toHaveValue("secret");
    expect(getByLabelText("Start when")).toHaveTextContent("When started manually");
    expect(getByLabelText("Start when")).toHaveTextContent("When requested in chat");
  });
});
