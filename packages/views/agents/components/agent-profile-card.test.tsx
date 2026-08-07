// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    agentDetail: (id: string) => `/test/agents/${id}`,
  }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getBaseUrl: () => "http://127.0.0.1:8080",
  },
}));

// AppLink is just a plain anchor here — the "Detail →" link target is not
// under test.
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

const mockAgents = vi.hoisted(() => ({ current: [] as unknown[] }));
const mockMembers = vi.hoisted(() => ({ current: [] as unknown[] }));
const mockRuntimes = vi.hoisted(() => ({ current: [] as unknown[] }));

// Distinguish the three list queries the card spreads into useQuery by their
// query-key shape:
//   ["workspaces", wsId, "agents"]   — agent list
//   ["workspaces", wsId, "members"]  — member list
//   ["runtimes",   wsId, "list"]     — runtime list
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
      if (key[0] === "workspaces" && key[2] === "members") {
        return { data: mockMembers.current, isLoading: false };
      }
      if (key[0] === "runtimes") {
        return { data: mockRuntimes.current, isLoading: false };
      }
      return { data: undefined, isLoading: false };
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
    name: "Lambda",
    description: "A coding agent.",
    instructions: "",
    avatar_url: null,
    // cloud so RuntimeRow reads "online" without a runtime fixture — the
    // Model row is what these tests exercise.
    runtime_mode: "cloud" as const,
    runtime_config: {},
    custom_args: [],
    visibility: "workspace" as const,
    permission_mode: "public_to" as const,
    invocation_targets: [],
    status: "idle" as const,
    max_concurrent_tasks: 1,
    model: "claude-opus-4-8",
    thinking_level: "high",
    owner_id: null,
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
  mockMembers.current = [];
  mockRuntimes.current = [];
  mockAgents.current = [makeAgent()];
});

describe("AgentProfileCard — Model row", () => {
  it("shows the pinned model id and the effort badge", () => {
    mockAgents.current = [makeAgent({ model: "claude-opus-4-8", thinking_level: "high" })];
    renderCard();

    expect(screen.getByText("claude-opus-4-8")).toBeInTheDocument();
    expect(screen.getByText("high")).toBeInTheDocument();
    expect(screen.queryByText(enAgents.profile_card.model_unset)).toBeNull();
  });

  // Regression (Emacs code review): an agent with no pinned model but a
  // persisted thinking_level runs WITH that effort at run time. The badge
  // must render even though the model cell reads "Runtime default" — gating
  // it on `hasModel` hid legitimate runtime config.
  it("shows the effort badge even when the model is unset (Runtime default)", () => {
    mockAgents.current = [makeAgent({ model: "", thinking_level: "high" })];
    renderCard();

    expect(screen.getByText(enAgents.profile_card.model_unset)).toBeInTheDocument();
    expect(screen.getByText("high")).toBeInTheDocument();
  });

  it("renders no effort badge when thinking_level is empty", () => {
    mockAgents.current = [makeAgent({ model: "", thinking_level: "" })];
    renderCard();

    expect(screen.getByText(enAgents.profile_card.model_unset)).toBeInTheDocument();
    // No effort token anywhere on the card.
    expect(screen.queryByText("high")).toBeNull();
    expect(screen.queryByText("medium")).toBeNull();
  });
});
