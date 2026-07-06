// @vitest-environment jsdom

// FIR-2748 — the personal review alert is the single top-of-page entry into
// change review: a personal count plus both controls (open the review sheet,
// toggle the "Pending changes" list filter). It must vanish when the user has
// nothing to review.

import { describe, it, expect, vi, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { SkillChangesAlert } from "./skill-changes-alert";

afterEach(() => cleanup());

function renderAlert(over: Partial<React.ComponentProps<typeof SkillChangesAlert>> = {}) {
  const onReview = vi.fn();
  const onTogglePending = vi.fn();
  render(
    <SkillChangesAlert
      count={3}
      onReview={onReview}
      pendingOnly={false}
      onTogglePending={onTogglePending}
      {...over}
    />,
  );
  return { onReview, onTogglePending };
}

describe("SkillChangesAlert", () => {
  it("renders the personal count and pluralises", () => {
    renderAlert({ count: 3 });
    expect(screen.getByText("3 changes to review")).toBeTruthy();
    expect(screen.getByText("on skills you own.")).toBeTruthy();
  });

  it("uses the singular for a single pending change", () => {
    renderAlert({ count: 1 });
    expect(screen.getByText("1 change to review")).toBeTruthy();
  });

  it("renders nothing when there is nothing to review", () => {
    const { container } = render(
      <SkillChangesAlert
        count={0}
        onReview={vi.fn()}
        pendingOnly={false}
        onTogglePending={vi.fn()}
      />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("opens the review sheet from the alert", () => {
    const { onReview } = renderAlert();
    fireEvent.click(screen.getByText("Review"));
    expect(onReview).toHaveBeenCalledTimes(1);
  });

  it("toggles the pending-changes list filter from the alert", () => {
    const { onTogglePending } = renderAlert({ pendingOnly: false });
    fireEvent.click(screen.getByText("Pending changes"));
    expect(onTogglePending).toHaveBeenCalledTimes(1);
  });
});
