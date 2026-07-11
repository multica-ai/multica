import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockReplace = vi.hoisted(() => vi.fn());
const mockPush = vi.hoisted(() => vi.fn());
const spacesRef = vi.hoisted(() => ({
  current: [
    {
      id: "space-1",
      workspace_id: "workspace-1",
      name: "Engineering",
      key: "ENG",
      icon: null,
      context: "",
      issue_counter: 0,
      is_default: true,
      visibility: "open" as const,
      archived_at: null,
      created_by: "user-1",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      is_member: true,
      member_role: "lead" as const,
      is_pinned: false,
      is_followed: false,
      sort_order: 1,
    },
  ],
}));
const navigationRef = vi.hoisted(() => ({
  current: {
    pathname: "/acme/settings/workspace/spaces",
    searchParams: new URLSearchParams(),
  },
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1", name: "Acme", slug: "acme" }),
  useWorkspacePaths: () => ({
    settingsSection: (scope: string, page: string) =>
      `/acme/settings/${scope}/${page}`,
  }),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: () => ({ data: spacesRef.current, isSuccess: true }),
  };
});

vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    ...navigationRef.current,
    push: mockPush,
    replace: mockReplace,
  }),
  AppLink: ({ href, children, ...props }: { href: string; children: ReactNode }) => (
    <a href={href} {...props}>{children}</a>
  ),
}));

vi.mock("./account-tab", () => ({ AccountTab: () => <div>profile page</div> }));
vi.mock("./preferences-tab", () => ({ PreferencesTab: () => <div>preferences page</div> }));
vi.mock("./chat-tab", () => ({ ChatTab: () => <div>chat preference</div> }));
vi.mock("./tokens-tab", () => ({ TokensTab: () => <div>tokens page</div> }));
vi.mock("./workspace-tab", () => ({ WorkspaceTab: () => <div>workspace page</div> }));
vi.mock("./members-tab", () => ({ MembersTab: () => <div>members page</div> }));
vi.mock("./repositories-tab", () => ({ RepositoriesTab: () => <div>repositories page</div> }));
vi.mock("./integrations-tab", () => ({ IntegrationsTab: () => <div>integrations page</div> }));
vi.mock("./notifications-tab", () => ({ NotificationsTab: () => <div>notifications page</div> }));
vi.mock("./workspace-spaces-tab", () => ({ WorkspaceSpacesTab: () => <div>spaces page</div> }));
vi.mock("../../spaces/components/space-detail-page", () => ({
  SpaceSettingsPage: ({ spaceKey }: { spaceKey: string }) => (
    <div>space settings {spaceKey}</div>
  ),
}));

import { SettingsPage } from "./settings-page";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

describe("SettingsPage navigation", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    navigationRef.current = {
      pathname: "/acme/settings/workspace/spaces",
      searchParams: new URLSearchParams(),
    };
  });

  it("renders a path-addressed Workspace page without rewriting it", () => {
    render(<SettingsPage />, { wrapper: Wrapper });

    expect(screen.getByText("spaces page")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Spaces" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(mockReplace).not.toHaveBeenCalled();
  });

  it("renders exactly My Account, Workspace, and Space with dynamic Space settings", () => {
    navigationRef.current = {
      pathname: "/acme/settings/space/ENG",
      searchParams: new URLSearchParams(),
    };

    const { container } = render(<SettingsPage />, { wrapper: Wrapper });

    expect(screen.getByText("space settings ENG")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Engineering" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(
      Array.from(container.querySelectorAll("nav section h2")).map(
        (heading) => heading.textContent,
      ),
    ).toEqual(["My Account", "Workspace", "Space"]);
    expect(mockReplace).not.toHaveBeenCalled();
  });

  it("moves legacy device pages into My Account", async () => {
    navigationRef.current = {
      pathname: "/acme/settings/device/updates",
      searchParams: new URLSearchParams(),
    };

    render(
      <SettingsPage
        extraDeviceTabs={[
          {
            value: "updates",
            label: "Updates",
            icon: () => null,
            content: <div>updates page</div>,
          },
        ]}
      />,
      { wrapper: Wrapper },
    );

    expect(screen.getByText("updates page")).toBeTruthy();
    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith(
        "/acme/settings/account/updates",
      );
    });
  });

  it("canonicalizes a legacy query-tab URL", async () => {
    navigationRef.current = {
      pathname: "/acme/settings",
      searchParams: new URLSearchParams("tab=github"),
    };

    render(<SettingsPage />, { wrapper: Wrapper });

    expect(screen.getByText("integrations page")).toBeTruthy();
    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith(
        "/acme/settings/workspace/integrations",
      );
    });
  });

  it("does not enqueue the same canonical redirect again before navigation commits", async () => {
    navigationRef.current = {
      pathname: "/acme/settings",
      searchParams: new URLSearchParams(),
    };

    const { rerender } = render(<SettingsPage />, { wrapper: Wrapper });
    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith(
        "/acme/settings/account/profile",
      );
    });

    rerender(<SettingsPage />);

    expect(mockReplace).toHaveBeenCalledTimes(1);
  });
});
