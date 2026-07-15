import { createRef } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { IssueTableGroupRow, QuickCreateFooter } from "./table-view";

const { createIssue } = vi.hoisted(() => ({
  createIssue: vi.fn(),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useCreateIssue: () => ({
    isPending: false,
    mutate: createIssue,
  }),
}));

describe("QuickCreateFooter", () => {
  beforeEach(() => {
    createIssue.mockClear();
  });

  it("creates an issue when the visible Add button is clicked", async () => {
    const user = userEvent.setup();
    renderWithI18n(
      <table>
        <tfoot>
          <QuickCreateFooter
            colSpan={3}
            createDefaults={{ status: "todo", priority: "high" }}
            sentinelRef={createRef<HTMLDivElement>()}
            loadingMore={false}
          />
        </tfoot>
      </table>,
    );

    const input = screen.getByPlaceholderText("Add an issue…");
    const submit = screen.getByRole("button", { name: "Add" });
    expect(input.closest("form")).toHaveClass("sticky", "left-1.5");
    expect(submit).toBeDisabled();

    await user.type(input, "  Clickable quick create  ");
    await user.click(submit);

    expect(createIssue).toHaveBeenCalledTimes(1);
    expect(createIssue).toHaveBeenCalledWith(
      {
        title: "Clickable quick create",
        status: "todo",
        priority: "high",
      },
      expect.objectContaining({
        onSuccess: expect.any(Function),
        onError: expect.any(Function),
      }),
    );
  });

  it("keeps full-width row controls anchored during horizontal scrolling", async () => {
    const user = userEvent.setup();
    const onToggle = vi.fn();
    renderWithI18n(
      <table>
        <tbody>
          <IssueTableGroupRow
            group={{
              kind: "group",
              key: "status:backlog",
              label: "Backlog",
              count: 13,
              collapsed: false,
            }}
            colSpan={3}
            onToggle={onToggle}
          />
        </tbody>
      </table>,
    );

    const group = screen.getByRole("button", { name: /Backlog\s*13/ });
    expect(group).toHaveClass("sticky", "left-4", "w-fit");

    await user.click(group);
    expect(onToggle).toHaveBeenCalledOnce();
  });
});
