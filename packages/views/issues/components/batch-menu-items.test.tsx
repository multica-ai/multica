import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { DropdownMenu, DropdownMenuContent } from "@multica/ui/components/ui/dropdown-menu";
import { commonIssueFields } from "@multica/core/issues/batch";
import type { Issue, IssueProperty } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { dropdownPrimitives } from "../../common/menu-primitives";
import { BatchMenuItems } from "./batch-menu-items";

const PROPERTIES = vi.hoisted(
  () => [{ id: "notes", name: "Notes", type: "text", config: {}, archived: false }] as IssueProperty[],
);

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: PROPERTIES }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ getShareableUrl: (path: string) => `https://app.example${path}` }),
}));

function issue(id: string, notes?: string): Issue {
  return { id, identifier: id, properties: notes === undefined ? {} : { notes } } as Issue;
}

function renderMenu(issues: Issue[]) {
  const onProperties = vi.fn();
  const onEditor = vi.fn();
  renderWithI18n(
    <DropdownMenu open>
      <DropdownMenuContent>
        <BatchMenuItems
          primitives={dropdownPrimitives}
          variant="more"
          issues={issues}
          common={commonIssueFields(issues)}
          projectId={null}
          allowedStatuses={null}
          disabled={false}
          canDelete
          onStatus={vi.fn()}
          onAssignee={vi.fn()}
          onUpdate={vi.fn()}
          onLabels={vi.fn().mockResolvedValue(true)}
          onProperties={onProperties}
          onEditor={onEditor}
          onSetParent={vi.fn()}
          onDelete={vi.fn()}
          onClose={vi.fn()}
        />
      </DropdownMenuContent>
    </DropdownMenu>,
  );
  fireEvent.click(screen.getByRole("menuitem", { name: "Custom properties" }));
  return { onProperties, onEditor };
}

describe("BatchMenuItems custom properties", () => {
  it("clears a text property whose values differ across the selection", async () => {
    const { onProperties } = renderMenu([issue("a", "alpha"), issue("b", "beta"), issue("c")]);
    fireEvent.click(await screen.findByRole("menuitem", { name: "Notes" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Clear (2 set)" }));
    expect(onProperties).toHaveBeenCalledWith({ unset: ["notes"] });
  });

  it("still offers the editor next to the clear action", async () => {
    const { onEditor } = renderMenu([issue("a", "alpha"), issue("b", "beta")]);
    fireEvent.click(await screen.findByRole("menuitem", { name: "Notes" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Set value…" }));
    expect(onEditor).toHaveBeenCalledWith({ kind: "property", property: PROPERTIES[0] });
  });

  it("opens the editor directly when no selected issue has a value", async () => {
    const { onEditor } = renderMenu([issue("a"), issue("b")]);
    fireEvent.click(await screen.findByRole("menuitem", { name: "Notes" }));
    expect(onEditor).toHaveBeenCalledWith({ kind: "property", property: PROPERTIES[0] });
    expect(screen.queryByRole("menuitem", { name: /Clear/ })).not.toBeInTheDocument();
  });
});
