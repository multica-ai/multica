// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { Space } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enProjects from "../../locales/en/projects.json";
import enSpaces from "../../locales/en/spaces.json";

const TEST_RESOURCES = {
  en: { common: enCommon, projects: enProjects, spaces: enSpaces },
};

// ApiError mirrors the production export so the dialog's `instanceof` check
// in its catch block matches the class identity the mocked mutation throws.
// vi.hoisted is required because vi.mock is hoisted above imports.
const { ApiError, mutateAsync } = vi.hoisted(() => {
  class ApiError extends Error {
    status: number;
    body: unknown;
    constructor(message: string, status: number, body: unknown) {
      super(message);
      this.status = status;
      this.body = body;
    }
  }
  return { ApiError, mutateAsync: vi.fn() };
});

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return { ...actual, ApiError };
});

vi.mock("@multica/core/projects/mutations", () => ({
  useUpdateProject: () => ({ isPending: false, mutateAsync }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const SPACE_A: Space = {
  id: "space-a",
  workspace_id: "ws-1",
  name: "Engineering",
  key: "ENG",
  description: "",
  icon: null,
  issue_counter: 5,
  archived_at: null,
  created_by: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  is_member: true,
  sort_order: 0,
};

const SPACE_B: Space = { ...SPACE_A, id: "space-b", name: "Design", key: "DES", sort_order: 1 };

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>("@tanstack/react-query");
  return {
    ...actual,
    // Both this dialog's own space list and SpaceMultiPicker's internal one
    // read the same activeSpaceListOptions query key (tail "list").
    useQuery: vi.fn(() => ({ data: [SPACE_A, SPACE_B], isLoading: false })),
  };
});

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { toast } from "sonner";
import { ManageProjectSpacesDialog } from "./manage-project-spaces-dialog";

function renderDialog(opts: { spaceIds?: string[] } = {}) {
  const onOpenChange = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ManageProjectSpacesDialog
        open
        onOpenChange={onOpenChange}
        wsId="ws-1"
        projectId="project-1"
        spaceIds={opts.spaceIds ?? [SPACE_A.id, SPACE_B.id]}
      />
    </I18nProvider>,
  );
  return { onOpenChange };
}

beforeEach(() => {
  mutateAsync.mockReset();
  vi.mocked(toast.success).mockClear();
  vi.mocked(toast.error).mockClear();
});

describe("ManageProjectSpacesDialog", () => {
  it("saves the unchanged space set directly when the server reports no conflict", async () => {
    mutateAsync.mockResolvedValueOnce({});
    const { onOpenChange } = renderDialog();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith({
        id: "project-1",
        space_ids: [SPACE_A.id, SPACE_B.id],
      });
    });
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(toast.success).toHaveBeenCalledTimes(1);
  });

  it("disables Save and shows a hint when no space is selected", () => {
    renderDialog({ spaceIds: [] });
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getByText("Select at least one space.")).toBeInTheDocument();
  });

  it("switches to the move-issues step on a 409 project_space_has_issues conflict", async () => {
    mutateAsync.mockRejectedValueOnce(
      new ApiError("conflict", 409, {
        code: "project_space_has_issues",
        error: "project has issues in a space being removed",
        spaces_with_issues: [{ space_id: SPACE_A.id, space_key: SPACE_A.key, issue_count: 3 }],
      }),
    );
    renderDialog();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Move issues before removing a space")).toBeInTheDocument();
    expect(screen.getByText("3 issues in ENG")).toBeInTheDocument();
    // The only other selected space is offered as the default move target.
    expect(screen.getByText("Design")).toBeInTheDocument();
  });

  it("resubmits with space_reassignments after confirming the move target", async () => {
    mutateAsync.mockRejectedValueOnce(
      new ApiError("conflict", 409, {
        code: "project_space_has_issues",
        error: "project has issues in a space being removed",
        spaces_with_issues: [{ space_id: SPACE_A.id, space_key: SPACE_A.key, issue_count: 3 }],
      }),
    );
    mutateAsync.mockResolvedValueOnce({});
    const { onOpenChange } = renderDialog();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByText("Move issues before removing a space");

    fireEvent.click(screen.getByRole("button", { name: "Move issues and save" }));

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenLastCalledWith({
        id: "project-1",
        space_ids: [SPACE_A.id, SPACE_B.id],
        space_reassignments: { [SPACE_A.id]: SPACE_B.id },
      });
    });
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it("falls back to a generic error toast when the 409 body doesn't match the schema", async () => {
    mutateAsync.mockRejectedValueOnce(new ApiError("conflict", 409, { unexpected: true }));
    renderDialog();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1));
    expect(screen.queryByText("Move issues before removing a space")).not.toBeInTheDocument();
  });
});
