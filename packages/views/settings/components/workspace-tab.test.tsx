import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const mockInvalidateQueries = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const workspaceRef = vi.hoisted(() => ({
  current: {
    id: "workspace-1",
    name: "Test Workspace",
    slug: "test-workspace",
    description: "",
    context: "",
    issue_prefix: "TES",
    repos: [] as { url: string }[],
  },
}));
const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as "owner" | "admin" | "member" }],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: membersRef.current, isFetched: true }),
  useQueryClient: () => ({
    setQueryData: vi.fn(),
    getQueryData: vi.fn(() => []),
    invalidateQueries: mockInvalidateQueries,
  }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => workspaceRef.current,
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => "/",
}));

vi.mock("@multica/core/platform", () => ({
  setCurrentWorkspace: vi.fn(),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
  workspaceListOptions: () => ({ queryKey: ["workspaces"], queryFn: vi.fn() }),
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueKeys: { all: (workspaceId: string) => ["issues", workspaceId] },
}));

vi.mock("@multica/core/workspace/mutations", () => ({
  useLeaveWorkspace: () => ({ mutateAsync: vi.fn() }),
  useDeleteWorkspace: () => ({ mutateAsync: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    updateWorkspace: mockUpdateWorkspace,
    getBaseUrl: () => "http://127.0.0.1:8080",
  },
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (selector?: (state: { user: { id: string } }) => unknown) =>
      selector ? selector({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

vi.mock("./delete-workspace-dialog", () => ({
  DeleteWorkspaceDialog: () => null,
}));

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: vi.fn() },
}));

import { WorkspaceTab } from "./workspace-tab";

const TEST_RESOURCES = {
  en: { common: enCommon, settings: enSettings },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

describe("WorkspaceTab — automatic updates", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    workspaceRef.current = {
      id: "workspace-1",
      name: "Test Workspace",
      slug: "test-workspace",
      description: "",
      context: "",
      issue_prefix: "TES",
      repos: [],
    };
    membersRef.current = [{ user_id: "user-1", role: "owner" }];
    mockUpdateWorkspace.mockImplementation(
      async (_id: string, payload: Record<string, unknown>) => ({
        ...workspaceRef.current,
        ...payload,
        issue_prefix:
          (payload.issue_prefix as string | undefined) ?? workspaceRef.current.issue_prefix,
      }),
    );
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  function setupUser() {
    return userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
  }

  it("renders the current prefix in the shared input control", () => {
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;
    expect(input.value).toBe("TES");
    expect(screen.queryByRole("button", { name: /^Save$/ })).toBeNull();
  });

  it("renders the workspace slug in the shared read-only input control", () => {
    render(<WorkspaceTab />, { wrapper: I18nWrapper });

    const input = screen.getByRole("textbox", { name: "Slug" }) as HTMLInputElement;
    expect(input.value).toBe("test-workspace");
    expect(input.readOnly).toBe(true);
  });

  it("uppercases and strips non-alphanumeric prefix input", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;

    await user.clear(input);
    await user.type(input, "ab-12!cd");

    expect(input.value).toBe("AB12CD");
  });

  it("auto-saves ordinary workspace fields without invalidating issue caches", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const nameInput = screen.getByDisplayValue("Test Workspace");

    await user.clear(nameInput);
    await user.type(nameInput, "Renamed Workspace");
    await user.tab();

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        name: "Renamed Workspace",
        description: "",
        context: "",
      });
      expect(mockToastSuccess).toHaveBeenCalledWith(
        "Workspace settings saved",
        { id: "settings-auto-save" },
      );
    });
    expect(mockInvalidateQueries).not.toHaveBeenCalled();
  });

  it("asks for confirmation on prefix blur and persists only after confirmation", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;

    await user.clear(input);
    await user.type(input, "NEW");
    await user.tab();

    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
    await screen.findByText(/Change issue prefix/i);
    expect(screen.getByText(/TES-N/)).toBeTruthy();
    expect(screen.getByText(/NEW-N/)).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        issue_prefix: "NEW",
      });
    });
    expect(mockInvalidateQueries).toHaveBeenCalledWith({
      queryKey: ["issues", "workspace-1"],
    });
    expect(mockToastSuccess).toHaveBeenCalledWith(
      "Workspace settings saved",
      { id: "settings-auto-save" },
    );
  });

  it("does not persist a prefix when the confirmation is cancelled", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;

    await user.clear(input);
    await user.type(input, "NEW");
    await user.tab();
    await screen.findByText(/Change issue prefix/i);
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
    expect(input.value).toBe("NEW");
  });

  it("marks an empty prefix invalid and does not persist it", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;

    await user.clear(input);
    await user.tab();

    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
  });

  it("disables editable workspace controls for regular members", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    render(<WorkspaceTab />, { wrapper: I18nWrapper });

    expect(screen.getByPlaceholderText("TES")).toBeDisabled();
    expect(screen.getByDisplayValue("Test Workspace")).toBeDisabled();
  });
});
