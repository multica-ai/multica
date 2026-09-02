import { describe, it, expect, vi } from "vitest";
import { screen, fireEvent, act } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import type { WebhookEventFilter } from "@multica/core/types";
import { WebhookEventFilterSection } from "./webhook-event-filter-section";

// flushDraft() is the imperative handle the dialog's Save handler calls so
// that text typed into the event/actions inputs — but not yet committed via
// Enter or the + button — is persisted instead of silently discarded. These
// tests pin that contract independently of the accessible-name coverage in
// webhook-event-filter-section.test.tsx.

describe("WebhookEventFilterSection.flushDraft", () => {
  it("commits an uncommitted draft row and returns the new filters", () => {
    const onChange = vi.fn();
    const ref = {
      current: null as null | { flushDraft: () => WebhookEventFilter[] },
    };
    renderWithI18n(
      <WebhookEventFilterSection
        filters={[]}
        onChange={onChange}
        ref={ref as never}
      />,
    );

    const eventInput = screen.getByPlaceholderText("e.g. workflow_run");
    const actionsInput = screen.getByPlaceholderText("completed, failed");
    fireEvent.change(eventInput, { target: { value: "workflow_run" } });
    fireEvent.change(actionsInput, { target: { value: "completed, failed" } });

    // No commit yet — typing alone must not call onChange.
    expect(onChange).not.toHaveBeenCalled();

    // flushDraft is what Save calls to persist a draft row that was never
    // committed via Enter or the + button. It returns the resulting filters
    // so the dialog avoids a stale-closure read of parent state.
    let result: WebhookEventFilter[] | undefined;
    act(() => {
      result = ref.current?.flushDraft();
    });
    expect(result).toEqual([
      { event: "workflow_run", actions: ["completed", "failed"] },
    ]);
    expect(onChange).toHaveBeenCalledWith([
      { event: "workflow_run", actions: ["completed", "failed"] },
    ]);
    // Inputs were cleared after the flush.
    expect(eventInput).toHaveValue("");
    expect(actionsInput).toHaveValue("");
  });

  it("is a no-op returning current filters when the event field is empty", () => {
    const onChange = vi.fn();
    const ref = {
      current: null as null | { flushDraft: () => WebhookEventFilter[] },
    };
    renderWithI18n(
      <WebhookEventFilterSection
        filters={[{ event: "issues" }]}
        onChange={onChange}
        ref={ref as never}
      />,
    );

    expect(ref.current?.flushDraft()).toEqual([{ event: "issues" }]);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("flushes a draft on top of existing filters and clears the input", () => {
    const onChange = vi.fn();
    const ref = {
      current: null as null | { flushDraft: () => WebhookEventFilter[] },
    };
    renderWithI18n(
      <WebhookEventFilterSection
        filters={[{ event: "issues" }]}
        onChange={onChange}
        ref={ref as never}
      />,
    );

    fireEvent.change(screen.getByPlaceholderText("e.g. workflow_run"), {
      target: { value: "workflow_run" },
    });

    let result: WebhookEventFilter[] | undefined;
    act(() => {
      result = ref.current?.flushDraft();
    });
    expect(result).toEqual([{ event: "issues" }, { event: "workflow_run" }]);
    expect(onChange).toHaveBeenCalledTimes(1);
    // flushDraft cleared the draft input, so the event field is empty again.
    expect(screen.getByPlaceholderText("e.g. workflow_run")).toHaveValue("");
  });
});
