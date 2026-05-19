import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const githubInstallationsRef = vi.hoisted(() => ({
  current: {
    configured: true,
    installations: [
      {
        id: "gh-inst-1",
        workspace_id: "workspace-1",
        installation_id: 123456,
        account_login: "multica-ai",
        account_type: "Organization" as const,
        account_avatar_url: "https://example.test/avatar.png",
        created_at: "2026-05-19T00:00:00Z",
      },
    ],
  },
}));

const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as const }],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: readonly unknown[] }) => {
    if (queryKey[0] === "github") {
      return { data: githubInstallationsRef.current };
    }
    return { data: membersRef.current };
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/github/queries", () => ({
  githubInstallationsOptions: () => ({
    queryKey: ["github", "workspace-1", "installations"],
    queryFn: vi.fn(),
  }),
}));

vi.mock("@multica/core/api", () => ({
  api: { getGitHubConnectURL: vi.fn() },
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
  toast: { error: vi.fn() },
}));

import { IntegrationsTab } from "./integrations-tab";

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

describe("IntegrationsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    membersRef.current = [{ user_id: "user-1", role: "owner" }];
    githubInstallationsRef.current = {
      configured: true,
      installations: [
        {
          id: "gh-inst-1",
          workspace_id: "workspace-1",
          installation_id: 123456,
          account_login: "multica-ai",
          account_type: "Organization",
          account_avatar_url: "https://example.test/avatar.png",
          created_at: "2026-05-19T00:00:00Z",
        },
      ],
    };
  });

  it("shows connected GitHub installations to workspace admins", () => {
    render(<IntegrationsTab />, { wrapper: I18nWrapper });

    expect(screen.getByText("multica-ai")).toBeInTheDocument();
    expect(screen.getByText("Organization")).toBeInTheDocument();
    expect(screen.getByText("Installation #123456")).toBeInTheDocument();
  });
});
