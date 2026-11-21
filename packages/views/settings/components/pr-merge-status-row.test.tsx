import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import type { IssueStatusEntry } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const workspaceRef = vi.hoisted(() => ({
  current: { id: "workspace-1", settings: {} as Record<string, unknown> },
}));
const catalogRef = vi.hoisted(() => ({ current: [] as IssueStatusEntry[] }));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: catalogRef.current, isPending: false, isError: false, refetch: vi.fn() }),
  useQueryClient: () => ({ setQueryData: vi.fn() }),
  queryOptions: <T,>(opts: T) => opts,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => workspaceRef.current }));
vi.mock("@multica/core/api", () => ({ api: { updateWorkspace: mockUpdateWorkspace } }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

import { PRMergeStatusRow } from "./pr-merge-status-row";

const TEST_RESOURCES = {
  en: { common: enCommon, issues: enIssues, settings: enSettings },
};

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function status(key: string, name: string, category: IssueStatusEntry["category"]): IssueStatusEntry {
  return {
    id: key,
    workspace_id: "workspace-1",
    key,
    name,
    description: "",
    category,
    color: "#888888",
    is_system: key !== "qa",
    position: 0,
    archived_at: null,
    created_at: "",
    updated_at: "",
  };
}

const select = () =>
  screen.getByRole("combobox", { name: enSettings.pr_merge_status.label });

beforeEach(() => {
  vi.clearAllMocks();
  workspaceRef.current = { id: "workspace-1", settings: {} };
  catalogRef.current = [
    status("in_review", "In Review", "started"),
    status("blocked", "Blocked", "started"),
    status("qa", "QA", "started"),
    status("done", "Done", "done"),
  ];
});

describe("PRMergeStatusRow", () => {
  it("moves issues to Done by default and shows no change when turned off", () => {
    render(<PRMergeStatusRow canManage />, { wrapper: Wrapper });
    expect(select()).toHaveTextContent("Done");

    cleanup();
    workspaceRef.current.settings = { pr_merge_status: "none" };
    render(<PRMergeStatusRow canManage />, { wrapper: Wrapper });
    expect(select()).toHaveTextContent(enSettings.pr_merge_status.none);
  });

  it("reads a status that is no longer offered as no change", () => {
    workspaceRef.current.settings = { pr_merge_status: "retired" };
    render(<PRMergeStatusRow canManage />, { wrapper: Wrapper });
    expect(select()).toHaveTextContent(enSettings.pr_merge_status.none);
  });

  it("saves the chosen status alongside the other workspace settings", async () => {
    const user = userEvent.setup();
    workspaceRef.current.settings = { github_enabled: true };
    mockUpdateWorkspace.mockResolvedValue({
      ...workspaceRef.current,
      settings: { github_enabled: true, pr_merge_status: "qa" },
    });
    render(<PRMergeStatusRow canManage />, { wrapper: Wrapper });

    await user.click(select());
    const qa = await screen.findByRole("option", { name: "QA" });
    // A merge never blocks an issue.
    expect(screen.queryByRole("option", { name: "Blocked" })).toBeNull();
    await user.click(qa);

    await waitFor(() =>
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        settings: { github_enabled: true, pr_merge_status: "qa" },
      }),
    );
  });

  it("is read-only for members who cannot manage the workspace", () => {
    render(<PRMergeStatusRow canManage={false} />, { wrapper: Wrapper });
    expect(select()).toBeDisabled();
  });
});
