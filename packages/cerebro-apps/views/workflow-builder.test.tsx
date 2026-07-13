// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { WorkflowBuilder } from "./workflow-builder";

describe("WorkflowBuilder", () => {
  it("renders a vertical trigger-to-step chain and emits the same JSON contract", async () => {
    const onChange = vi.fn();
    render(<WorkflowBuilder value={{ schema_version: "1", trigger: { id: "trigger", type: "manual", config: {} }, steps: [] }} onChange={onChange} />);
    expect(screen.getByText("When started manually")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Add step" }));
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ steps: [expect.objectContaining({ type: "registry.read" })] }));
  });
});
