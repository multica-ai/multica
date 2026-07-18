import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const workspaceRef = vi.hoisted(() => ({
  current: {
    id: "workspace-1",
    name: "Test Workspace",
    slug: "test-workspace",
    repos: [{ url: "https://github.com/multica-ai/multica" }] as {
      url: string;
      description?: string;
    }[],
  },
}));
const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as "owner" | "admin" | "member" }],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: membersRef.current }),
  useQueryClient: () => ({ setQueryData: vi.fn() }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => workspaceRef.current,
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/core/api", () => ({
  api: { updateWorkspace: mockUpdateWorkspace },
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (selector?: (state: { user: { id: string } }) => unknown) =>
      selector ? selector({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: vi.fn() },
}));

import { RepositoriesTab } from "./repositories-tab";

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

describe("RepositoriesTab — automatic updates", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    workspaceRef.current = {
      id: "workspace-1",
      name: "Test Workspace",
      slug: "test-workspace",
      repos: [{ url: "https://github.com/multica-ai/multica" }],
    };
    membersRef.current = [{ user_id: "user-1", role: "owner" }];
    mockUpdateWorkspace.mockImplementation(
      async (_id: string, payload: { repos: { url: string; description?: string }[] }) => ({
        ...workspaceRef.current,
        repos: payload.repos,
      }),
    );
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  function setupUser() {
    return userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
  }

  it("renders persisted repositories as the same shared input controls used for editing", () => {
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    const inputs = screen.getAllByRole("textbox") as HTMLInputElement[];
    expect(inputs).toHaveLength(2);
    expect(inputs[0]!.value).toBe("https://github.com/multica-ai/multica");
    expect(screen.queryByRole("button", { name: /^Save$/ })).toBeNull();
  });

  it("updates a changed URL automatically on blur", async () => {
    const user = setupUser();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    const urlInput = screen.getAllByRole("textbox")[0]!;
    await user.clear(urlInput);
    await user.type(urlInput, "https://github.com/multica-ai/edited");
    await user.tab();

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        repos: [{ url: "https://github.com/multica-ai/edited" }],
      });
      expect(mockToastSuccess).toHaveBeenCalledWith("Repositories saved", {
        id: "settings-auto-save",
      });
    });
  });

  it("debounces updates while the user is still typing", async () => {
    const user = setupUser();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    const urlInput = screen.getAllByRole("textbox")[0]!;
    await user.type(urlInput, "-next");
    expect(mockUpdateWorkspace).not.toHaveBeenCalled();

    vi.advanceTimersByTime(650);
    await waitFor(() => expect(mockUpdateWorkspace).toHaveBeenCalledTimes(1));
  });

  it("does not persist a new row until its URL is non-empty", async () => {
    const user = setupUser();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: /Add repository/ }));
    expect(screen.getAllByRole("textbox")).toHaveLength(4);
    vi.advanceTimersByTime(1000);
    expect(mockUpdateWorkspace).not.toHaveBeenCalled();

    const newUrlInput = screen.getAllByRole("textbox")[2]!;
    await user.type(newUrlInput, "git@github.com:multica-ai/second.git");
    await user.tab();

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        repos: [
          { url: "https://github.com/multica-ai/multica" },
          { url: "git@github.com:multica-ai/second.git" },
        ],
      });
    });
  });

  it("persists deletion immediately without a separate save action", async () => {
    const user = setupUser();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: "Delete repository" }));
    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
    await user.click(
      screen.getByRole("button", { name: "Delete repository" }),
    );

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", { repos: [] });
    });
    expect(screen.getByText("No repositories yet.")).toBeTruthy();
  });

  it("accepts scp-like repository shorthand", async () => {
    const user = setupUser();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    const urlInput = screen.getAllByRole("textbox")[0] as HTMLInputElement;
    await user.clear(urlInput);
    await user.type(urlInput, "git@github.com:multica-ai/multica.git");
    expect(urlInput.type).toBe("text");
    expect(urlInput.validity.valid).toBe(true);
    await user.tab();

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        repos: [{ url: "git@github.com:multica-ai/multica.git" }],
      });
    });
  });

  it("includes the description in the automatic update payload", async () => {
    workspaceRef.current = {
      ...workspaceRef.current,
      repos: [{ url: "https://github.com/multica-ai/multica", description: "Main app" }],
    };
    const user = setupUser();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    const descriptionInput = screen.getAllByRole("textbox")[1] as HTMLInputElement;
    expect(descriptionInput.value).toBe("Main app");
    await user.clear(descriptionInput);
    await user.type(descriptionInput, "Updated description");
    await user.tab();

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        repos: [
          {
            url: "https://github.com/multica-ai/multica",
            description: "Updated description",
          },
        ],
      });
    });
  });

  it("keeps repository controls read-only for members", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    expect(screen.getAllByRole("textbox").every((input) => input.hasAttribute("disabled"))).toBe(true);
    expect(screen.queryByRole("button", { name: /Add repository/ })).toBeNull();
  });
});
