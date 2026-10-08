import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enIssues from "../locales/en/issues.json";

const data = vi.hoisted(() => ({
  installed: { plugins: [] as Array<Record<string, unknown>> },
  launch: {
    url: "https://plugin-content.example.test/plugin-surfaces/opaque",
    bridge_token: "single-use-proof",
    version: "1.0.0",
    digest: "abc",
  } as Record<string, unknown> | null,
  flagEnabled: true,
}));

vi.mock("@tanstack/react-query", () => ({
  // Two different queries reach this mock: the installation list and one
  // surface's code. They are told apart by the query key the options object
  // carries, so a test can make the code unavailable without touching the list.
  useQuery: (options: { queryKey?: readonly unknown[] }) =>
    options?.queryKey?.[0] === "surface"
      ? { data: data.launch, isPending: false, isError: data.launch === null }
      : { data: data.installed, isLoading: false, isError: false },
}));
vi.mock("@multica/core/plugins", () => ({
  pluginInstallationsOptions: () => ({ queryKey: ["plugins"] }),
  pluginSurfaceLaunchOptions: () => ({ queryKey: ["surface"] }),
}));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ id: "workspace-1", name: "Acme", slug: "acme" }) }));
vi.mock("@multica/core/config", () => ({ useFeatureEnabled: () => data.flagEnabled }));
vi.mock("../platform/local-directory", () => ({ isDesktopShell: () => false }));

import { PluginPanelSection } from "./plugin-panel-section";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

function Wrapper({ children }: { children: ReactNode }) {
  return <I18nProvider locale="en" resources={TEST_RESOURCES}>{children}</I18nProvider>;
}

function installation(overrides: Record<string, unknown> = {}) {
  return {
    id: "installation-1",
    plugin_key: "com.example.hello",
    name: "Hello Panel",
    version: "1.0.0",
    package_version_id: "version-1",
    enabled: true,
    granted_scopes: ["issues:read"],
    config_schema: [],
    config: {},
    configured_secrets: [],
    surfaces: [{ key: "hello", type: "issue_panel", name: "Hello", entry: "ui/main.js", platforms: [] }],
    hooks: [],
    resources: [],
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

describe("PluginPanelSection", () => {
  beforeEach(() => {
    data.installed.plugins = [];
    data.launch = {
      url: "https://plugin-content.example.test/plugin-surfaces/opaque",
      bridge_token: "single-use-proof",
      version: "1.0.0",
      digest: "abc",
    };
    data.flagEnabled = true;
  });

  it("mounts an enabled issue_panel with an opaque nested plugin frame", () => {
    data.installed.plugins = [installation()];
    render(<PluginPanelSection issueId="issue-1" />, { wrapper: Wrapper });

    const frame = screen.getByTitle("Hello Panel — Hello");
    // The visible iframe is trusted host code. Its srcdoc creates the actual
    // plugin iframe with allow-scripts and deliberately no allow-same-origin.
    expect(frame).toHaveAttribute("sandbox", "allow-scripts allow-same-origin");
    expect(frame).toHaveAttribute("srcdoc");
    expect(frame).not.toHaveAttribute("src");
    const srcdoc = frame.getAttribute("srcdoc") ?? "";
    expect(srcdoc).toContain('child.setAttribute("sandbox", "allow-scripts")');
    expect(srcdoc).not.toContain('child.setAttribute("sandbox", "allow-scripts allow-same-origin")');
  });

  it("does not mount a disabled installation", () => {
    data.installed.plugins = [installation({ enabled: false })];
    render(<PluginPanelSection issueId="issue-1" />, { wrapper: Wrapper });
    expect(screen.queryByTitle("Hello Panel — Hello")).not.toBeInTheDocument();
  });

  it("ignores surfaces that belong somewhere other than the issue page", () => {
    data.installed.plugins = [installation({
      surfaces: [{ key: "side", type: "sidebar_panel", name: "Side", entry: "ui/main.js", platforms: [] }],
    })];
    render(<PluginPanelSection issueId="issue-1" />, { wrapper: Wrapper });
    expect(screen.queryByTitle("Hello Panel — Side")).not.toBeInTheDocument();
  });

  it("does not render a surface this platform is excluded from", () => {
    // platforms is a filter, not a hint: a desktop-only panel appearing on web
    // is a surface running somewhere its author said it should not.
    data.installed.plugins = [installation({
      surfaces: [{ key: "hello", type: "issue_panel", name: "Hello", entry: "ui/main.js", platforms: ["desktop"] }],
    })];
    render(<PluginPanelSection issueId="issue-1" />, { wrapper: Wrapper });
    expect(screen.queryByTitle("Hello Panel — Hello")).not.toBeInTheDocument();
  });

  it("renders a surface that declares no platforms at all", () => {
    data.installed.plugins = [installation()];
    render(<PluginPanelSection issueId="issue-1" />, { wrapper: Wrapper });
    expect(screen.getByTitle("Hello Panel — Hello")).toBeInTheDocument();
  });

  it("renders nothing when the feature flag is off", () => {
    data.flagEnabled = false;
    data.installed.plugins = [installation()];
    const { container } = render(<PluginPanelSection issueId="issue-1" />, { wrapper: Wrapper });
    expect(container).toBeEmptyDOMElement();
  });

  it("says the surface could not load instead of showing an empty frame", () => {
    // An empty body is what a malformed response parses to. Mounting a frame
    // for it would look like a working panel that renders nothing.
    data.launch = null;
    data.installed.plugins = [installation()];
    render(<PluginPanelSection issueId="issue-1" />, { wrapper: Wrapper });

    expect(screen.queryByTitle("Hello Panel — Hello")).not.toBeInTheDocument();
    expect(screen.getByText(/could not load its interface/i)).toBeInTheDocument();
  });

  it("loads only Multica's dedicated content origin", () => {
    data.installed.plugins = [installation()];
    render(<PluginPanelSection issueId="issue-1" />, { wrapper: Wrapper });

    const srcdoc = screen.getByTitle("Hello Panel — Hello").getAttribute("srcdoc") ?? "";
    expect(srcdoc).toContain("frame-src https://plugin-content.example.test");
    expect(srcdoc).not.toContain("frame-src https:;");
  });

  it("shows a useful plugin bootstrap failure", () => {
    data.installed.plugins = [installation()];
    render(<PluginPanelSection issueId="issue-1" />, { wrapper: Wrapper });

    const frame = screen.getByTitle("Hello Panel — Hello") as HTMLIFrameElement;
    const event = new MessageEvent("message", { data: { type: "multica:plugin-surface-error" } });
    Object.defineProperty(event, "source", { value: frame.contentWindow, configurable: true });
    act(() => window.dispatchEvent(event));

    expect(screen.getByText("Hello Panel could not load its interface.")).toBeInTheDocument();
  });

  it("clears a previous surface failure when the issue changes", () => {
    data.installed.plugins = [installation()];
    const { rerender } = render(<PluginPanelSection issueId="issue-1" />, { wrapper: Wrapper });

    const frame = screen.getByTitle("Hello Panel — Hello") as HTMLIFrameElement;
    const event = new MessageEvent("message", { data: { type: "multica:plugin-surface-error" } });
    Object.defineProperty(event, "source", { value: frame.contentWindow, configurable: true });
    act(() => window.dispatchEvent(event));
    expect(screen.getByText("Hello Panel could not load its interface.")).toBeInTheDocument();

    rerender(<PluginPanelSection issueId="issue-2" />);
    expect(screen.queryByText("Hello Panel could not load its interface.")).not.toBeInTheDocument();
    expect(screen.getByTitle("Hello Panel — Hello")).toBeInTheDocument();
  });

  it("clears a previous surface failure when its document changes", () => {
    data.installed.plugins = [installation()];
    const { rerender } = render(<PluginPanelSection issueId="issue-1" />, { wrapper: Wrapper });

    const frame = screen.getByTitle("Hello Panel — Hello") as HTMLIFrameElement;
    const originalDocument = frame.getAttribute("srcdoc");
    const event = new MessageEvent("message", { data: { type: "multica:plugin-surface-error" } });
    Object.defineProperty(event, "source", { value: frame.contentWindow, configurable: true });
    act(() => window.dispatchEvent(event));
    expect(screen.getByText("Hello Panel could not load its interface.")).toBeInTheDocument();

    data.launch = {
      url: "https://plugin-content.example.test/plugin-surfaces/replacement",
      bridge_token: "replacement-proof",
      version: "1.0.1",
      digest: "def",
    };
    rerender(<PluginPanelSection issueId="issue-1" />);
    expect(screen.queryByText("Hello Panel could not load its interface.")).not.toBeInTheDocument();
    expect(screen.getByTitle("Hello Panel — Hello").getAttribute("srcdoc")).not.toBe(originalDocument);
  });
});
