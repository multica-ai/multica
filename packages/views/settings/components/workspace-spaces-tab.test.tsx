import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import enSpaces from "../../locales/en/spaces.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const mockInvalidateQueries = vi.hoisted(() => vi.fn());
const spacesRef = vi.hoisted(() => ({
  current: [
    {
      id: "space-default",
      workspace_id: "workspace-1",
      name: "General",
      key: "GEN",
      icon: null,
      issue_counter: 0,
      archived_at: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      is_default: true,
      visibility: "open" as const,
      is_member: true,
      member_role: "lead" as const,
      sort_order: 1,
    },
    {
      id: "space-other",
      workspace_id: "workspace-1",
      name: "Engineering",
      key: "ENG",
      icon: null,
      issue_counter: 0,
      archived_at: null,
      created_at: "2026-01-02T00:00:00Z",
      updated_at: "2026-01-02T00:00:00Z",
      is_default: false,
      visibility: "open" as const,
      is_member: true,
      member_role: "member" as const,
      sort_order: 2,
    },
  ],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly string[] }) =>
    options.queryKey?.[0] === "spaces"
      ? { data: spacesRef.current, isLoading: false }
      : { data: [{ user_id: "user-1", role: "owner" }] },
  useQueryClient: () => ({
    setQueryData: vi.fn(),
    invalidateQueries: mockInvalidateQueries,
  }),
}));

vi.mock("@multica/core/api", () => ({
  api: { updateWorkspace: mockUpdateWorkspace },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1", name: "Acme", slug: "acme" }),
  useWorkspacePaths: () => ({
    spaceNew: () => "/acme/space/new",
    spaceDetail: (key: string) => `/acme/space/${key}`,
    spaceSettings: (key: string) => `/acme/space/${key}/settings`,
  }),
}));

vi.mock("@multica/core/spaces/queries", () => ({
  spaceListOptions: () => ({ queryKey: ["spaces", "workspace-1", "list"] }),
  spaceKeys: { all: (wsId: string) => ["spaces", wsId] },
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members", "workspace-1"] }),
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...props }: { href: string; children: ReactNode }) => (
    <a href={href} {...props}>{children}</a>
  ),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { WorkspaceSpacesTab } from "./workspace-spaces-tab";

const TEST_RESOURCES = {
  en: { common: enCommon, settings: enSettings, spaces: enSpaces },
};

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

describe("WorkspaceSpacesTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUpdateWorkspace.mockResolvedValue({
      id: "workspace-1",
      name: "Acme",
      slug: "acme",
    });
  });

  it("shows the configured default and changes it explicitly", async () => {
    const user = userEvent.setup();
    render(<WorkspaceSpacesTab />, { wrapper: Wrapper });

    expect(screen.getByText("General")).toBeTruthy();
    expect(screen.getByText("Default")).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Set as Default" }));

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        default_space_id: "space-other",
      });
    });
    expect(mockInvalidateQueries).toHaveBeenCalledWith({
      queryKey: ["spaces", "workspace-1"],
    });
  });
});
