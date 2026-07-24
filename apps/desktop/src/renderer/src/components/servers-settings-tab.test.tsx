import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

const mocks = vi.hoisted(() => ({
  listServers: vi.fn(),
  upsertServer: vi.fn(),
  removeServer: vi.fn(),
  switchServer: vi.fn(),
  switchDesktopServer: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

const translations = {
  desktop: {
    servers: {
      title: "Servers",
      description: "Manage backends",
      loading: "Loading servers…",
      active: "Active",
      switch: "Switch",
      edit: "Edit server",
      edit_title: "Edit server",
      edit_active_hint: "Changing active API reloads",
      remove: "Remove server",
      added: "Server added",
      updated: "Server updated",
      updated_reloading: "Server updated — reloading…",
      removed: "Server removed",
      cannot_remove_last: "Keep at least one server",
      api_url_required: "API URL is required",
      dev_mode_hint: "Dev builds cannot switch",
      add_title: "Add server",
      add_description: "Save another endpoint",
      add: "Add server",
      name_label: "Display name",
      name_placeholder: "Personal",
      api_url_label: "API URL",
      app_url_label: "Web app URL",
      app_url_placeholder: "Optional",
      cancel: "Cancel",
      save: "Save",
    },
  },
};

vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (selector: (resources: typeof translations) => string) =>
      selector(translations),
  }),
}));

vi.mock("sonner", () => ({
  toast: {
    success: mocks.toastSuccess,
    error: mocks.toastError,
  },
}));

vi.mock("../platform/server-switch", () => ({
  switchDesktopServer: (...args: unknown[]) =>
    mocks.switchDesktopServer(...args),
}));

import { ServersSettingsTab } from "./servers-settings-tab";

const personalState = {
  editable: true,
  activeServerId: "personal",
  servers: [
    {
      id: "cloud",
      name: "Multica Cloud",
      apiUrl: "https://api.multica.ai",
      wsUrl: "wss://api.multica.ai/ws",
      appUrl: "https://multica.ai",
    },
    {
      id: "personal",
      name: "Personal",
      apiUrl: "http://127.0.0.1:28443",
      wsUrl: "ws://127.0.0.1:28443/ws",
      appUrl: "http://127.0.0.1:28443",
    },
  ],
};

describe("ServersSettingsTab", () => {
  beforeEach(() => {
    mocks.listServers.mockReset().mockResolvedValue({
      ok: true,
      servers: personalState,
    });
    mocks.upsertServer.mockReset();
    mocks.removeServer.mockReset();
    mocks.switchServer.mockReset();
    mocks.switchDesktopServer.mockReset().mockResolvedValue(undefined);
    mocks.toastSuccess.mockReset();
    mocks.toastError.mockReset();

    Object.defineProperty(window, "desktopAPI", {
      configurable: true,
      value: {
        listServers: mocks.listServers,
        upsertServer: mocks.upsertServer,
        removeServer: mocks.removeServer,
        switchServer: mocks.switchServer,
      },
    });
  });

  it("lists servers and marks the active one", async () => {
    render(<ServersSettingsTab />);
    expect(await screen.findByText("Personal")).toBeInTheDocument();
    expect(screen.getByText("Multica Cloud")).toBeInTheDocument();
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Switch" })).toBeInTheDocument();
  });

  it("switches to another server", async () => {
    render(<ServersSettingsTab />);
    await screen.findByText("Multica Cloud");
    fireEvent.click(screen.getByRole("button", { name: "Switch" }));
    await waitFor(() => {
      expect(mocks.switchDesktopServer).toHaveBeenCalledWith("cloud");
    });
  });

  it("adds a server through the form", async () => {
    mocks.upsertServer.mockResolvedValue({
      ok: true,
      servers: {
        ...personalState,
        servers: [
          ...personalState.servers,
          {
            id: "company",
            name: "Company",
            apiUrl: "https://multica.corp.example",
            wsUrl: "wss://multica.corp.example/ws",
            appUrl: "https://multica.corp.example",
          },
        ],
      },
    });

    render(<ServersSettingsTab />);
    await screen.findByText("Personal");
    fireEvent.click(screen.getByRole("button", { name: "Add server" }));
    fireEvent.change(screen.getByPlaceholderText("Personal"), {
      target: { value: "Company" },
    });
    fireEvent.change(screen.getByPlaceholderText("https://api.example.com"), {
      target: { value: "https://multica.corp.example" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.upsertServer).toHaveBeenCalledWith({
        id: undefined,
        name: "Company",
        apiUrl: "https://multica.corp.example",
        appUrl: undefined,
      });
      expect(mocks.toastSuccess).toHaveBeenCalledWith("Server added");
    });
  });

  it("edits an existing server by id", async () => {
    mocks.upsertServer.mockResolvedValue({
      ok: true,
      servers: {
        ...personalState,
        servers: personalState.servers.map((s) =>
          s.id === "cloud" ? { ...s, name: "Cloud Renamed" } : s,
        ),
      },
    });

    render(<ServersSettingsTab />);
    await screen.findByText("Multica Cloud");
    const editButtons = screen.getAllByRole("button", { name: "Edit server" });
    fireEvent.click(editButtons[0]!);
    expect(await screen.findByText("Edit server")).toBeInTheDocument();
    fireEvent.change(screen.getByDisplayValue("Multica Cloud"), {
      target: { value: "Cloud Renamed" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.upsertServer).toHaveBeenCalledWith(
        expect.objectContaining({
          id: "cloud",
          name: "Cloud Renamed",
          apiUrl: "https://api.multica.ai",
        }),
      );
      expect(mocks.toastSuccess).toHaveBeenCalledWith("Server updated");
    });
  });

  it("reloads when the active server endpoint is edited", async () => {
    mocks.upsertServer.mockResolvedValue({
      ok: true,
      servers: {
        ...personalState,
        servers: personalState.servers.map((s) =>
          s.id === "personal"
            ? { ...s, apiUrl: "http://127.0.0.1:9999", name: "Personal" }
            : s,
        ),
      },
    });

    render(<ServersSettingsTab />);
    await screen.findByText("Personal");
    const editButtons = screen.getAllByRole("button", { name: "Edit server" });
    // personal is second in the list
    fireEvent.click(editButtons[1]!);
    // API URL and appUrl both default to the same host for this fixture.
    const apiInput = screen.getByPlaceholderText("https://api.example.com");
    fireEvent.change(apiInput, {
      target: { value: "http://127.0.0.1:9999" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.switchDesktopServer).toHaveBeenCalledWith("personal");
      expect(mocks.toastSuccess).toHaveBeenCalledWith(
        "Server updated — reloading…",
      );
    });
  });

  it("shows the dev-mode hint when the list is not editable", async () => {
    mocks.listServers.mockResolvedValue({
      ok: true,
      servers: { ...personalState, editable: false },
    });
    render(<ServersSettingsTab />);
    expect(await screen.findByText("Dev builds cannot switch")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add server" })).toBeNull();
  });
});
