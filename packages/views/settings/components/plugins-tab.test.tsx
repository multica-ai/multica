import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockInstall = vi.hoisted(() => vi.fn());
const mockSetEnabled = vi.hoisted(() => vi.fn());
const mockUpgrade = vi.hoisted(() => vi.fn());
const mockRollback = vi.hoisted(() => vi.fn());
const mockUninstall = vi.hoisted(() => vi.fn());
const mockConfigureRemoteMCP = vi.hoisted(() => vi.fn());
const mockTestRemoteMCP = vi.hoisted(() => vi.fn());
const mockApproveRemoteMCP = vi.hoisted(() => vi.fn());
const mockRevokeRemoteMCP = vi.hoisted(() => vi.fn());
const mockStartRemoteMCPOAuth = vi.hoisted(() => vi.fn());
const mockOpenExternal = vi.hoisted(() => vi.fn());
const mockRefetch = vi.hoisted(() => vi.fn());

const data = vi.hoisted(() => ({
  catalog: {
    supported: true,
    diagnostics: [],
    releases: [{
      plugin_key: "ai.multica.software-delivery",
      name: "Software Delivery",
      description: "Official Multica software-delivery workflow skills.",
      version: "1.1.0",
      publisher: "multica",
      publisher_type: "official",
      trust_tier: "official",
      source_kind: "bundled",
      source_ref: "bundled://ai.multica.software-delivery/1.1.0",
      requested_capabilities: ["agent.skill.contribute"],
      host_api: ">=1.0.0 <2.0.0",
      required_daemon_features: ["execution-manifest-v1", "agent-skill-v1"],
      signature_key_id: "multica-plugin-release-2026-01",
      signature_verified: true,
      manifest_digest: "sha256:manifest",
      archive_digest: "sha256:archive",
      artifact_digest: "sha256:artifact",
      compatible: true,
      contributions: [{
        key: "review-readiness",
        type: "agent.skill.v1",
        name: "review-readiness",
        description: "Verify that a software change is ready for review and handoff.",
        entry_path: "skills/review-readiness/SKILL.md",
        entry_digest: "sha256:entry",
      }],
    }],
  },
  installed: { plugins: [] as Array<Record<string, unknown>> },
  agents: [{ id: "agent-1", name: "Reviewer", archived_at: null }],
  members: [{ user_id: "member-1", name: "Alice", email: "alice@example.com", role: "admin" }],
  role: "owner" as "owner" | "admin" | "member",
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly string[] }) => {
    const key = options.queryKey?.join(":") ?? "";
    if (key.includes("catalog")) return { data: data.catalog, isPending: false, isError: false, refetch: mockRefetch };
    if (key.includes("installed")) return { data: data.installed, isPending: false, isError: false, refetch: mockRefetch };
    if (key.includes("members")) return { data: data.members, isPending: false, isError: false, refetch: mockRefetch };
    return { data: data.agents, isPending: false, isError: false, refetch: mockRefetch };
  },
}));

vi.mock("@multica/core/plugins", () => ({
  comparePluginVersions: (left: string, right: string) => left.localeCompare(right),
  pluginCatalogOptions: () => ({ queryKey: ["plugins", "catalog"] }),
  pluginInstallationsOptions: () => ({ queryKey: ["plugins", "installed"] }),
  useInstallPlugin: () => ({ mutateAsync: mockInstall, isPending: false }),
  useSetPluginEnabled: () => ({ mutateAsync: mockSetEnabled, isPending: false }),
  useUpgradePlugin: () => ({ mutateAsync: mockUpgrade, isPending: false }),
  useRollbackPlugin: () => ({ mutateAsync: mockRollback, isPending: false }),
  useUninstallPlugin: () => ({ mutateAsync: mockUninstall, isPending: false }),
  useConfigurePluginRemoteMCP: () => ({ mutateAsync: mockConfigureRemoteMCP, isPending: false }),
  useTestPluginRemoteMCP: () => ({ mutateAsync: mockTestRemoteMCP, isPending: false }),
  useApprovePluginRemoteMCPTools: () => ({ mutateAsync: mockApproveRemoteMCP, isPending: false }),
  useRevokePluginRemoteMCPCredential: () => ({ mutateAsync: mockRevokeRemoteMCP, isPending: false }),
  useStartPluginRemoteMCPOAuth: () => ({ mutateAsync: mockStartRemoteMCPOAuth, isPending: false }),
}));

vi.mock("../../platform/open-external", () => ({ openExternal: mockOpenExternal }));
vi.mock("../../platform/local-directory", () => ({ isDesktopShell: () => true }));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
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

describe("PluginsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.history.replaceState({}, "", "/");
    data.role = "owner";
    data.catalog.releases[0]!.compatible = true;
    data.catalog.releases[0]!.signature_verified = true;
    data.installed.plugins = [];
    mockInstall.mockResolvedValue({});
    mockSetEnabled.mockResolvedValue({});
    mockUninstall.mockResolvedValue({});
    mockStartRemoteMCPOAuth.mockResolvedValue({ authorization_url: "https://auth.example.test/authorize" });
  });

  it("renders the install review and keeps installation disabled by default", async () => {
    const user = userEvent.setup();
    render(<PluginsTab />, { wrapper: Wrapper });

    expect(screen.getByText("review-readiness")).toBeInTheDocument();
    expect(screen.getByText("agent.skill.contribute")).toBeInTheDocument();
    expect(screen.getByText("Signature verified")).toBeInTheDocument();
    expect(screen.getByText(/Installation is disabled by default/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Install" }));
    await waitFor(() => expect(mockInstall).toHaveBeenCalledWith({
      plugin_key: "ai.multica.software-delivery",
      version: "1.1.0",
    }));
  });

  it("enables an installed Plugin for the workspace through a generic binding", async () => {
    const user = userEvent.setup();
    data.installed.plugins = [{
      id: "installation-1",
      plugin_key: "ai.multica.software-delivery",
      display_name: "Software Delivery",
      desired_version: "1.1.0",
      active_version: "1.1.0",
      enabled: false,
      desired_generation: 1,
      active_generation: 1,
      lifecycle_status: "installed",
      contributions: ["review-readiness"],
      bindings: [],
      remote_mcp: [],
    }];
    render(<PluginsTab />, { wrapper: Wrapper });

    expect(screen.getByText("Disabled")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Enable scope" }));
    await waitFor(() => expect(mockSetEnabled).toHaveBeenCalledWith({
      installationId: "installation-1",
      enabled: true,
      binding: { scope_type: "workspace", scope_id: "workspace-1" },
    }));
  });

  it("fails closed when compatibility or signature evidence is absent", () => {
    data.catalog.releases[0]!.compatible = false;
    data.catalog.releases[0]!.signature_verified = false;
    render(<PluginsTab />, { wrapper: Wrapper });

    expect(screen.getByText("Incompatible release")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Install" })).toBeDisabled();
  });

  it("keeps the catalog visible but actions read-only for members", () => {
    data.role = "member";
    render(<PluginsTab />, { wrapper: Wrapper });

    expect(screen.getByText("Read-only access")).toBeInTheDocument();
    expect(screen.getByText("review-readiness")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Install" })).toBeDisabled();
  });

  it("shows a private unverified installation outside the official catalog", async () => {
    const user = userEvent.setup();
    data.installed.plugins = [{
      id: "private-installation-1",
      plugin_key: "dev.acme.incident-guide",
      display_name: "Incident Guide",
      desired_version: "0.2.0",
      active_version: "0.2.0",
      enabled: false,
      desired_generation: 2,
      active_generation: 2,
      lifecycle_status: "installed",
      description: "Private incident response guidance.",
      publisher: "acme.internal",
      publisher_type: "private_dev",
      trust_tier: "private_dev",
      source_kind: "private_dev",
      source_ref: "private://sha256:archive",
      uploader_id: "member-1",
      manifest_digest: "sha256:manifest",
      archive_digest: "sha256:archive",
      artifact_digest: "sha256:artifact",
      signature_verified: false,
      requested_capabilities: ["agent.skill.contribute"],
      available_versions: ["0.2.0", "0.1.0"],
      contributions: ["incident-guide"],
      contribution_details: [{
        key: "incident-guide",
        type: "agent.skill.v1",
        name: "Incident Guide",
        description: "Guide incident response.",
        entry_path: "skills/incident-guide/SKILL.md",
        entry_digest: "sha256:entry",
      }],
      bindings: [],
      remote_mcp: [],
    }];
    render(<PluginsTab />, { wrapper: Wrapper });

    expect(screen.getByText("Private")).toBeInTheDocument();
    expect(screen.getByText("Unverified")).toBeInTheDocument();
    expect(screen.getByText(/Private workspace upload/)).toBeInTheDocument();
    expect(screen.getByText(/Uploaded by Alice/)).toBeInTheDocument();
    expect(screen.queryByText(/member-1/)).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Uninstall" }));
    await waitFor(() => expect(mockUninstall).toHaveBeenCalledWith("private-installation-1"));
  });

  it("opens OAuth in the platform browser and restores saved advanced configuration", async () => {
    const user = userEvent.setup();
    data.installed.plugins = [{
      id: "private-remote-1",
      plugin_key: "dev.acme.search",
      display_name: "Search",
      desired_version: "0.1.0",
      active_version: "0.1.0",
      enabled: false,
      desired_generation: 1,
      active_generation: 1,
      lifecycle_status: "installed",
      publisher: "acme.internal",
      publisher_type: "private_dev",
      trust_tier: "private_dev",
      source_kind: "private_dev",
      source_ref: "private://sha256:search",
      signature_verified: false,
      requested_capabilities: ["tool.remote-mcp.connect"],
      available_versions: ["0.1.0"],
      contributions: ["search"],
      contribution_details: [],
      bindings: [],
      remote_mcp: [{
        contribution_key: "search",
        default_endpoint: "https://default.example.test/mcp",
        preferred_auth: "oauth",
        supported_auth: ["oauth"],
        endpoint: "https://saved.example.test/mcp",
        auth_type: "oauth",
        public_config: { locale: "en" },
        failure_policy: "optional",
        credential_state: "missing",
        approved_tools: [],
        discovered_tools: [],
        reviewed: false,
        ready: false,
      }],
    }];
    render(<PluginsTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: "Connect" }));
    await waitFor(() => expect(mockStartRemoteMCPOAuth).toHaveBeenCalledWith(expect.objectContaining({
      installationId: "private-remote-1",
      contributionKey: "search",
      request: expect.objectContaining({
        endpoint: "https://saved.example.test/mcp",
        public_config: { locale: "en" },
        failure_policy: "optional",
      }),
    })));
    expect(mockOpenExternal).toHaveBeenCalledWith("https://auth.example.test/authorize");

    await user.click(screen.getByText("Advanced configuration"));
    expect(screen.getByLabelText("HTTPS endpoint")).toHaveValue("https://saved.example.test/mcp");
    expect(screen.getByLabelText("Public configuration (JSON object)")).toHaveValue('{\n  "locale": "en"\n}');
  });

  it("consumes a successful OAuth callback and refreshes installation state", async () => {
    window.history.replaceState({}, "", "/settings?tab=plugins&remote_mcp_connected=1#plugins");

    render(<PluginsTab />, { wrapper: Wrapper });

    await waitFor(() => expect(mockRefetch).toHaveBeenCalled());
    expect(window.location.search).toBe("?tab=plugins");
    expect(window.location.hash).toBe("#plugins");
  });
});
