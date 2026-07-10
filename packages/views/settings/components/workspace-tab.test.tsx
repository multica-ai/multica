import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const mockNavigationPush = vi.hoisted(() => vi.fn());
const mockNavigationReplace = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const workspaceRef = vi.hoisted(() => ({
  current: {
    id: "workspace-1",
    name: "Test Workspace",
    slug: "test-workspace",
    description: "",
    context: "",
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
    invalidateQueries: vi.fn(),
  }),
}));

vi.mock("@multica/core/paths", () => ({
  paths: {
    workspace: (slug: string) => ({
      settingsSection: (scope: string, page: string) =>
        `/${slug}/settings/${scope}/${page}`,
    }),
  },
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
  useNavigation: () => ({
    push: mockNavigationPush,
    replace: mockNavigationReplace,
  }),
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
      repos: [],
    };
    membersRef.current = [{ user_id: "user-1", role: "owner" }];
    mockUpdateWorkspace.mockImplementation(
      async (_id: string, payload: Record<string, unknown>) => ({
        ...workspaceRef.current,
        ...payload,
      }),
    );
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  function setupUser() {
    return userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
  }

  it("renders the current slug in the shared input control", () => {
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("test-workspace") as HTMLInputElement;
    expect(input.value).toBe("test-workspace");
    expect(screen.queryByRole("button", { name: /^Save$/ })).toBeNull();
  });

  it("lowercases and strips unsupported slug characters", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("test-workspace") as HTMLInputElement;

    await user.clear(input);
    await user.type(input, "New_Workspace!");

    expect(input.value).toBe("newworkspace");
  });

  it("auto-saves ordinary workspace fields", async () => {
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
  });

  it("asks for confirmation on slug blur, persists, and navigates", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("test-workspace") as HTMLInputElement;

    await user.clear(input);
    await user.type(input, "new-workspace");
    await user.tab();

    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
    await screen.findByText(/Change workspace URL/i);
    expect(screen.getByText(/\/test-workspace/)).toBeTruthy();
    expect(screen.getByText(/\/new-workspace/)).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        slug: "new-workspace",
      });
    });
    expect(mockNavigationReplace).toHaveBeenCalledWith(
      "/new-workspace/settings/workspace/general",
    );
    expect(mockToastSuccess).toHaveBeenCalledWith(
      "Workspace settings saved",
      { id: "settings-auto-save" },
    );
  });

  it("saves the selected workspace default space", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });

    await user.selectOptions(screen.getByLabelText("Default space"), "space-2");

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        default_space_id: "space-2",
      });
    });
  });

  it("does not persist a slug when confirmation is cancelled", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("test-workspace") as HTMLInputElement;

    await user.clear(input);
    await user.type(input, "new-workspace");
    await user.tab();
    await screen.findByText(/Change workspace URL/i);
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
    expect(input.value).toBe("new-workspace");
  });

  it("marks an empty slug invalid and does not persist it", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("test-workspace") as HTMLInputElement;

    await user.clear(input);
    await user.tab();

    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
  });

  it("disables editable workspace controls for regular members", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    render(<WorkspaceTab />, { wrapper: I18nWrapper });

    expect(screen.getByPlaceholderText("test-workspace")).toBeDisabled();
    expect(screen.getByDisplayValue("Test Workspace")).toBeDisabled();
  });
});
