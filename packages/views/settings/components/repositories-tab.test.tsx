import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
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
  toast: { success: mockToastSuccess, error: mockToastError },
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

describe("RepositoriesTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
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

  it("renders persisted repositories as compact summaries instead of inputs", () => {
    workspaceRef.current = {
      ...workspaceRef.current,
      repos: [
        {
          url: "https://github.com/multica-ai/multica",
          description: "Managed agents platform",
        },
      ],
    };

    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByText("https://github.com/multica-ai/multica")).toBeTruthy();
    expect(screen.getByText("Managed agents platform")).toBeTruthy();
  });

  it("edits URL and description in an explicitly submitted dialog", async () => {
    workspaceRef.current = {
      ...workspaceRef.current,
      repos: [
        {
          url: "https://github.com/multica-ai/multica",
          description: "Main app",
        },
      ],
    };
    const user = userEvent.setup();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: "Edit repository" }));
    const urlInput = screen.getByLabelText("Repository URL");
    const descriptionInput = screen.getByLabelText("Description");
    expect(urlInput).toHaveValue("https://github.com/multica-ai/multica");
    expect(descriptionInput).toHaveValue("Main app");

    await user.clear(descriptionInput);
    await user.type(descriptionInput, "Updated description");
    expect(mockUpdateWorkspace).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Save repository" }));

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
    expect(mockToastSuccess).toHaveBeenCalledWith("Repositories saved");
  });

  it("does not persist a new repository until the dialog is submitted", async () => {
    const user = userEvent.setup();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: "Add repository" }));
    const saveButton = screen.getByRole("button", { name: "Save repository" });
    expect(saveButton).toBeDisabled();

    await user.type(
      screen.getByLabelText("Repository URL"),
      "  git@github.com:multica-ai/second.git  ",
    );
    await user.type(screen.getByLabelText("Description"), "  Cloud services  ");
    expect(mockUpdateWorkspace).not.toHaveBeenCalled();

    await user.click(saveButton);

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        repos: [
          { url: "https://github.com/multica-ai/multica" },
          {
            url: "git@github.com:multica-ai/second.git",
            description: "Cloud services",
          },
        ],
      });
    });
  });

  it("cancels a new repository without persisting it", async () => {
    const user = userEvent.setup();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: "Add repository" }));
    await user.type(
      screen.getByLabelText("Repository URL"),
      "https://github.com/multica-ai/discarded",
    );
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(screen.queryByLabelText("Repository URL")).toBeNull());
    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
  });

  it("persists deletion only after confirmation", async () => {
    const user = userEvent.setup();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: "Delete repository" }));
    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
    await user.click(
      screen.getByRole("button", { name: "Delete repository" }),
    );

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", { repos: [] });
    });
  });

  it("keeps the editor open when saving fails", async () => {
    mockUpdateWorkspace.mockRejectedValueOnce(new Error("Network unavailable"));
    const user = userEvent.setup();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: "Edit repository" }));
    await user.click(screen.getByRole("button", { name: "Save repository" }));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith("Network unavailable");
    });
    expect(screen.getByLabelText("Repository URL")).toBeTruthy();
  });

  it("keeps repository management read-only for members", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    expect(screen.queryByRole("button", { name: "Add repository" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Edit repository" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete repository" })).toBeNull();
    expect(
      screen.getByText("Only admins and owners can manage repositories."),
    ).toBeTruthy();
  });
});
