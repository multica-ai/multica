// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useCerebroTasksStore } from "../core/store";
import { TasksPage } from "./tasks-page";

const queryKeys: unknown[][] = [];
const silentReplace = vi.fn();
const navigation = {
  pathname: "/acme/tasks",
  searchParams: new URLSearchParams(),
  replace: vi.fn(),
  replaceSilent: silentReplace as ((path: string) => void) | undefined,
  push: vi.fn(),
  back: vi.fn(),
  getShareableUrl: (path: string) => path,
};

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: (options: { queryKey: readonly unknown[] }) => {
      queryKeys.push([...options.queryKey]);
      return {
        data: options.queryKey[3] === "list" ? { tasks: [], total: 0 } : [],
        isLoading: false,
        isFetching: false,
        isError: false,
        dataUpdatedAt: 0,
      };
    },
  };
});

vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => true }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme" }) }));
vi.mock("@multica/views/navigation", () => ({ useNavigation: () => navigation }));
vi.mock("@multica/views/layout/page-header", () => ({
  PageHeader: ({ children }: { children: React.ReactNode }) => <header>{children}</header>,
}));
vi.mock("@multica/core/workspace/queries", () => ({ agentListOptions: () => ({ queryKey: ["agents"] }) }));
vi.mock("@multica/core/projects", () => ({ projectListOptions: () => ({ queryKey: ["projects"] }) }));
vi.mock("@multica/core/issues/queries", () => ({ issueListOptions: () => ({ queryKey: ["issues"] }) }));
vi.mock("./components/tasks-table", () => ({ TasksTable: () => <div>Tasks table</div> }));
vi.mock("./components/tasks-pagination", () => ({ TasksPagination: () => null }));

beforeEach(() => {
  queryKeys.length = 0;
  navigation.searchParams = new URLSearchParams();
  navigation.replace.mockReset();
  silentReplace.mockReset();
  navigation.replaceSilent = silentReplace;
  useCerebroTasksStore.getState().reset();
});

afterEach(cleanup);

describe("TasksPage", () => {
  it("applies a status filter through a silent URL update", async () => {
    render(<TasksPage />);

    fireEvent.click(screen.getByRole("radio", { name: "Queued" }));

    await waitFor(() => {
      expect(screen.getByRole("radio", { name: "Queued" }).getAttribute("aria-checked")).toBe("true");
    });
    expect(silentReplace).toHaveBeenCalledWith("/acme/tasks?status=queued");
    expect(navigation.replace).not.toHaveBeenCalled();
    expect(queryKeys).toContainEqual([
      "cerebro",
      "tasks",
      "ws-1",
      "list",
      null,
      null,
      null,
      "queued",
      null,
      "24h",
      null,
      null,
      "",
      50,
      0,
    ]);
  });

  it("falls back to route replacement when silent updates are unavailable", async () => {
    navigation.replaceSilent = undefined;
    render(<TasksPage />);

    fireEvent.click(screen.getByRole("radio", { name: "Running" }));

    await waitFor(() => {
      expect(navigation.replace).toHaveBeenCalledWith("/acme/tasks?status=running");
    });
  });
});
