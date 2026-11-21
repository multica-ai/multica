import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import type { Issue, Label } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";
import { BatchLabelPicker } from "./batch-label-picker";

const LABELS = vi.hoisted(() =>
  ["bug", "frontend", "docs"].map((name) => ({ id: `l-${name}`, name, color: "blue" })),
);

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: LABELS }),
}));

function issue(id: string, labelNames: string[]): Issue {
  return { id, labels: LABELS.filter((label) => labelNames.includes(label.name)) as Label[] } as Issue;
}

function renderPicker(issues: Issue[], onApply = vi.fn().mockResolvedValue(true)) {
  const onOpenChange = vi.fn();
  const picker = () => (
    <BatchLabelPicker issues={issues} onApply={onApply} open onOpenChange={onOpenChange} trigger="Labels" />
  );
  const view = renderWithI18n(picker());
  const row = (name: string) => screen.getByText(name).closest("button")!;
  const box = (name: string) => row(name).querySelector("[data-state]")!;
  return { onApply, onOpenChange, row, box, rerender: () => view.rerender(picker()), unmount: view.unmount };
}

describe("BatchLabelPicker", () => {
  const issues = [issue("a", ["bug", "frontend"]), issue("b", ["bug"]), issue("c", [])];

  it("shows whether all, some or none of the selection has each label", () => {
    const { row, box } = renderPicker(issues);
    expect(box("bug")).toHaveAttribute("data-state", "some");
    expect(row("bug")).toHaveTextContent("2/3");
    expect(box("frontend")).toHaveAttribute("data-state", "some");
    expect(box("docs")).toHaveAttribute("data-state", "none");
    expect(row("docs")).not.toHaveTextContent("/3");
  });

  it("adds a partial label to every issue, and removes one every issue has", async () => {
    const { onApply, row, box } = renderPicker(issues);
    fireEvent.click(row("bug"));
    expect(onApply).toHaveBeenLastCalledWith({ add: ["l-bug"] });
    // Held as applied until realtime brings the updated issues.
    expect(box("bug")).toHaveAttribute("data-state", "all");
    // Confirmed: the next click on the now-complete label removes it.
    await act(async () => {});

    fireEvent.click(row("bug"));
    expect(onApply).toHaveBeenLastCalledWith({ remove: ["l-bug"] });
    expect(box("bug")).toHaveAttribute("data-state", "none");
  });

  it("restores the real counts when a write does not fully apply, so a retry adds again", async () => {
    for (const outcome of [() => Promise.resolve(false), () => Promise.reject(new Error("network"))]) {
      const onApply = vi.fn(outcome);
      const { row, box, rerender, unmount } = renderPicker(issues, onApply);
      fireEvent.click(row("bug"));
      await waitFor(() => expect(box("bug")).toHaveAttribute("data-state", "some"));
      // An unchanged refetch keeps the same issues reference.
      rerender();
      expect(row("bug")).toHaveTextContent("2/3");
      fireEvent.click(row("bug"));
      expect(onApply).toHaveBeenLastCalledWith({ add: ["l-bug"] });
      unmount();
    }
  });

  it("ignores another click on a label while its write is in flight", () => {
    const onApply = vi.fn(() => new Promise<boolean>(() => {}));
    const { row } = renderPicker(issues, onApply);
    fireEvent.click(row("bug"));
    fireEvent.click(row("bug"));
    expect(onApply).toHaveBeenCalledTimes(1);
    expect(onApply).toHaveBeenCalledWith({ add: ["l-bug"] });
  });

  it("clears every label from the selection from the footer", () => {
    const { onApply, onOpenChange } = renderPicker(issues);
    fireEvent.click(screen.getByRole("button", { name: /Remove all labels/ }));
    expect(onApply).toHaveBeenCalledWith({ clear: true });
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("offers no clear action when no selected issue has a label", () => {
    renderPicker([issue("a", []), issue("b", [])]);
    expect(screen.queryByRole("button", { name: /Remove all labels/ })).not.toBeInTheDocument();
  });
});
