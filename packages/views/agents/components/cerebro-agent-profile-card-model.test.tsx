// @vitest-environment jsdom
//
// CEREBRO-PATCH(profile-card-model-row) coverage: FIR-3406 added a Model row
// to the agent hover card. These tests pin that row — model id when set, the
// Default label when not — so an upstream sync that drops the patch fails CI.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

// useWorkspaceId is a Context-backed hook in core; stub it to a static id so
// the card runs outside a WorkspaceIdProvider in tests.
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// Paths: agentDetail for Detail →, plus useCurrentWorkspace for Spend today
// (AgentProfileSpend → useCostFormatter → useDisplayCurrencyQuery).
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    agentDetail: (id: string) => `/test/agents/${id}`,
  }),
  useCurrentWorkspace: () => ({ id: "ws-1" }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getBaseUrl: () => "http://127.0.0.1:8080",
  },
}));

// AppLink is just a plain anchor here — wiring the navigation adapter would
// add nothing to these assertions.
vi.mock("../../navigation", () => ({
  AppLink: ({
    href,
    children,
    ...rest
  }: {
    href: string;
    children: React.ReactNode;
    [k: string]: unknown;
  }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}));

// Each test sets these up via beforeEach.
const mockAgents = vi.hoisted(() => ({ current: [] as unknown[] }));

// Distinguish queries by queryKey shape — the card spreads three
// queryOptions records into useQuery:
//   ["workspaces", wsId, "agents"]  — agent list
//   ["workspaces", wsId, "members"] — member list
//   ["runtimes",  wsId, "list"]     — runtime list
vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return {
    ...actual,
    useQuery: (opts: { queryKey: readonly unknown[] }) => {
      const key = opts.queryKey;
      if (key[0] === "workspaces" && key[2] === "agents") {
        return { data: mockAgents.current, isLoading: false };
      }
      return { data: [], isLoading: false };
    },
  };
});

vi.mock("@multica/core/agents", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/agents")>(
      "@multica/core/agents",
    );
  return {
    ...actual,
    useAgentPresenceDetail: () => ({
      availability: "online",
      workload: "idle",
      runningCount: 0,
      queuedCount: 0,
      capacity: 1,
    }),
  };
});

import { AgentProfileCard } from "./agent-profile-card";

function makeAgent(overrides: Record<string, unknown> = {}) {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    runtime_id: "rt-1",
    name: "Squirtle",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local" as const,
    runtime_config: {},
    custom_args: [],
    visibility: "private" as const,
    status: "idle" as const,
    max_concurrent_tasks: 1,
    model: "",
    owner_id: "user-me",
    skills: [],
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function renderCard() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <AgentProfileCard agentId="agent-1" />
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  cleanup();
  mockAgents.current = [makeAgent()];
});

describe("AgentProfileCard model row", () => {
  it("shows the agent's model id when a model override is set", () => {
    mockAgents.current = [makeAgent({ model: "claude-opus-5" })];

    renderCard();

    expect(
      screen.getByText(enAgents.profile_card.model_label),
    ).toBeInTheDocument();
    expect(screen.getByText("claude-opus-5")).toBeInTheDocument();
  });

  it("falls back to the Default label when no model override is set", () => {
    mockAgents.current = [makeAgent({ model: "" })];

    renderCard();

    expect(
      screen.getByText(enAgents.profile_card.model_label),
    ).toBeInTheDocument();
    expect(
      screen.getByText(enAgents.pickers.model_default),
    ).toBeInTheDocument();
  });
});
