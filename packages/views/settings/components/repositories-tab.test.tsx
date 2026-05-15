import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const workspaceRef = vi.hoisted(() => ({
  current: {
    id: "workspace-1",
    name: "Test Workspace",
    slug: "test-workspace",
    repos: [{ url: "https://github.com/multica-ai/multica" }] as { url: string }[],
  },
}));
const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as const }],
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
    (sel?: (s: { user: { id: string } }) => unknown) =>
      sel ? sel({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
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

describe("RepositoriesTab — view/edit toggle", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    workspaceRef.current = {
      id: "workspace-1",
      name: "Test Workspace",
      slug: "test-workspace",
      repos: [{ url: "https://github.com/multica-ai/multica" }],
    };
    membersRef.current = [{ user_id: "user-1", role: "owner" }];
  });

  it("renders persisted repos in display mode (no input)", () => {
    render(<RepositoriesTab />, { wrapper: I18nWrapper });
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByText("https://github.com/multica-ai/multica")).toBeTruthy();
  });

  it("Save button is disabled when clean", () => {
    render(<RepositoriesTab />, { wrapper: I18nWrapper });
    expect(screen.getByRole("button", { name: /^Save$/ })).toBeDisabled();
  });

  it("clicking Edit reveals an input pre-filled with the URL", async () => {
    const user = userEvent.setup();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: "Edit repository" }));

    const input = screen.getByRole("textbox") as HTMLInputElement;
    expect(input.value).toBe("https://github.com/multica-ai/multica");
  });

  it("Save re-enables after editing, then returns to display mode + disabled on success", async () => {
    const user = userEvent.setup();
    mockUpdateWorkspace.mockImplementation(async (_id: string, payload: { repos: { url: string }[] }) => ({
      ...workspaceRef.current,
      repos: payload.repos,
    }));

    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: "Edit repository" }));
    const input = screen.getByRole("textbox");
    await user.clear(input);
    await user.type(input, "https://github.com/multica-ai/edited");

    const saveBtn = screen.getByRole("button", { name: /^Save$/ });
    expect(saveBtn).not.toBeDisabled();

    // Simulate the workspace cache resync that the parent provider does
    // after a successful save — `setQueryData` updates the cache and the
    // useCurrentWorkspace hook would yield the new value on the next render.
    mockUpdateWorkspace.mockImplementationOnce(async (_id: string, payload: { repos: { url: string }[] }) => {
      workspaceRef.current = { ...workspaceRef.current, repos: payload.repos };
      return workspaceRef.current;
    });

    await user.click(saveBtn);

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalled();
    });

    // After successful save, edit mode is cleared — input gone, Save disabled.
    await waitFor(() => {
      expect(screen.queryByRole("textbox")).toBeNull();
    });
    expect(screen.getByRole("button", { name: /^Save$/ })).toBeDisabled();
  });

  it("newly added rows start in edit mode", async () => {
    const user = userEvent.setup();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    expect(screen.queryByRole("textbox")).toBeNull();
    await user.click(screen.getByRole("button", { name: /Add repository/ }));

    expect(screen.getByRole("textbox")).toBeTruthy();
    expect(screen.getByRole("button", { name: /^Save$/ })).not.toBeDisabled();
  });

  it("Edit clean row → Cancel returns to display mode without changing URL or dirtying Save", async () => {
    const user = userEvent.setup();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: "Edit repository" }));
    expect(screen.getByRole("textbox")).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Cancel edit" }));

    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByText("https://github.com/multica-ai/multica")).toBeTruthy();
    expect(screen.getByRole("button", { name: /^Save$/ })).toBeDisabled();
    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
  });

  it("Cancel on a dirty edited row reverts the URL and exits edit mode", async () => {
    const user = userEvent.setup();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: "Edit repository" }));
    const input = screen.getByRole("textbox") as HTMLInputElement;
    await user.clear(input);
    await user.type(input, "https://github.com/multica-ai/changed");
    expect(screen.getByRole("button", { name: /^Save$/ })).not.toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Cancel edit" }));

    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByText("https://github.com/multica-ai/multica")).toBeTruthy();
    expect(screen.getByRole("button", { name: /^Save$/ })).toBeDisabled();
  });

  it("Cancel on a newly added (never saved) row removes the row entirely", async () => {
    const user = userEvent.setup();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: /Add repository/ }));
    expect(screen.getByRole("textbox")).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Cancel edit" }));

    expect(screen.queryByRole("textbox")).toBeNull();
    // Original persisted row is still there; the new empty row is gone.
    expect(screen.getByText("https://github.com/multica-ai/multica")).toBeTruthy();
    expect(screen.getByRole("button", { name: /^Save$/ })).toBeDisabled();
  });

  it("deleting a row shifts tracked edit indices so the wrong row doesn't open", async () => {
    workspaceRef.current = {
      ...workspaceRef.current,
      repos: [{ url: "https://a.example/repo.git" }, { url: "https://b.example/repo.git" }],
    };
    const user = userEvent.setup();
    render(<RepositoriesTab />, { wrapper: I18nWrapper });

    // Edit the second row.
    const editButtons = screen.getAllByRole("button", { name: "Edit repository" });
    await user.click(editButtons[1]!);
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe(
      "https://b.example/repo.git",
    );

    // Delete the first row. The remaining row should remain in edit mode
    // (its index dropped from 1 → 0).
    const deleteButtons = screen.getAllByRole("button", { name: "Delete repository" });
    await user.click(deleteButtons[0]!);

    const input = screen.getByRole("textbox") as HTMLInputElement;
    expect(input.value).toBe("https://b.example/repo.git");
  });
});
