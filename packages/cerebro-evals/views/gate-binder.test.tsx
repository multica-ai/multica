// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { GateBinder } from "./gate-binder";

const evals = [{ id: "e1", title: "Answer quality", version: "1.0.0" }];
const workflows = [{ id: "w1", name: "Standard build loop" }];

describe("GateBinder", () => {
  it("renders a phase selector and a Block/Warn toggle", () => {
    render(<GateBinder evals={evals} workflows={workflows} onBind={() => {}} />);
    const phase = screen.getByLabelText("Phase") as HTMLSelectElement;
    expect(Array.from(phase.options).map((o) => o.value)).toEqual(["plan", "delivery", "monitor"]);
    expect(phase.value).toBe("delivery");
    const enforcement = screen.getByLabelText("Enforcement") as HTMLSelectElement;
    expect(Array.from(enforcement.options).map((o) => o.value)).toEqual(["block", "warn"]);
    expect(enforcement.value).toBe("block");
  });

  it("submits the chosen phase and blocking flag", () => {
    const onBind = vi.fn();
    render(<GateBinder evals={evals} workflows={workflows} onBind={onBind} />);
    fireEvent.change(screen.getByLabelText("Eval"), { target: { value: "e1" } });
    fireEvent.change(screen.getByLabelText("Issue workflow"), { target: { value: "w1" } });
    fireEvent.change(screen.getByLabelText("Phase"), { target: { value: "plan" } });
    fireEvent.change(screen.getByLabelText("Enforcement"), { target: { value: "warn" } });
    fireEvent.click(screen.getByRole("button"));
    expect(onBind).toHaveBeenCalledWith({ workflowId: "w1", evalId: "e1", phase: "plan", blocking: false });
  });

  it("keeps the default delivery/Block binding when nothing is changed", () => {
    const onBind = vi.fn();
    render(<GateBinder evals={evals} workflows={workflows} onBind={onBind} />);
    fireEvent.change(screen.getByLabelText("Eval"), { target: { value: "e1" } });
    fireEvent.change(screen.getByLabelText("Issue workflow"), { target: { value: "w1" } });
    fireEvent.click(screen.getByRole("button"));
    expect(onBind).toHaveBeenCalledWith({ workflowId: "w1", evalId: "e1", phase: "delivery", blocking: true });
  });

  it("forces Monitor bindings to Warn only", () => {
    const onBind = vi.fn();
    render(<GateBinder evals={evals} workflows={workflows} onBind={onBind} />);
    fireEvent.change(screen.getByLabelText("Eval"), { target: { value: "e1" } });
    fireEvent.change(screen.getByLabelText("Issue workflow"), { target: { value: "w1" } });
    fireEvent.change(screen.getByLabelText("Phase"), { target: { value: "monitor" } });
    const enforcement = screen.getByLabelText("Enforcement") as HTMLSelectElement;
    expect(enforcement.value).toBe("warn");
    expect(enforcement.disabled).toBe(true);
    fireEvent.click(screen.getByRole("button"));
    expect(onBind).toHaveBeenCalledWith({ workflowId: "w1", evalId: "e1", phase: "monitor", blocking: false });
  });

  it("does not submit without an eval and a workflow", () => {
    const onBind = vi.fn();
    render(<GateBinder evals={evals} workflows={workflows} onBind={onBind} />);
    fireEvent.click(screen.getByRole("button"));
    expect(onBind).not.toHaveBeenCalled();
  });
});
