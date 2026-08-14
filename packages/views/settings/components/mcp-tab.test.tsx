// @vitest-environment jsdom

import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import enAgents from "../../locales/en/agents.json";

const mockUpsert = vi.hoisted(() => vi.fn());
const mockDelete = vi.hoisted(() => vi.fn());

const data = vi.hoisted(() => ({
  config: {
    workspace_id: "workspace-1",
    servers: [
      { name: "linear", transport: "http", enabled: true },
      { name: "local-tool", transport: "stdio", enabled: false },
    ],
    server_count: 2,
  },
  isLoading: false,
  role: "owner" as "owner" | "admin" | "member",
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: data.config, isLoading: data.isLoading }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceMcpConfigOptions: () => ({ queryKey: ["workspaces", "workspace-1", "mcp-config"] }),
}));

vi.mock("@multica/core/workspace/mutations", () => ({
  useUpsertWorkspaceMcpServer: () => ({ mutateAsync: mockUpsert, isPending: false }),
  useDeleteWorkspaceMcpServer: () => ({ mutateAsync: mockDelete, isPending: false }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1", name: "Acme", slug: "acme" }),
}));

vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: data.role, isLoading: false }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { McpTab } from "./mcp-tab";

const TEST_RESOURCES = {
  en: { common: enCommon, settings: enSettings, agents: enAgents },
};

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

describe("McpTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    data.role = "owner";
    data.isLoading = false;
    data.config = {
      workspace_id: "workspace-1",
      servers: [
        { name: "linear", transport: "http", enabled: true },
        { name: "local-tool", transport: "stdio", enabled: false },
      ],
      server_count: 2,
    };
    mockUpsert.mockResolvedValue({});
    mockDelete.mockResolvedValue({});
  });

  it("lists the shared servers with their transport and disabled state", () => {
    render(<McpTab />, { wrapper: Wrapper });

    expect(screen.getByText("linear")).toBeInTheDocument();
    expect(screen.getByText("HTTP")).toBeInTheDocument();
    expect(screen.getByText("local-tool")).toBeInTheDocument();
    expect(screen.getByText("stdio")).toBeInTheDocument();
    expect(screen.getByText("Disabled")).toBeInTheDocument();
  });

  it("adds a server through the per-server endpoint", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: /Add server/ }));
    await user.type(screen.getByLabelText("Name"), "github");
    // The shared dialog defaults to the STDIO transport.
    await user.type(screen.getByLabelText("Command"), "github-mcp");
    await user.click(screen.getByRole("button", { name: "Add Server" }));

    await waitFor(() =>
      expect(mockUpsert).toHaveBeenCalledWith(
        expect.objectContaining({ name: "github" }),
      ),
    );
    // The entry is sent on its own — the client never holds, and therefore
    // never resends, the rest of the shared document.
    const call = mockUpsert.mock.calls[0]![0] as { entry: Record<string, unknown> };
    expect(call.entry).not.toHaveProperty("mcpServers");
  });

  // Regression: the shared dialog lets the name be edited, and the API has no
  // atomic rename. Without pinning the name, "editing" linear to linear-v2
  // would create a second server, leave linear behind, and still toast
  // "updated".
  it("pins the name while editing so a rename cannot orphan the original", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(screen.getAllByRole("button", { name: "Edit server" })[0]!);

    const nameInput = screen.getByLabelText("Name") as HTMLInputElement;
    expect(nameInput.value).toBe("linear");
    expect(nameInput).toHaveAttribute("readonly");

    // Even if something did change the field, the upsert targets the entry
    // that was opened.
    await user.type(screen.getByLabelText("Server URL"), "https://linear-v2.example");
    await user.click(screen.getByRole("button", { name: "Save Changes" }));

    await waitFor(() =>
      expect(mockUpsert).toHaveBeenCalledWith(
        expect.objectContaining({ name: "linear" }),
      ),
    );
    expect(mockUpsert).toHaveBeenCalledTimes(1);
  });

  // The saved entry cannot be read back, but the safe summary still knows the
  // transport — so the empty form must open on the right one instead of
  // defaulting a stdio server to HTTP.
  it("opens the edit form on the server's own transport", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    // Row 1 is the stdio server.
    await user.click(screen.getAllByRole("button", { name: "Edit server" })[1]!);

    expect(screen.getByRole("button", { name: "STDIO" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByLabelText("Command")).toBeInTheDocument();
  });

  // Regression: the guided form only speaks stdio/http and REWRITES the entry
  // on save (`type: "http"`), so editing an SSE server through it would change
  // its protocol and can break the server. The server returns `sse`, so this is
  // reachable, not hypothetical — those entries take the JSON path instead.
  it.each(["sse", "websocket"])(
    "edits a %s server through JSON, never the transport-rewriting form",
    async (transport) => {
      const user = userEvent.setup();
      data.config = {
        workspace_id: "workspace-1",
        servers: [{ name: "streamy", transport, enabled: true }],
        server_count: 1,
      };
      render(<McpTab />, { wrapper: Wrapper });

      await user.click(screen.getByRole("button", { name: "Edit server" }));

      expect(screen.getByRole("tab", { name: "JSON" })).toHaveAttribute(
        "aria-selected",
        "true",
      );
      // Not merely unselected: switching to the form would rewrite the entry.
      expect(screen.getByRole("tab", { name: "Form" })).toHaveAttribute(
        "aria-disabled",
        "true",
      );
      expect(screen.queryByLabelText("Server URL")).toBeNull();
    },
  );

  it("removes a server after confirmation", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(screen.getAllByRole("button", { name: "Remove server" })[0]!);
    await user.click(screen.getByRole("button", { name: "Remove" }));

    await waitFor(() => expect(mockDelete).toHaveBeenCalledWith("linear"));
  });

  it("hides every write affordance from a plain member", () => {
    data.role = "member";
    render(<McpTab />, { wrapper: Wrapper });

    // The inventory itself stays visible — it carries no credential material.
    expect(screen.getByText("linear")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Add server/ })).toBeNull();
    expect(screen.queryByRole("button", { name: "Edit server" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Remove server" })).toBeNull();
    expect(
      screen.getByText(/Only workspace owners and admins/),
    ).toBeInTheDocument();
  });

  it("renders an empty state when the workspace shares nothing", () => {
    data.config = { workspace_id: "workspace-1", servers: [], server_count: 0 };
    render(<McpTab />, { wrapper: Wrapper });

    expect(screen.getByText("No shared MCP servers")).toBeInTheDocument();
  });

  // The document is write-only, so the screen must never imply it is showing
  // a saved configuration: it says an edit replaces the entry.
  it("states that saved configurations are write-only", () => {
    render(<McpTab />, { wrapper: Wrapper });

    expect(screen.getByText(/write-only/)).toBeInTheDocument();
  });

  it("survives a server payload with no servers array", () => {
    // Backend drift: the schema defaults `servers` to [], but the component
    // must not crash if it ever arrives undefined.
    data.config = { workspace_id: "workspace-1" } as typeof data.config;
    render(<McpTab />, { wrapper: Wrapper });

    expect(screen.getByText("No shared MCP servers")).toBeInTheDocument();
  });
});
