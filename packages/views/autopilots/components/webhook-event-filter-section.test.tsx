import { describe, it, expect, vi } from "vitest";
import { screen, fireEvent, within } from "@testing-library/react";
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

  it("lists each payload condition with a readable operator", () => {
    renderWithI18n(
      <WebhookEventFilterSection
        filters={[
          {
            event: "pull_request",
            conditions: [
              { path: "pull_request.draft", op: "equals", value: false },
              { path: "pull_request.base.ref", op: "in", values: ["main", "release"] },
            ],
          },
        ]}
        onChange={vi.fn()}
      />,
    );
    const draft = screen.getByTitle("pull_request.draft equals false");
    expect(draft).toHaveTextContent("pull_request.draftisfalse");
    expect(
      screen.getByTitle('pull_request.base.ref in ["main", "release"]'),
    ).toHaveTextContent('pull_request.base.refis one of"main", "release"');
  });

  it("adds a condition to the filter it was opened on", () => {
    const onChange = vi.fn();
    renderWithI18n(
      <WebhookEventFilterSection
        filters={[{ event: "issues", actions: ["labeled"] }, { event: "push" }]}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getAllByRole("button", { name: "Add condition" })[0]!);
    const add = within(screen.getByRole("group", { name: "Add condition" })).getByRole(
      "button",
      { name: "Add" },
    );
    expect(add).toBeDisabled();

    fireEvent.change(screen.getByRole("textbox", { name: "Payload field" }), {
      target: { value: "label.name" },
    });
    fireEvent.change(screen.getByRole("textbox", { name: "Value" }), {
      target: { value: "agent" },
    });
    expect(add).toBeEnabled();
    fireEvent.click(add);

    expect(onChange).toHaveBeenCalledWith([
      {
        event: "issues",
        actions: ["labeled"],
        conditions: [{ path: "label.name", op: "equals", value: "agent" }],
      },
      { event: "push" },
    ]);
  });

  it("closes the editor on cancel without changing filters", () => {
    const onChange = vi.fn();
    renderWithI18n(
      <WebhookEventFilterSection filters={[{ event: "issues" }]} onChange={onChange} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Add condition" }));
    expect(screen.getByRole("textbox", { name: "Payload field" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("textbox", { name: "Payload field" })).not.toBeInTheDocument();
    expect(onChange).not.toHaveBeenCalled();
  });

  it("removes one condition and drops the key when none are left", () => {
    const onChange = vi.fn();
    renderWithI18n(
      <WebhookEventFilterSection
        filters={[
          { event: "issues", conditions: [{ path: "label.name", op: "equals", value: "agent" }] },
        ]}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Remove condition" }));
    expect(onChange).toHaveBeenCalledWith([{ event: "issues" }]);
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
