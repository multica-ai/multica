import { describe, it, expect, vi } from "vitest";
import { screen, fireEvent } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { WebhookEventFilterSection } from "./webhook-event-filter-section";

// The two icon-only controls in this section used to render with no
// accessible name at all, so a screen reader announced a bare "button" and
// sighted users had nothing telling them the row still had to be committed.
// getByRole(..., { name }) is the assertion that keeps them named.

describe("WebhookEventFilterSection", () => {
  it("names the add control instead of shipping a bare + icon", () => {
    renderWithI18n(<WebhookEventFilterSection filters={[]} onChange={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Add" })).toBeInTheDocument();
  });

  it("names the remove control on a committed row", () => {
    renderWithI18n(
      <WebhookEventFilterSection
        filters={[{ event: "workflow_run", actions: ["completed"] }]}
        onChange={vi.fn()}
      />,
    );
    expect(
      screen.getByRole("button", { name: "Remove filter" }),
    ).toBeInTheDocument();
  });

  it("keeps add disabled until an event is typed", () => {
    renderWithI18n(<WebhookEventFilterSection filters={[]} onChange={vi.fn()} />);
    const add = screen.getByRole("button", { name: "Add" });
    expect(add).toBeDisabled();

    fireEvent.change(screen.getByPlaceholderText("e.g. workflow_run"), {
      target: { value: "workflow_run" },
    });
    expect(add).toBeEnabled();
  });

  it("stays disabled for whitespace-only input", () => {
    renderWithI18n(<WebhookEventFilterSection filters={[]} onChange={vi.fn()} />);
    fireEvent.change(screen.getByPlaceholderText("e.g. workflow_run"), {
      target: { value: "   " },
    });
    expect(screen.getByRole("button", { name: "Add" })).toBeDisabled();
  });

  it("commits the row through onChange when add is clicked", () => {
    const onChange = vi.fn();
    renderWithI18n(<WebhookEventFilterSection filters={[]} onChange={onChange} />);
    fireEvent.change(screen.getByPlaceholderText("e.g. workflow_run"), {
      target: { value: "workflow_run" },
    });
    fireEvent.change(screen.getByPlaceholderText("completed, failed"), {
      target: { value: "completed, failed" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(onChange).toHaveBeenCalledWith([
      { event: "workflow_run", actions: ["completed", "failed"] },
    ]);
  });

  it("drops the row from the list when remove is clicked", () => {
    const onChange = vi.fn();
    renderWithI18n(
      <WebhookEventFilterSection
        filters={[{ event: "issues" }, { event: "workflow_run" }]}
        onChange={onChange}
      />,
    );
    fireEvent.click(
      screen.getAllByRole("button", { name: "Remove filter" })[1]!,
    );
    expect(onChange).toHaveBeenCalledWith([{ event: "issues" }]);
  });
});
