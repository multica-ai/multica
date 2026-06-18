// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

// Keep the real parseWithFallback so the malformed-response path is exercised
// end-to-end (API Response Compatibility rule); only the network call is mocked.
vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: { ...actual.api, cerebroRequest: mockCerebroRequest },
  };
});

import { CerebroCapabilitiesTab } from "./capabilities-tab";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Tine",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "cloud",
  runtime_config: {},
  custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
  persona_sandbox: "",
};

function renderTab(agent: Agent = baseAgent) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <CerebroCapabilitiesTab agent={agent} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("CerebroCapabilitiesTab", () => {
  it("renders every section as pills from a well-formed response", async () => {
    mockCerebroRequest.mockResolvedValue({
      agent_id: "agent-1",
      name: "Tine",
      model: "claude",
      description: "",
      skills: [{ id: "s1", name: "deploy", description: "Ship a PR" }],
      tools: [
        { key: "add_comment", permission: "allow", decided_by: "runtime" },
        { key: "create_local_runtime", permission: "deny", decided_by: "agent" },
      ],
      repos: [
        {
          url: "https://github.com/firtal-group/firtal-cerebro",
          permissions: [
            { key: "repo.read", title: "Read code", permission: "allow" },
            { key: "repo.push", title: "Push changes", permission: "deny" },
          ],
        },
      ],
      connections: [
        {
          name: "bigquery",
          display_name: "BigQuery",
          type: "mcp_http",
          url: "https://bq-mcp.firtal.internal",
          internal: true,
          enabled: true,
          tools: [
            { name: "bigquery.query", permission: "allow" },
            { name: "bigquery.insert", permission: "deny" },
          ],
          endpoints: [],
        },
        {
          name: "registry",
          type: "api",
          url: "https://registry.firtal.com",
          enabled: false,
          tools: [],
          endpoints: [{ path: "/datasets", methods: ["GET"] }],
        },
      ],
      credentials: [{ name: "GITHUB_TOKEN", type: "secret", description: "" }],
      infisical_secrets: [{ environment: "prod", path: "/Cloudflare" }],
      agent_secrets: {
        source: "agent_custom_env",
        status: "known",
        count: 2,
        names: ["OPENAI_API_KEY", "SLIPLANE_KEY"],
        redacted: false,
        runtime_id: "",
      },
      runtime_secrets: {
        source: "runtime",
        status: "known",
        count: 1,
        names: ["ANTHROPIC_API_KEY"],
        redacted: false,
        runtime_id: "runtime-1",
      },
      observed_access: {
        status: "known",
        window_days: 30,
        task_count: 4,
        drift_count: 1,
        tools: [
          { name: "Bash", uses: 12, last_used: "2026-06-10T08:00:00Z", permission: "allow", status: "allowed", drift: false },
          { name: "WebFetch", uses: 2, last_used: "", permission: "", status: "unmapped", drift: true },
        ],
      },
      limits: {
        sandbox: { network_allowlist: ["api.github.com:443"] },
        mcp_servers: ["multica"],
        has_mcp_config: true,
      },
    });

    renderTab();

    // Every section is present.
    expect(await screen.findByText("Skills")).toBeInTheDocument();
    expect(screen.getByText("Tools")).toBeInTheDocument();
    expect(screen.getByText("Repos")).toBeInTheDocument();
    expect(screen.getByText("Connections")).toBeInTheDocument();
    expect(screen.getByText("Credentials")).toBeInTheDocument();
    expect(screen.getByText("Infisical secrets")).toBeInTheDocument();
    expect(screen.getByText("Limits")).toBeInTheDocument();

    // Pills render the names.
    expect(screen.getByText("deploy")).toBeInTheDocument();
    expect(screen.getByText("add_comment")).toBeInTheDocument();
    expect(screen.getByText("create_local_runtime")).toBeInTheDocument();

    // Repo permissions as pills.
    expect(screen.getByText("Read code")).toBeInTheDocument();
    expect(screen.getByText("Push changes")).toBeInTheDocument();
    expect(
      screen.getByText("github.com/firtal-group/firtal-cerebro"),
    ).toBeInTheDocument();

    // Connection tools (with per-tool verdicts) + the disabled marker.
    expect(screen.getByText("BigQuery")).toBeInTheDocument();
    expect(screen.getByText("bigquery.query")).toBeInTheDocument();
    expect(screen.getByText("bigquery.insert")).toBeInTheDocument();
    expect(screen.getByText("disabled")).toBeInTheDocument();
    expect(screen.getByText("/datasets")).toBeInTheDocument();

    // Credentials + Infisical names (never values).
    expect(screen.getByText("GITHUB_TOKEN")).toBeInTheDocument();
    expect(screen.getByText("/Cloudflare")).toBeInTheDocument();

    // Agent + runtime secrets (names-only) — TECH-3738 Bid A.
    expect(screen.getByText("Agent secrets")).toBeInTheDocument();
    expect(screen.getByText("Runtime secrets")).toBeInTheDocument();
    expect(screen.getByText("OPENAI_API_KEY")).toBeInTheDocument();
    expect(screen.getByText("SLIPLANE_KEY")).toBeInTheDocument();
    expect(screen.getByText("ANTHROPIC_API_KEY")).toBeInTheDocument();

    // Limits.
    expect(screen.getByText("multica")).toBeInTheDocument();

    // Observed access (TECH-3738 Bid B): the used tools + the drift verdict.
    expect(screen.getByText("Observed access")).toBeInTheDocument();
    expect(screen.getByText("WebFetch")).toBeInTheDocument();
    expect(
      screen.getByText(/the declared policy does not account for them/i),
    ).toBeInTheDocument();
    // Honest-scope disclaimer: secret use is not tracked.
    expect(
      screen.getByText(/Secret and credential/i),
    ).toBeInTheDocument();
  });

  it("renders workspace and built-in skills as labelled layers", async () => {
    mockCerebroRequest.mockResolvedValue({
      agent_id: "agent-1",
      name: "Tine",
      model: "claude",
      skills: [
        { id: "s1", name: "deploy", description: "Ship a PR", source: "workspace" },
        {
          id: "",
          name: "multica-mentioning",
          description: "How to @mention",
          source: "builtin",
        },
      ],
    });

    renderTab();

    expect(await screen.findByText("deploy")).toBeInTheDocument();
    expect(screen.getByText("multica-mentioning")).toBeInTheDocument();
    // The built-in pill carries an explicit "built-in" tag.
    expect(screen.getByText("· built-in")).toBeInTheDocument();
    // The layer note explains why the CLI count differs.
    expect(screen.getByText(/workspace ·.*built-in/i)).toBeInTheDocument();
  });

  it("redacts secret names for non-privileged callers but keeps the count", async () => {
    mockCerebroRequest.mockResolvedValue({
      agent_id: "agent-1",
      name: "Tine",
      model: "claude",
      agent_secrets: {
        source: "agent_custom_env",
        status: "known",
        count: 3,
        names: [],
        redacted: true,
        runtime_id: "",
      },
      runtime_secrets: {
        source: "runtime",
        status: "unknown",
        count: 0,
        names: [],
        redacted: false,
        runtime_id: "runtime-1",
      },
    });

    renderTab();

    // The count is shown, but no secret name leaks, and the lock note explains why.
    expect(await screen.findByText("Agent secrets")).toBeInTheDocument();
    expect(
      screen.getByText(/names visible to workspace owners\/admins only/i),
    ).toBeInTheDocument();
    // Runtime source could not be determined → explicit "unknown", not "none".
    expect(
      screen.getByText("Could not determine secrets for this source."),
    ).toBeInTheDocument();
  });

  it("shows the observed-access empty state when the agent logged no tool use", async () => {
    mockCerebroRequest.mockResolvedValue({
      agent_id: "agent-1",
      name: "Tine",
      model: "claude",
      observed_access: {
        status: "not_configured",
        window_days: 30,
        task_count: 0,
        drift_count: 0,
        tools: [],
      },
    });

    renderTab();

    expect(await screen.findByText("Observed access")).toBeInTheDocument();
    expect(
      screen.getByText("No tool use recorded in this window."),
    ).toBeInTheDocument();
  });

  it("survives a malformed response without throwing (fallback to empty)", async () => {
    // Missing fields, wrong types, null arrays — every defense at once.
    mockCerebroRequest.mockResolvedValue({
      agent_id: 123,
      skills: null,
      tools: "not-an-array",
      repos: 7,
      connections: undefined,
      credentials: undefined,
      infisical_secrets: 42,
      limits: { mcp_servers: null },
    });

    renderTab();

    // The card still renders its section scaffold and shows empty states
    // rather than white-screening.
    expect(await screen.findByText("Skills")).toBeInTheDocument();
    expect(screen.getByText("No skills loaded.")).toBeInTheDocument();
    expect(screen.getByText("No tools resolved.")).toBeInTheDocument();
    expect(screen.getByText("No repositories.")).toBeInTheDocument();
    expect(screen.getByText("No connections.")).toBeInTheDocument();
    expect(screen.getByText("No credentials bound.")).toBeInTheDocument();
    expect(screen.getByText("No Infisical folders.")).toBeInTheDocument();
    // New secret sections still scaffold from schema defaults (TECH-3738 Bid A).
    expect(screen.getByText("Agent secrets")).toBeInTheDocument();
    expect(screen.getByText("Runtime secrets")).toBeInTheDocument();
  });
});
