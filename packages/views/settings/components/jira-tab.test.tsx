// @vitest-environment jsdom

import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockConnectJira = vi.hoisted(() => vi.fn());
const mockDeleteJiraConnection = vi.hoisted(() => vi.fn());
const mockSyncJiraConnection = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

const connectionsRef = vi.hoisted(() => ({
  current: {
    connections: [] as {
      id: string;
      workspace_id: string;
      base_url: string;
      account_email: string;
      webhook_url: string;
      webhook_path: string;
      jql: string;
      created_at: string;
    }[],
    configured: true,
    can_manage: true as boolean,
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: connectionsRef.current }),
  useQueryClient: () => ({ invalidateQueries: mockInvalidate }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/jira", () => ({
  jiraConnectionsOptions: () => ({ queryKey: ["jira", "workspace-1", "connections"] }),
}));

// Only issueKeys is used by the tab; mocking keeps the heavy issues barrel
// (stores, mutations, ws-updaters) out of this jsdom suite.
vi.mock("@multica/core/issues", () => ({
  issueKeys: { all: (wsId: string) => ["issues", wsId] as const },
}));

vi.mock("@multica/core/api", () => {
  class ApiError extends Error {
    readonly status: number;
    readonly statusText: string;
    constructor(message: string, status: number, statusText: string) {
      super(message);
      this.name = "ApiError";
      this.status = status;
      this.statusText = statusText;
    }
  }
  return {
    ApiError,
    api: {
      connectJira: mockConnectJira,
      deleteJiraConnection: mockDeleteJiraConnection,
      syncJiraConnection: mockSyncJiraConnection,
    },
  };
});

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: mockToastError },
}));

import { ApiError } from "@multica/core/api";
import { JiraTab } from "./jira-tab";

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

function renderTab() {
  return render(<JiraTab />, { wrapper: I18nWrapper });
}

const CONNECTION = {
  id: "conn-1",
  workspace_id: "workspace-1",
  base_url: "https://acme.atlassian.net",
  account_email: "ops@acme.dev",
  webhook_url: "https://multica.example.com/api/webhooks/jira/conn-1",
  webhook_path: "/api/webhooks/jira/conn-1",
  jql: "",
  created_at: "2026-01-01T00:00:00Z",
};

async function fillConnectForm(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText("Jira site URL"), "https://acme.atlassian.net");
  await user.type(screen.getByLabelText("Account email"), "ops@acme.dev");
  await user.type(screen.getByLabelText("API token"), "tok-123");
}

describe("Settings JiraTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    connectionsRef.current = { connections: [], configured: true, can_manage: true };
  });

  it("lists connections with their inbound webhook URL and a disconnect action", () => {
    connectionsRef.current.connections = [CONNECTION];

    renderTab();

    expect(screen.getByText("https://acme.atlassian.net")).toBeInTheDocument();
    expect(screen.getByText("Connected as ops@acme.dev")).toBeInTheDocument();
    expect(
      screen.getByDisplayValue("https://multica.example.com/api/webhooks/jira/conn-1"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /disconnect/i })).toBeInTheDocument();
  });

  it("prefixes the current origin when the server reports no absolute webhook URL", () => {
    connectionsRef.current.connections = [{ ...CONNECTION, webhook_url: "" }];

    renderTab();

    expect(
      screen.getByDisplayValue(`${window.location.origin}/api/webhooks/jira/conn-1`),
    ).toBeInTheDocument();
  });

  it("hides the connect form and disconnect actions from non-admins", () => {
    connectionsRef.current.can_manage = false;

    renderTab();

    expect(screen.queryByLabelText("Jira site URL")).toBeNull();
    expect(screen.queryByRole("button", { name: /disconnect/i })).toBeNull();
    expect(screen.getByText("Ask a workspace admin to connect a Jira site.")).toBeInTheDocument();
  });

  it("shows the operator hint when the integration is not configured server-side", () => {
    connectionsRef.current.configured = false;

    renderTab();

    expect(screen.getByText(/Jira integration is not configured/)).toBeInTheDocument();
    expect(screen.getByText("MULTICA_JIRA_SECRET_KEY")).toBeInTheDocument();
  });

  it("connects and surfaces the one-time webhook secret with a copy warning", async () => {
    const user = userEvent.setup();
    mockConnectJira.mockResolvedValue({
      ...CONNECTION,
      webhook_secret: "one-time-secret",
    });

    renderTab();
    await fillConnectForm(user);
    await user.click(screen.getByRole("button", { name: "Connect" }));

    await waitFor(() => {
      expect(screen.getByDisplayValue("one-time-secret")).toBeInTheDocument();
    });
    expect(mockConnectJira).toHaveBeenCalledWith("workspace-1", {
      base_url: "https://acme.atlassian.net",
      account_email: "ops@acme.dev",
      api_token: "tok-123",
      jql: "",
    });
    expect(
      screen.getByText("Copy the secret now. It is shown only once and cannot be retrieved later."),
    ).toBeInTheDocument();
    expect(mockInvalidate).toHaveBeenCalledWith({ queryKey: ["jira", "workspace-1"] });
    expect(mockToastSuccess).toHaveBeenCalled();
  });

  it("shows an inline error when Jira rejects the credentials (400)", async () => {
    const user = userEvent.setup();
    mockConnectJira.mockRejectedValue(
      new ApiError("jira rejected the email/API token pair", 400, "Bad Request"),
    );

    renderTab();
    await fillConnectForm(user);
    await user.click(screen.getByRole("button", { name: "Connect" }));

    await waitFor(() => {
      expect(
        screen.getByText("Jira rejected the email/API token pair. Check both and try again."),
      ).toBeInTheDocument();
    });
    expect(mockToastError).not.toHaveBeenCalled();
  });

  it("shows an inline error when the Jira site is unreachable (502)", async () => {
    const user = userEvent.setup();
    mockConnectJira.mockRejectedValue(
      new ApiError("could not reach the jira site", 502, "Bad Gateway"),
    );

    renderTab();
    await fillConnectForm(user);
    await user.click(screen.getByRole("button", { name: "Connect" }));

    await waitFor(() => {
      expect(
        screen.getByText("Could not reach the Jira site. Check the URL and try again."),
      ).toBeInTheDocument();
    });
  });

  it("passes the optional JQL filter through on connect", async () => {
    const user = userEvent.setup();
    mockConnectJira.mockResolvedValue({ ...CONNECTION, webhook_secret: "s" });

    renderTab();
    await fillConnectForm(user);
    await user.type(screen.getByLabelText("JQL filter (optional)"), "project = OPS");
    await user.click(screen.getByRole("button", { name: "Connect" }));

    await waitFor(() => {
      expect(mockConnectJira).toHaveBeenCalledWith("workspace-1", {
        base_url: "https://acme.atlassian.net",
        account_email: "ops@acme.dev",
        api_token: "tok-123",
        jql: "project = OPS",
      });
    });
  });

  it("syncs a connection and toasts the created/updated summary", async () => {
    const user = userEvent.setup();
    connectionsRef.current.connections = [CONNECTION];
    let resolveSync!: (v: { created: number; updated: number; total: number }) => void;
    mockSyncJiraConnection.mockImplementation(
      () => new Promise((resolve) => (resolveSync = resolve)),
    );

    renderTab();
    await user.click(screen.getByRole("button", { name: "Sync now" }));

    // In-flight: spinner label shows and the button is disabled.
    const syncing = await screen.findByRole("button", { name: "Syncing..." });
    expect(syncing).toBeDisabled();

    resolveSync({ created: 3, updated: 2, total: 6 });
    await waitFor(() => {
      expect(mockToastSuccess).toHaveBeenCalledWith("Sync complete: 3 created, 2 updated");
    });
    expect(mockSyncJiraConnection).toHaveBeenCalledWith("workspace-1", "conn-1");
    // Pulled issues must appear in the issue lists.
    expect(mockInvalidate).toHaveBeenCalledWith({ queryKey: ["issues", "workspace-1"] });
    expect(screen.getByRole("button", { name: "Sync now" })).toBeEnabled();
  });

  it("toasts an error when the sync fails", async () => {
    const user = userEvent.setup();
    connectionsRef.current.connections = [CONNECTION];
    mockSyncJiraConnection.mockRejectedValue(
      new ApiError("jira rejected the JQL query", 400, "Bad Request"),
    );

    renderTab();
    await user.click(screen.getByRole("button", { name: "Sync now" }));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith("jira rejected the JQL query");
    });
    expect(mockToastSuccess).not.toHaveBeenCalled();
  });

  it("hides the sync action from non-admins", () => {
    connectionsRef.current.connections = [CONNECTION];
    connectionsRef.current.can_manage = false;

    renderTab();

    expect(screen.queryByRole("button", { name: "Sync now" })).toBeNull();
  });

  it("disconnects after confirming the dialog", async () => {
    const user = userEvent.setup();
    connectionsRef.current.connections = [CONNECTION];
    mockDeleteJiraConnection.mockResolvedValue(undefined);

    renderTab();
    await user.click(screen.getByRole("button", { name: /disconnect/i }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Disconnect" }));

    await waitFor(() => {
      expect(mockDeleteJiraConnection).toHaveBeenCalledWith("workspace-1", "conn-1");
    });
    expect(mockToastSuccess).toHaveBeenCalled();
  });
});
