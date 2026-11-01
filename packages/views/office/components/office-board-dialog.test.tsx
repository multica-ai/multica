/**
 * @vitest-environment jsdom
 *
 * The board dialog shows the kanban board for one project or for every issue.
 * The board itself is the shared issue surface, so it is replaced by a stub that
 * reports the scope it was given; these tests cover what the dialog decides: which
 * scope opens, how the picker switches it, and that the columns fit the dialog.
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { projectKeys } from "@multica/core/projects/queries";
import type { Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { OfficeBoardDialog } from "./office-board-dialog";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("../../issues/surface/issue-surface", () => ({
  IssueSurface: ({ scope }: { scope: { type: string; projectId?: string } }) => (
    <div data-testid="surface">{scope.type === "project" ? `project ${scope.projectId}` : "every issue"}</div>
  ),
}));

const project = (id: string, title: string) => ({ id, title, icon: null }) as Project;

function setup(initialProjectId?: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(projectKeys.list("ws-1"), { projects: [project("p1", "Website"), project("p2", "Mobile")], total: 2 });
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <OfficeBoardDialog open initialProjectId={initialProjectId} onClose={() => {}} />
    </QueryClientProvider>,
  );
}

describe("OfficeBoardDialog", () => {
  afterEach(cleanup);

  it("opens on every issue when the page has no project", () => {
    setup();

    expect(screen.getByTestId("surface").textContent).toBe("every issue");
    expect(screen.getByRole("button", { name: "Project" }).textContent).toContain("All projects");
  });

  it("opens on the project the page is on", () => {
    setup("p2");

    expect(screen.getByTestId("surface").textContent).toBe("project p2");
    expect(screen.getByRole("button", { name: "Project" }).textContent).toContain("Mobile");
  });

  it("switches the board with the picker, and back to every issue", async () => {
    const user = userEvent.setup();
    setup();

    await user.click(screen.getByRole("button", { name: "Project" }));
    await user.click(await screen.findByRole("menuitem", { name: /Website/ }));
    expect(screen.getByTestId("surface").textContent).toBe("project p1");

    await user.click(screen.getByRole("button", { name: "Project" }));
    await user.click(await screen.findByRole("menuitem", { name: "All projects" }));
    expect(screen.getByTestId("surface").textContent).toBe("every issue");
  });

  it("shares the dialog's width between the columns instead of scrolling sideways", () => {
    setup();

    expect(screen.getByTestId("surface").parentElement?.className).toContain("flex-1!");
  });
});
