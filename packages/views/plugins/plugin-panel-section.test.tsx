import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enIssues from "../locales/en/issues.json";

const data = vi.hoisted(() => ({
  installed: { plugins: [] as Array<Record<string, unknown>> },
  flagEnabled: true,
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: data.installed, isLoading: false, isError: false }),
}));
vi.mock("@multica/core/plugins", () => ({ pluginInstallationsOptions: () => ({ queryKey: ["plugins"] }) }));
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
    source_url: "https://example.com/plugin/multica.plugin.json",
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
    data.flagEnabled = true;
  });

  it("mounts an enabled issue_panel in a sandbox that never gets allow-same-origin", () => {
    data.installed.plugins = [installation()];
    render(<PluginPanelSection issueId="issue-1" />, { wrapper: Wrapper });

    const frame = screen.getByTitle("Hello Panel — Hello");
    // allow-scripts WITHOUT allow-same-origin is the entire isolation boundary.
    // The pairing is what the HTML spec calls out as defeating the sandbox, so
    // this assertion is the regression guard for the whole surface model.
    expect(frame).toHaveAttribute("sandbox", "allow-scripts");
    expect(frame.getAttribute("sandbox")).not.toContain("allow-same-origin");
    // srcDoc, not src: the host generates the document so it can set the CSP.
    expect(frame).toHaveAttribute("srcdoc");
    expect(frame).not.toHaveAttribute("src");
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

  it("says why a local install cannot render instead of showing an empty frame", () => {
    data.installed.plugins = [installation({ source_url: "local:hello" })];
    render(<PluginPanelSection issueId="issue-1" />, { wrapper: Wrapper });

    expect(screen.queryByTitle("Hello Panel — Hello")).not.toBeInTheDocument();
    expect(screen.getByText(/installed from a local source/i)).toBeInTheDocument();
  });
});
