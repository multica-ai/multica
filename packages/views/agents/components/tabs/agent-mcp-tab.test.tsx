// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent } from "@multica/core/types";
import { configStore } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

// AgentMcpTab reads its connection list + toolkit catalog from two queries and
// writes through the useUpdateAgentAllowlist mutation. We stub all three at the
// module boundary so the tests assert the tab's own logic (which slugs are
// selectable, what the toggle computes, the empty/redacted branches) rather
// than the query/mutation plumbing, which is covered elsewhere.
const connectionsRef = vi.hoisted(() => ({
  current: [] as { toolkit_slug: string; status: string }[],
}));
const toolkitsRef = vi.hoisted(() => ({
  current: [] as { slug: string; name: string }[],
}));
const queryStateRef = vi.hoisted(() => ({
  isLoading: false,
  isError: false,
}));
const queryCallsRef = vi.hoisted(() => ({
  current: [] as { queryKey: unknown[]; enabled?: boolean }[],
}));
const mutateSpy = vi.hoisted(() => vi.fn());
const isPendingRef = vi.hoisted(() => ({ current: false }));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    queryCallsRef.current.push(opts);
    const key = JSON.stringify(opts.queryKey);
    if (queryStateRef.isLoading) return { data: undefined, isLoading: true, isError: false };
    if (queryStateRef.isError) return { data: undefined, isLoading: false, isError: true };
    if (key.includes("connections"))
      return { data: connectionsRef.current, isLoading: false, isError: false };
    if (key.includes("toolkits"))
      return { data: toolkitsRef.current, isLoading: false, isError: false };
    return { data: undefined, isLoading: false, isError: false };
  },
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/composio", () => ({
  composioConnectionsOptions: () => ({ queryKey: ["composio", "connections"] }),
  composioToolkitsOptions: () => ({ queryKey: ["composio", "toolkits"] }),
}));

vi.mock("@multica/core/agents", () => ({
  useUpdateAgentAllowlist: () => ({
    mutate: mutateSpy,
    isPending: isPendingRef.current,
  }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ settings: () => "/ws/settings" }),
}));

vi.mock("../../../navigation", () => ({
  AppLink: ({ href, children }: { href: string; children: ReactNode }) => (
    <a href={href} data-testid="app-link">
      {children}
    </a>
  ),
}));

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

import { AgentMcpTab } from "./agent-mcp-tab";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-06-30T00:00:00Z",
  updated_at: "2026-06-30T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderTab(overrides: Partial<Agent> = {}) {
  const agent = { ...baseAgent, ...overrides };
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <AgentMcpTab agent={agent} />
    </I18nProvider>,
  );
}

describe("AgentMcpTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    connectionsRef.current = [
      { toolkit_slug: "notion", status: "active" },
      { toolkit_slug: "slack", status: "active" },
    ];
    toolkitsRef.current = [
      { slug: "notion", name: "Notion" },
      { slug: "slack", name: "Slack" },
    ];
    queryStateRef.isLoading = false;
    queryStateRef.isError = false;
    isPendingRef.current = false;
    queryCallsRef.current = [];
    configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: true });
  });

  it("renders nothing and disables Composio queries when the feature flag is off", () => {
    configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: false });

    const { container } = renderTab({ composio_toolkit_allowlist: ["notion"] });

    expect(container.firstChild).toBeNull();
    expect(queryCallsRef.current).toHaveLength(2);
    expect(queryCallsRef.current.every((call) => call.enabled === false)).toBe(true);
  });

  it("lists active connections with checkbox state reflecting the allowlist", () => {
    renderTab({ composio_toolkit_allowlist: ["notion"] });

    const notion = screen.getByLabelText(/Allow Notion for this agent/i);
    const slack = screen.getByLabelText(/Allow Slack for this agent/i);
    expect(notion.getAttribute("aria-checked")).toBe("true");
    expect(slack.getAttribute("aria-checked")).toBe("false");
  });

  it("checking a toolkit writes the augmented allowlist via the mutation", async () => {
    const user = userEvent.setup();
    renderTab({ composio_toolkit_allowlist: [] });

    await user.click(screen.getByLabelText(/Allow Notion for this agent/i));

    expect(mutateSpy).toHaveBeenCalledTimes(1);
    expect(mutateSpy.mock.calls[0]?.[0]).toEqual(["notion"]);
  });

  it("unchecking a toolkit removes only that slug", async () => {
    const user = userEvent.setup();
    renderTab({ composio_toolkit_allowlist: ["notion", "slack"] });

    await user.click(screen.getByLabelText(/Allow Notion for this agent/i));

    expect(mutateSpy).toHaveBeenCalledTimes(1);
    expect(mutateSpy.mock.calls[0]?.[0]).toEqual(["slack"]);
  });

  it("only offers active connections — expired/revoked are not selectable", () => {
    connectionsRef.current = [
      { toolkit_slug: "notion", status: "active" },
      { toolkit_slug: "github", status: "expired" },
    ];
    renderTab({ composio_toolkit_allowlist: [] });

    expect(screen.getByLabelText(/Allow Notion for this agent/i)).toBeTruthy();
    expect(screen.queryByLabelText(/Allow github for this agent/i)).toBeNull();
  });

  it("shows an empty state with a Settings link when there are no active connections", () => {
    connectionsRef.current = [];
    renderTab({ composio_toolkit_allowlist: [] });

    expect(screen.getByText(/No connected apps yet/i)).toBeTruthy();
    const link = screen.getByTestId("app-link");
    expect(link.getAttribute("href")).toBe("/ws/settings?tab=integrations");
  });

  it("renders a defensive hidden state when the allowlist is redacted", () => {
    renderTab({ composio_toolkit_allowlist_redacted: true });

    expect(screen.getByText(/hidden from your view/i)).toBeTruthy();
    expect(screen.queryByLabelText(/Allow Notion for this agent/i)).toBeNull();
  });

  it("shows the strong workspace warning for a public_to-workspace agent", () => {
    renderTab({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "workspace", target_id: null }],
      composio_toolkit_allowlist: [],
    });

    expect(
      screen.getByText(/any workspace member may use these Composio apps/i),
    ).toBeTruthy();
  });

  it("shows the generic shared warning for a public_to-member agent", () => {
    renderTab({
      visibility: "private",
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "member", target_id: "user-2" }],
      composio_toolkit_allowlist: [],
    });

    expect(screen.getByText(/This agent is shared\./i)).toBeTruthy();
    expect(
      screen.queryByText(/any workspace member may use these Composio apps/i),
    ).toBeNull();
  });

  it("shows no sharing warning for a private agent", () => {
    renderTab({
      visibility: "private",
      permission_mode: "private",
      invocation_targets: [],
      composio_toolkit_allowlist: ["notion"],
    });

    expect(screen.queryByText(/This agent is shared\./i)).toBeNull();
    expect(
      screen.queryByText(/any workspace member may use these Composio apps/i),
    ).toBeNull();
  });

  // Regression: GH #4915. Legacy self-host backends / stale caches may
  // return an agent without `invocation_targets` even though the modern
  // type declares a required array. The tab must degrade to the "not
  // workspace-public" copy instead of crashing the whole detail route
  // with "Cannot read properties of undefined (reading 'some')".
  it("does not crash when invocation_targets is undefined", () => {
    expect(() =>
      renderTab({
        permission_mode: "public_to",
        invocation_targets:
          undefined as unknown as Agent["invocation_targets"],
        composio_toolkit_allowlist: ["notion"],
      }),
    ).not.toThrow();
    // Falls back to the generic shared warning (no workspace target
    // resolves to `false`), not the workspace-wide one.
    expect(
      screen.queryByText(/any workspace member may use these Composio apps/i),
    ).toBeNull();
  });
});
