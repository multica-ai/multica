import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockPreview = vi.hoisted(() => vi.fn());
const mockInstall = vi.hoisted(() => vi.fn());
const mockConfigure = vi.hoisted(() => vi.fn());
const mockSetEnabled = vi.hoisted(() => vi.fn());
const mockUninstall = vi.hoisted(() => vi.fn());

const data = vi.hoisted(() => ({
  installed: { plugins: [] as Array<Record<string, unknown>> },
  role: "owner" as "owner" | "admin" | "member",
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: data.installed, isLoading: false, isError: false }),
}));

vi.mock("@multica/core/plugins", () => ({
  pluginInstallationsOptions: () => ({ queryKey: ["plugins", "installed"] }),
  usePreviewPlugin: () => ({ mutateAsync: mockPreview, isPending: false }),
  useInstallPlugin: () => ({ mutateAsync: mockInstall, isPending: false }),
  useConfigurePlugin: () => ({ mutateAsync: mockConfigure, isPending: false }),
  useSetPluginEnabled: () => ({ mutateAsync: mockSetEnabled, isPending: false }),
  useUninstallPlugin: () => ({ mutateAsync: mockUninstall, isPending: false }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1", name: "Acme", slug: "acme" }),
}));

vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: data.role, isLoading: false }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { PluginsTab } from "./plugins-tab";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function Wrapper({ children }: { children: ReactNode }) {
  return <I18nProvider locale="en" resources={TEST_RESOURCES}>{children}</I18nProvider>;
}

const INSTALLATION = {
  id: "installation-1",
  plugin_key: "com.example.hello",
  name: "Hello Panel",
  description: "A greeting panel.",
  version: "1.0.0",
  source_url: "https://example.com/multica.plugin.json",
  enabled: true,
  granted_scopes: ["issues:read", "comments:write", "net:example.com"],
  config_schema: [
    { key: "repo", type: "string", label: "Repo", required: true, options: [] },
    { key: "token", type: "secret", label: "Token", required: true, options: [] },
  ],
  config: { repo: "multica-ai/multica" },
  configured_secrets: ["token"],
  surfaces: [{ key: "hello", type: "issue_panel", name: "Hello", entry: "ui/main.js", platforms: [] }],
  hooks: [],
  resources: [],
  created_at: "2026-08-18T00:00:00Z",
  updated_at: "2026-08-18T00:00:00Z",
};

const PREVIEW = {
  manifest: {
    key: "com.example.hello",
    name: "Hello Panel",
    description: "A greeting panel.",
    version: "1.0.0",
    author: { name: "example" },
  },
  scopes: ["issues:read", "comments:write", "net:example.com"],
  config_schema: [],
  installed: false,
  added_scopes: [],
};

describe("PluginsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    data.role = "owner";
    data.installed.plugins = [];
    mockPreview.mockResolvedValue(PREVIEW);
    mockInstall.mockResolvedValue(INSTALLATION);
    mockConfigure.mockResolvedValue(INSTALLATION);
    mockSetEnabled.mockResolvedValue(INSTALLATION);
    mockUninstall.mockResolvedValue(undefined);
  });

  it("shows the scope consent screen before anything is installed", async () => {
    const user = userEvent.setup();
    render(<PluginsTab />, { wrapper: Wrapper });

    await user.type(
      screen.getByPlaceholderText("https://example.com/multica.plugin.json"),
      "https://example.com/multica.plugin.json",
    );
    await user.click(screen.getByRole("button", { name: "Review" }));

    // The raw scope strings are the trust model, so they must be on screen
    // verbatim alongside their plain-language meaning.
    await screen.findByText("This Plugin is asking for the following access");
    expect(screen.getByText("issues:read")).toBeInTheDocument();
    expect(screen.getByText("comments:write")).toBeInTheDocument();
    expect(screen.getByText("net:example.com")).toBeInTheDocument();
    expect(screen.getByText("Send data to example.com")).toBeInTheDocument();
    expect(mockInstall).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Grant and install" }));
    await waitFor(() => expect(mockInstall).toHaveBeenCalledWith({
      source_url: "https://example.com/multica.plugin.json",
      granted_scopes: ["issues:read", "comments:write", "net:example.com"],
    }));
  });

  it("renders the configuration form from the manifest and never shows a stored secret", async () => {
    data.installed.plugins = [INSTALLATION];
    const user = userEvent.setup();
    render(<PluginsTab />, { wrapper: Wrapper });

    const repo = screen.getByDisplayValue("multica-ai/multica");
    expect(repo).toBeInTheDocument();

    const secret = screen.getByPlaceholderText("Saved — enter a new value to replace it");
    expect(secret).toHaveAttribute("type", "password");
    expect(secret).toHaveValue("");

    // Saving without retyping the secret must not send an empty value: that
    // would clear a stored secret as a side effect of an unrelated edit.
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mockConfigure).toHaveBeenCalledWith({
      installationId: "installation-1",
      values: { repo: "multica-ai/multica" },
    }));

    mockConfigure.mockClear();
    await user.type(secret, "new-token");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mockConfigure).toHaveBeenCalledWith({
      installationId: "installation-1",
      values: { repo: "multica-ai/multica", token: "new-token" },
    }));
  });

  it("disables and uninstalls an installed Plugin", async () => {
    data.installed.plugins = [INSTALLATION];
    const user = userEvent.setup();
    render(<PluginsTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("switch", { name: "Enable Plugin" }));
    await waitFor(() => expect(mockSetEnabled).toHaveBeenCalledWith({
      installationId: "installation-1",
      enabled: false,
    }));

    await user.click(screen.getByRole("button", { name: "Uninstall" }));
    await waitFor(() => expect(mockUninstall).toHaveBeenCalledWith("installation-1"));
  });

  it("blocks management for a non-admin member", () => {
    data.role = "member";
    data.installed.plugins = [INSTALLATION];
    render(<PluginsTab />, { wrapper: Wrapper });

    expect(screen.getByText("Read-only access")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Review" })).not.toBeInTheDocument();
    // Base UI's Switch marks the disabled state with aria-disabled rather than
    // the native attribute, so assert what a screen reader actually sees.
    expect(screen.getByRole("switch", { name: "Enable Plugin" })).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByRole("button", { name: "Uninstall" })).toBeDisabled();
  });
});
