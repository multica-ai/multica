import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockGiteaConnect = vi.hoisted(() => vi.fn());
const mockDeleteConnection = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());
const mockNavPush = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

const workspaceRef = vi.hoisted(() => ({
  current: { id: "workspace-1", name: "Acme", slug: "acme" },
}));
const connectionsRef = vi.hoisted(() => ({
  current: {
    connections: [] as { id: string; base_url: string; account_login: string }[],
    configured: true,
    can_manage: true as boolean,
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[] }) => {
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("gitea")) return { data: connectionsRef.current };
    return { data: undefined };
  },
  useQueryClient: () => ({ invalidateQueries: mockInvalidate }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => workspaceRef.current,
}));

vi.mock("@multica/core/gitea", () => ({
  giteaConnectionsOptions: () => ({ queryKey: ["gitea", "workspace-1", "connections"], queryFn: vi.fn() }),
  giteaKeys: { connections: (wsId: string) => ["gitea", wsId, "connections"] },
}));

vi.mock("@multica/core/api", () => ({
  api: { giteaConnect: mockGiteaConnect, deleteGiteaConnection: mockDeleteConnection },
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: mockNavPush,
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/settings",
    searchParams: new URLSearchParams("tab=gitea"),
  }),
}));

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: mockToastError },
}));

import { GiteaTab } from "./gitea-tab";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function resetFixtures() {
  vi.clearAllMocks();
  workspaceRef.current = { id: "workspace-1", name: "Acme", slug: "acme" };
  connectionsRef.current = { connections: [], configured: true, can_manage: true };
}

describe("GiteaTab", () => {
  beforeEach(resetFixtures);

  it("blocks connect and toasts when either field is empty", async () => {
    const user = userEvent.setup();
    render(<GiteaTab />, { wrapper: I18nWrapper });

    await user.type(screen.getByLabelText(/Instance URL/i), "https://gitea.example.com");
    await user.click(screen.getByRole("button", { name: /^Connect$/ }));

    expect(mockGiteaConnect).not.toHaveBeenCalled();
    expect(mockToastError).toHaveBeenCalled();
  });

  it("submits the base URL and token, then invalidates", async () => {
    const user = userEvent.setup();
    mockGiteaConnect.mockResolvedValue({ id: "conn-1" });
    render(<GiteaTab />, { wrapper: I18nWrapper });

    await user.type(screen.getByLabelText(/Instance URL/i), "https://gitea.example.com");
    await user.type(screen.getByLabelText(/Personal access token/i), "pat-123");
    await user.click(screen.getByRole("button", { name: /^Connect$/ }));

    await waitFor(() => {
      expect(mockGiteaConnect).toHaveBeenCalledWith("workspace-1", {
        base_url: "https://gitea.example.com",
        token: "pat-123",
      });
      expect(mockInvalidate).toHaveBeenCalled();
      expect(mockToastSuccess).toHaveBeenCalled();
    });
  });

  it("lists a connection and disconnects only after confirming", async () => {
    const user = userEvent.setup();
    connectionsRef.current = {
      configured: true,
      can_manage: true,
      connections: [{ id: "conn-9", base_url: "https://gitea.acme.dev", account_login: "acme" }],
    };
    mockDeleteConnection.mockResolvedValue(undefined);
    render(<GiteaTab />, { wrapper: I18nWrapper });

    expect(screen.getByText("https://gitea.acme.dev")).toBeTruthy();
    expect(screen.getByText(/Connected as acme/i)).toBeTruthy();

    await user.click(screen.getByRole("button", { name: /^Disconnect$/ }));
    expect(mockDeleteConnection).not.toHaveBeenCalled();

    const confirm = screen
      .getAllByRole("button", { name: /^Disconnect$/ })
      .find((b) => b.getAttribute("data-slot")?.includes("alert-dialog"));
    await user.click(confirm ?? screen.getAllByRole("button", { name: /^Disconnect$/ })[1]!);

    await waitFor(() => {
      expect(mockDeleteConnection).toHaveBeenCalledWith("workspace-1", "conn-9");
    });
  });

  it("non-admin sees read-only hint and no connect form", () => {
    connectionsRef.current = { connections: [], configured: true, can_manage: false };
    render(<GiteaTab />, { wrapper: I18nWrapper });

    expect(screen.getByText(/Read-only view\./i)).toBeTruthy();
    expect(screen.queryByLabelText(/Instance URL/i)).toBeNull();
  });

  it("shows the secret-key hint and no form when the deployment is not configured", () => {
    connectionsRef.current = { connections: [], configured: false, can_manage: true };
    render(<GiteaTab />, { wrapper: I18nWrapper });

    expect(screen.getByText(/MULTICA_GITEA_SECRET_KEY/)).toBeTruthy();
    expect(screen.queryByLabelText(/Instance URL/i)).toBeNull();
  });

  it("features shortcut navigates to the GitHub tab", async () => {
    const user = userEvent.setup();
    render(<GiteaTab />, { wrapper: I18nWrapper });
    await user.click(screen.getByRole("button", { name: /Manage in GitHub tab/ }));
    expect(mockNavPush).toHaveBeenCalledWith("/acme/settings?tab=github");
  });
});
