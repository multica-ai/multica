import { createRef } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { QuickCreateFooter } from "./table-view";

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
});
