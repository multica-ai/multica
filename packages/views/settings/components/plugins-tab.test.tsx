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
const mockPublish = vi.hoisted(() => vi.fn());
const mockDeletePackage = vi.hoisted(() => vi.fn());

const data = vi.hoisted(() => ({
  installed: { plugins: [] as Array<Record<string, unknown>> },
  packages: { packages: [] as Array<Record<string, unknown>> },
  role: "owner" as "owner" | "admin" | "member",
}));

vi.mock("@tanstack/react-query", () => ({
  // Two queries reach this tab: what is installed, and what has been published.
  // They are told apart by the query key so a test can have one without the
  // other — which is the normal state right after a publish.
  useQuery: (options: { queryKey?: readonly unknown[] }) =>
    options?.queryKey?.[1] === "packages"
      ? { data: data.packages, isLoading: false, isError: false }
      : { data: data.installed, isLoading: false, isError: false },
}));

vi.mock("@multica/core/plugins", () => ({
  pluginInstallationsOptions: () => ({ queryKey: ["plugins", "installed"] }),
  pluginPackagesOptions: () => ({ queryKey: ["plugins", "packages"] }),
  usePreviewPlugin: () => ({ mutateAsync: mockPreview, isPending: false }),
  useInstallPlugin: () => ({ mutateAsync: mockInstall, isPending: false }),
  useConfigurePlugin: () => ({ mutateAsync: mockConfigure, isPending: false }),
  useSetPluginEnabled: () => ({ mutateAsync: mockSetEnabled, isPending: false }),
  useUninstallPlugin: () => ({ mutateAsync: mockUninstall, isPending: false }),
  usePublishPluginPackage: () => ({ mutateAsync: mockPublish, isPending: false }),
  useDeletePluginPackage: () => ({ mutateAsync: mockDeletePackage, isPending: false }),
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
  package_version_id: "version-1",
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
  version_id: "version-1",
  version: "1.0.0",
  digest: "0123456789abcdef",
  installed: false,
  added_scopes: [],
};

// One published plugin with two versions: the installed one and a newer one an
// administrator has not adopted. That gap is the normal state after a publish,
// and it is what the version list has to make legible.
const PACKAGE = {
  id: "package-1",
  plugin_key: "com.example.hello",
  name: "Hello Panel",
  created_at: "2026-08-18T00:00:00Z",
  versions: [
    {
      id: "version-2",
      version: "2.0.0",
      digest: "fedcba9876543210",
      size_bytes: 2048,
      published_at: "2026-08-20T00:00:00Z",
      installed: false,
    },
    {
      id: "version-1",
      version: "1.0.0",
      digest: "0123456789abcdef",
      size_bytes: 1024,
      published_at: "2026-08-18T00:00:00Z",
      installed: true,
    },
  ],
};

describe("PluginsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    data.role = "owner";
    data.installed.plugins = [];
    data.packages.packages = [PACKAGE];
    mockPreview.mockResolvedValue(PREVIEW);
    mockInstall.mockResolvedValue(INSTALLATION);
    mockConfigure.mockResolvedValue(INSTALLATION);
    mockSetEnabled.mockResolvedValue(INSTALLATION);
    mockUninstall.mockResolvedValue(undefined);
  });

  it("shows the scope consent screen before anything is installed", async () => {
    data.packages.packages = [{ ...PACKAGE, versions: [{ ...PACKAGE.versions[0] }] }];
    const user = userEvent.setup();
    render(<PluginsTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: "Review and install" }));

    // The raw scope strings are the trust model, so they must be on screen
    // verbatim alongside their plain-language meaning.
    await screen.findByText("This Plugin is asking for the following access");
    expect(screen.getByText("issues:read")).toBeInTheDocument();
    expect(screen.getByText("comments:write")).toBeInTheDocument();
    expect(screen.getByText("net:example.com")).toBeInTheDocument();
    expect(screen.getByText("Send data to example.com")).toBeInTheDocument();
    expect(mockInstall).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Grant and install" }));
    // The version reviewed is the version installed. Anything else would put
    // the consent screen back to describing one artifact while another runs.
    await waitFor(() => expect(mockInstall).toHaveBeenCalledWith({
      version_id: "version-1",
      granted_scopes: ["issues:read", "comments:write", "net:example.com"],
    }));
  });

  it("marks the running version and offers an upgrade only for the others", async () => {
    // A publish does not move an installed workspace, so "which one am I on"
    // has to be answerable on this screen or the guarantee is invisible.
    const user = userEvent.setup();
    render(<PluginsTab />, { wrapper: Wrapper });

    expect(screen.getByText("1.0.0")).toBeInTheDocument();
    expect(screen.getByText("2.0.0")).toBeInTheDocument();
    expect(screen.getByText("In use")).toBeInTheDocument();

    const upgrade = screen.getAllByRole("button", { name: "Review and upgrade" });
    expect(upgrade).toHaveLength(1);

    mockPreview.mockResolvedValue({ ...PREVIEW, version_id: "version-2", version: "2.0.0", installed: true, installed_version: "1.0.0" });
    await user.click(upgrade[0]!);
    await waitFor(() => expect(mockPreview).toHaveBeenCalledWith({ version_id: "version-2" }));
  });

  it("publishes an uploaded package", async () => {
    const user = userEvent.setup();
    mockPublish.mockResolvedValue({ ...PACKAGE, versions: [PACKAGE.versions[0]] });
    render(<PluginsTab />, { wrapper: Wrapper });

    const bundle = new File(["zip bytes"], "plugin.zip", { type: "application/zip" });
    await user.upload(screen.getByLabelText("Upload package"), bundle);
    await waitFor(() => expect(mockPublish).toHaveBeenCalledWith(bundle));
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
    // The whole publish-and-install section is admin-only.
    expect(screen.queryByRole("button", { name: "Upload package" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Review and install" })).not.toBeInTheDocument();
    // Base UI's Switch marks the disabled state with aria-disabled rather than
    // the native attribute, so assert what a screen reader actually sees.
    expect(screen.getByRole("switch", { name: "Enable Plugin" })).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByRole("button", { name: "Uninstall" })).toBeDisabled();
  });
});
