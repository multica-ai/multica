import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enIssues from "../../locales/en/issues.json";
import enCommon from "../../locales/en/common.json";

const state = vi.hoisted(() => ({
  enabled: false,
  query: vi.fn(),
  taskId: undefined as string | undefined,
}));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({
    settings: { code_changes_enabled: state.enabled },
  }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/issues", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/issues")>(
    "@multica/core/issues",
  );
  return {
    issueCodeChangesOptions: (_ws: string, _issue: string, taskId?: string) => {
      state.taskId = taskId;
      return { queryKey: ["code-changes"] };
    },
    parseDiffLines: actual.parseDiffLines,
  };
});
vi.mock("@tanstack/react-query", () => ({ useQuery: () => state.query() }));
import { CodeChangesSection } from "./code-changes-section";
const snapshot = {
  version: 1,
  baseRef: "aaaaaa",
  headRef: "bbbbbb",
  workingTreeDirty: true,
  truncated: false,
  files: [
    {
      path: "src/app.go",
      status: "M",
      additions: 1,
      deletions: 1,
      binary: false,
      truncated: false,
      diff: "@@ -1 +1 @@\n-old\n+new",
    },
  ],
};
function mount() {
  return render(
    <I18nProvider
      locale="en"
      resources={{ en: { issues: enIssues, common: enCommon } }}
    >
      <CodeChangesSection issueId="issue" />
    </I18nProvider>,
  );
}
beforeEach(() => {
  state.enabled = false;
  state.taskId = undefined;
  state.query.mockReset();
  state.query.mockReturnValue({
    data: {
      available: true,
      sourceTaskId: "run-a",
      snapshot,
      runs: [
        {
          taskId: "run-a",
          createdAt: "2026-10-25T00:00:00Z",
          fileCount: 1,
          status: "completed",
        },
        {
          taskId: "run-b",
          createdAt: "2026-10-25T00:01:00Z",
          fileCount: 0,
          status: "completed",
        },
      ],
    },
    isPending: false,
    refetch: vi.fn(),
  });
});
describe("code changes sidebar", () => {
  it("does not query or display code while disabled", () => {
    mount();
    expect(state.query).not.toHaveBeenCalled();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
  it("opens the file tree and numbered diff, selects a saved execution, and closes", async () => {
    state.enabled = true;
    const user = userEvent.setup();
    mount();
    await user.click(screen.getByRole("button", { name: "Browse changes" }));
    expect(
      screen.getByRole("button", { name: "src/app.go" }),
    ).toBeInTheDocument();
    expect(screen.getByText("+new")).toBeInTheDocument();
    expect(screen.getByText(enIssues.code_changes.dirty)).toBeInTheDocument();
    await user.selectOptions(
      screen.getByRole("combobox", { name: "Execution history" }),
      "run-b",
    );
    expect(state.taskId).toBe("run-b");
    await user.click(screen.getByRole("button", { name: "Close changes" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("shows a retry action on query failure", async () => {
    state.enabled = true;
    const refetch = vi.fn();
    state.query.mockReturnValue({
      error: new Error("offline"),
      isPending: false,
      refetch,
    });
    const user = userEvent.setup();
    mount();
    await user.click(screen.getByRole("button", { name: "Browse changes" }));
    await user.click(screen.getByRole("button", { name: "Retry" }));
    expect(refetch).toHaveBeenCalledOnce();
  });
});
