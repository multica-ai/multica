import { describe, it, expect, vi } from "vitest";
import { screen, fireEvent } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { WebhookEventFilterSection } from "./webhook-event-filter-section";
import type { WebhookEventFilter } from "@multica/core/types";

describe("WebhookEventFilterSection", () => {
  it("renders existing filters with their actions", () => {
    const filters: WebhookEventFilter[] = [
      { event: "workflow_run", actions: ["completed", "requested"] },
      { event: "push" },
    ];
    renderWithI18n(<WebhookEventFilterSection filters={filters} onChange={vi.fn()} />);

    expect(screen.getByText("workflow_run")).toBeInTheDocument();
    expect(screen.getByText(": completed, requested")).toBeInTheDocument();
    expect(screen.getByText("push")).toBeInTheDocument();
  });

  it("adds a filter parsing comma-separated actions", () => {
    const onChange = vi.fn();
    renderWithI18n(<WebhookEventFilterSection filters={[]} onChange={onChange} />);

    fireEvent.change(screen.getByPlaceholderText("e.g. workflow_run"), {
      target: { value: "workflow_run" },
    });
    fireEvent.change(screen.getByPlaceholderText("completed, failed"), {
      target: { value: "completed, failed" },
    });
    fireEvent.keyDown(screen.getByPlaceholderText("e.g. workflow_run"), { key: "Enter" });

    expect(onChange).toHaveBeenCalledWith([
      { event: "workflow_run", actions: ["completed", "failed"] },
    ]);
  });

  it("omits the actions key when no actions are provided", () => {
    const onChange = vi.fn();
    renderWithI18n(<WebhookEventFilterSection filters={[]} onChange={onChange} />);

    fireEvent.change(screen.getByPlaceholderText("e.g. workflow_run"), {
      target: { value: "push" },
    });
    fireEvent.keyDown(screen.getByPlaceholderText("e.g. workflow_run"), { key: "Enter" });

    expect(onChange).toHaveBeenCalledWith([{ event: "push" }]);
  });

  it("removes a filter by index", () => {
    const onChange = vi.fn();
    const filters: WebhookEventFilter[] = [{ event: "push" }, { event: "workflow_run" }];
    const { container } = renderWithI18n(
      <WebhookEventFilterSection filters={filters} onChange={onChange} />,
    );

    // Each filter row has a remove button; click the first one.
    const firstRemove = container.querySelector("button");
    expect(firstRemove).not.toBeNull();
    fireEvent.click(firstRemove!);

    expect(onChange).toHaveBeenCalledWith([{ event: "workflow_run" }]);
  });
});
