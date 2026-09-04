// @vitest-environment jsdom

import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import enAgents from "../../locales/en/agents.json";

const mockCreate = vi.hoisted(() => vi.fn());
const mockUpdate = vi.hoisted(() => vi.fn());
const mockDelete = vi.hoisted(() => vi.fn());

const server = (over: Record<string, unknown>) => ({
  id: "srv-1",
  workspace_id: "workspace-1",
  name: "linear",
  transport: "http",
  created_at: "2026-08-14T00:00:00Z",
  updated_at: "2026-08-14T00:00:00Z",
  ...over,
});

const data = vi.hoisted(() => ({
  servers: [] as Array<Record<string, unknown>>,
  isLoading: false,
  role: "owner" as "owner" | "admin" | "member",
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: data.servers, isLoading: data.isLoading }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceMcpServersOptions: () => ({ queryKey: ["workspaces", "workspace-1", "mcp-servers"] }),
}));

vi.mock("@multica/core/workspace/mutations", () => ({
  useCreateWorkspaceMcpServer: () => ({ mutateAsync: mockCreate, isPending: false }),
  useUpdateWorkspaceMcpServer: () => ({ mutateAsync: mockUpdate, isPending: false }),
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
    data.servers = [
      server({ id: "srv-1", name: "linear", transport: "http" }),
      server({ id: "srv-2", name: "local-tool", transport: "stdio" }),
    ];
    mockCreate.mockResolvedValue({});
    mockUpdate.mockResolvedValue({});
    mockDelete.mockResolvedValue({});
  });

  it("shows each transport as visible text beside its labeled icon", async () => {
    const user = userEvent.setup();
    data.servers[1] = server({ id: "srv-2", name: "local-tool", transport: "stdio", enabled: false });
    render(<McpTab />, { wrapper: Wrapper });

    expect(screen.getByText("linear")).toBeInTheDocument();
    expect(screen.getByLabelText("Streamable HTTP")).toBeInTheDocument();
    expect(screen.getByText("local-tool")).toBeInTheDocument();
    expect(screen.getByLabelText("STDIO")).toBeInTheDocument();
    expect(screen.getByText("Streamable HTTP")).toBeVisible();
    expect(screen.getByText("STDIO")).toBeVisible();
    expect(screen.getByText("Disabled")).toBeVisible();
    const replaceButton = screen.getByRole("button", {
      name: "Replace configuration linear",
    });
    expect(replaceButton).toHaveTextContent(/^$/);
    await user.hover(replaceButton);
    expect(await screen.findByText("Replace configuration", { exact: true })).toBeVisible();
  });

  it.each([
    ["linear", "Server URL", "https://mcp.example.com/mcp"],
    ["local-tool", "Command", "npx"],
  ])("provides a connection example when replacing %s", async (name, label, example) => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(
      screen.getByRole("button", { name: `Replace configuration ${name}` }),
    );

    expect(screen.getByLabelText(label)).toHaveValue("");
    expect(screen.getByLabelText(label)).toHaveAttribute("placeholder", example);
  });

  it("adds a server to the library", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: /Add server/ }));
    await user.type(screen.getByLabelText("Server name"), "github");
    // The shared dialog defaults to the STDIO transport.
    await user.type(screen.getByLabelText("Command"), "github-mcp");
    await user.click(screen.getByRole("button", { name: "Add argument" }));
    await user.type(screen.getByLabelText("Startup arguments 1"), "   ");
    await user.click(screen.getByRole("button", { name: "Add argument" }));
    await user.type(screen.getByLabelText("Startup arguments 2"), "--stdio");
    await user.click(
      screen.getByRole("button", { name: "Add environment variable" }),
    );
    await user.type(
      screen.getByLabelText("Environment variables: Variable name 1"),
      "EMPTY_VALUE",
    );
    await user.type(
      screen.getByLabelText("Environment variables: Value 1"),
      "   ",
    );
    await user.click(screen.getByRole("button", { name: "Add" }));

    await waitFor(() =>
      expect(mockCreate).toHaveBeenCalledWith(
        expect.objectContaining({ name: "github" }),
      ),
    );
    // Every added row is explicit user input. Whitespace and empty values can
    // carry meaning for process arguments and environment variables.
    const call = mockCreate.mock.calls[0]![0] as { config: Record<string, unknown> };
    expect(call.config).toEqual({
      command: "github-mcp",
      args: ["   ", "--stdio"],
      env: { EMPTY_VALUE: "   " },
    });
  });

  it("opens a new shared dialog without displaying validation errors", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: /Add server/ }));

    expect(screen.getByLabelText("Server name")).not.toHaveAttribute("aria-invalid");
    expect(screen.getByLabelText("Command")).not.toHaveAttribute("aria-invalid");
    expect(screen.queryByText("Enter a server name.")).toBeNull();
    expect(screen.queryByText(/Enter the command used/)).toBeNull();
  });

  it("waits for an explicit save attempt before showing inline validation", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: /Add server/ }));
    const nameInput = screen.getByLabelText("Server name");
    await user.click(nameInput);
    await user.tab();

    expect(screen.queryByRole("alert")).toBeNull();
    expect(nameInput).not.toHaveAttribute("aria-invalid");

    await user.click(screen.getByRole("button", { name: "Add" }));

    const error = screen.getByText("Enter a server name.");
    expect(error).toHaveTextContent("Enter a server name.");
    expect(nameInput).toHaveAttribute("aria-describedby", error.id);
    expect(error.parentElement).toBe(nameInput.parentElement);
    expect(nameInput).toHaveFocus();
  });

  it("focuses the missing connection field after a save attempt", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: /Add server/ }));
    await user.type(screen.getByLabelText("Server name"), "github");
    await user.click(screen.getByRole("button", { name: "Add" }));

    expect(screen.getByLabelText("Command")).toHaveFocus();
    expect(
      screen.getByText("Enter the command used to launch this server."),
    ).toBeInTheDocument();
  });

  it("focuses a missing URL and clears the error when transport changes", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: /Add server/ }));
    await user.type(screen.getByLabelText("Server name"), "remote");
    await user.click(screen.getByRole("button", { name: /^Streamable HTTP/ }));
    await user.click(screen.getByRole("button", { name: "Add" }));

    const urlInput = screen.getByLabelText("Server URL");
    expect(urlInput).toHaveFocus();
    expect(screen.getByText("Enter the MCP server URL.")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /^STDIO/ }));
    expect(screen.queryByText("Enter the MCP server URL.")).toBeNull();
    expect(screen.getByLabelText("Command")).not.toHaveAttribute("aria-invalid");
  });

  it.each([
    ["invalid name", "Use only letters, numbers, hyphens, and underscores."],
    ["linear", "A server with this name already exists."],
  ])("rejects the server name %s with its precise inline error", async (name, message) => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: /Add server/ }));
    const nameInput = screen.getByLabelText("Server name");
    await user.type(nameInput, name);
    await user.type(screen.getByLabelText("Command"), "tool");
    await user.click(screen.getByRole("button", { name: "Add" }));

    const error = screen.getByText(message);
    expect(nameInput).toHaveFocus();
    expect(nameInput).toHaveAttribute("aria-describedby", error.id);
    expect(mockCreate).not.toHaveBeenCalled();
  });

  it.each([
    ["[]", "The server configuration must be a JSON object."],
    ["{}", "The server configuration must include a command or URL."],
  ])("rejects semantic JSON error for %s", async (json, message) => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: /Add server/ }));
    await user.type(screen.getByLabelText("Server name"), "github");
    await user.click(screen.getByRole("tab", { name: "JSON" }));
    const jsonInput = screen.getByLabelText("MCP server JSON configuration");
    fireEvent.change(jsonInput, { target: { value: json } });
    await user.click(screen.getByRole("button", { name: "Add" }));

    const error = screen.getByText(message);
    expect(jsonInput).toHaveFocus();
    expect(jsonInput).toHaveAttribute("aria-describedby", error.id);
    expect(mockCreate).not.toHaveBeenCalled();
  });

  it("keeps the footer outside the scroll area and caps the dialog to the viewport", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    const expectStableDialogLayout = () => {
      const dialog = screen.getByRole("dialog");
      const scrollArea = dialog.querySelector<HTMLElement>(
        '[data-slot="mcp-dialog-scroll-area"]',
      );
      const footer = dialog.querySelector<HTMLElement>(
        '[data-slot="mcp-dialog-footer"]',
      );
      const submit = screen.getByRole("button", { name: "Add" });

      expect(dialog).toHaveClass("flex", "max-h-[88vh]", "flex-col");
      expect(scrollArea).toHaveClass("overflow-y-auto");
      expect(scrollArea).not.toContainElement(footer);
      expect(footer).toHaveClass("shrink-0", "border-t", "bg-muted/30");
      expect(submit).toHaveAttribute("form", scrollArea?.id);
    };

    await user.click(screen.getByRole("button", { name: /Add server/ }));
    expectStableDialogLayout();

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    await user.click(screen.getByRole("button", { name: /Add server/ }));
    expectStableDialogLayout();
  });

  it("renames a library server inline without replacing its config", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(
      screen.getAllByRole("button", { name: /Rename server/ })[0]!,
    );

    const nameInput = screen.getByRole("textbox", { name: "Rename server" });
    expect(nameInput).toHaveValue("linear");
    expect(screen.queryByRole("dialog")).toBeNull();

    await user.clear(nameInput);
    await user.type(nameInput, "linear-v2");
    await user.click(screen.getByRole("button", { name: "Save name" }));

    await waitFor(() =>
      expect(mockUpdate).toHaveBeenCalledWith({
        serverId: "srv-1",
        name: "linear-v2",
      }),
    );
    expect(mockCreate).not.toHaveBeenCalled();
  });

  it("validates every inline rename branch and cancels a no-op", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(
      screen.getAllByRole("button", { name: /Rename server/ })[1]!,
    );
    const nameInput = screen.getByRole("textbox", { name: "Rename server" });

    await user.clear(nameInput);
    await user.click(screen.getByRole("button", { name: "Save name" }));
    expect(screen.getByText("Enter a server name.")).toBeInTheDocument();
    expect(nameInput).toHaveAttribute("aria-invalid", "true");

    await user.type(nameInput, "bad name");
    await user.click(screen.getByRole("button", { name: "Save name" }));
    expect(
      screen.getByText("Use only letters, numbers, hyphens, and underscores."),
    ).toBeInTheDocument();

    await user.clear(nameInput);
    await user.type(nameInput, "linear");
    await user.click(screen.getByRole("button", { name: "Save name" }));

    expect(
      screen.getByText("A server with this name already exists."),
    ).toBeInTheDocument();
    expect(nameInput).toHaveAttribute("aria-invalid", "true");
    expect(mockUpdate).not.toHaveBeenCalled();

    await user.clear(nameInput);
    await user.type(nameInput, "local-tool");
    await user.click(screen.getByRole("button", { name: "Save name" }));
    expect(screen.queryByRole("textbox", { name: "Rename server" })).toBeNull();
    expect(mockUpdate).not.toHaveBeenCalled();
  });

  it.each([
    { reason: "an API error", error: new Error("Rename failed"), message: "Rename failed" },
    { reason: "an empty error", error: new Error(""), message: "Could not rename the shared MCP server" },
    { reason: "a non-Error rejection", error: null, message: "Could not rename the shared MCP server" },
  ])("keeps inline rename open after $reason", async ({ error, message }) => {
    const user = userEvent.setup();
    mockUpdate.mockRejectedValueOnce(error);
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(
      screen.getAllByRole("button", { name: /Rename server/ })[0]!,
    );
    const nameInput = screen.getByRole("textbox", { name: "Rename server" });
    await user.clear(nameInput);
    await user.type(nameInput, "linear-v2");
    await user.click(screen.getByRole("button", { name: "Save name" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(message);
    expect(nameInput).toHaveValue("linear-v2");
    expect(nameInput).not.toBeDisabled();
  });

  it("blocks competing library actions while an inline rename is pending", async () => {
    const user = userEvent.setup();
    let finishRename!: () => void;
    mockUpdate.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishRename = () => resolve({});
        }),
    );
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(
      screen.getAllByRole("button", { name: /Rename server/ })[0]!,
    );
    const nameInput = screen.getByRole("textbox", { name: "Rename server" });
    await user.clear(nameInput);
    await user.type(nameInput, "linear-v2");
    await user.click(screen.getByRole("button", { name: "Save name" }));

    await waitFor(() => expect(mockUpdate).toHaveBeenCalledOnce());
    expect(nameInput).toBeDisabled();
    expect(screen.getByRole("button", { name: "Save name" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel rename" })).toBeDisabled();
    expect(screen.getByRole("button", { name: /Add server/ })).toBeDisabled();

    finishRename();
    await waitFor(() =>
      expect(screen.queryByRole("textbox", { name: "Rename server" })).toBeNull(),
    );
  });

  it("replaces the full config without sending a name change", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(
      screen.getAllByRole("button", {
        name: /^Replace configuration /,
      })[0]!,
    );

    expect(
      screen.getByRole("heading", { name: "Replace configuration for linear" }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Server name")).toBeNull();
    expect(screen.queryByText("Name", { exact: true })).toBeNull();
    expect(screen.getByRole("dialog")).toHaveTextContent("linear");
    expect(screen.getByLabelText("Server URL")).toHaveValue("");

    await user.type(screen.getByLabelText("Server URL"), "https://linear-v2.example");
    await user.click(screen.getByRole("button", { name: "Replace configuration" }));

    await waitFor(() =>
      expect(mockUpdate).toHaveBeenCalledWith({
        serverId: "srv-1",
        config: { type: "http", url: "https://linear-v2.example" },
      }),
    );
  });

  it("keeps an in-progress replacement when the settings page rerenders", async () => {
    const user = userEvent.setup();
    const view = render(<McpTab />, { wrapper: Wrapper });

    await user.click(
      screen.getAllByRole("button", {
        name: /^Replace configuration /,
      })[0]!,
    );
    const urlInput = screen.getByLabelText("Server URL");
    await user.type(urlInput, "https://draft.example/mcp");

    view.rerender(<McpTab />);

    expect(urlInput).toHaveValue("https://draft.example/mcp");
  });

  // The saved entry cannot be read back, but the safe summary still knows the
  // transport — so the blank replacement form opens on the right one.
  it("opens the replacement form on the server's own transport", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    // Row 1 is the stdio server.
    await user.click(
      screen.getAllByRole("button", {
        name: /^Replace configuration /,
      })[1]!,
    );

    expect(screen.getByRole("button", { name: /^STDIO/ })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByLabelText("Command")).toHaveValue("");
  });

  // Unknown transports start in JSON because that best reflects the inventory,
  // but replacement is explicit, so the user may intentionally switch to the
  // visual editor and replace it with STDIO or HTTP.
  it.each(["sse", "websocket"])(
    "starts a %s replacement in JSON while keeping the visual editor available",
    async (transport) => {
      const user = userEvent.setup();
      data.servers = [server({ name: "streamy", transport })];
      render(<McpTab />, { wrapper: Wrapper });

      await user.click(
        screen.getByRole("button", { name: /^Replace configuration / }),
      );

      expect(screen.getByRole("tab", { name: "JSON" })).toHaveAttribute(
        "aria-selected",
        "true",
      );
      expect(screen.getByRole("tab", { name: "Visual editor" })).toHaveAttribute(
        "aria-disabled",
        "false",
      );
      expect(screen.queryByLabelText("Server URL")).toBeNull();
    },
  );

  it("removes a server after confirmation", async () => {
    const user = userEvent.setup();
    render(<McpTab />, { wrapper: Wrapper });

    await user.click(
      screen.getAllByRole("button", { name: /Remove server/ })[0]!,
    );
    await user.click(screen.getByRole("button", { name: "Remove" }));

    await waitFor(() => expect(mockDelete).toHaveBeenCalledWith("srv-1"));
  });

  it("hides every write affordance from a plain member", () => {
    data.role = "member";
    render(<McpTab />, { wrapper: Wrapper });

    // The inventory itself stays visible — it carries no credential material.
    expect(screen.getByText("linear")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Add server/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Rename server/ })).toBeNull();
    expect(
      screen.queryByRole("button", { name: /^Replace configuration / }),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: /Remove server/ })).toBeNull();
    expect(
      screen.getByText(/Only workspace owners and admins/),
    ).toBeInTheDocument();
  });

  it("renders an empty state when the library is empty", () => {
    data.servers = [];
    render(<McpTab />, { wrapper: Wrapper });

    expect(screen.getByText("No shared MCP servers")).toBeInTheDocument();
  });

  it("survives a payload that is not an array", () => {
    // Backend drift: the schema defaults the list to [], but the component
    // must not crash if it ever arrives undefined.
    data.servers = undefined as unknown as typeof data.servers;
    render(<McpTab />, { wrapper: Wrapper });

    expect(screen.getByText("No shared MCP servers")).toBeInTheDocument();
  });
});
