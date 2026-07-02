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

// Preserve the real paths module (sub-components import other helpers from it)
// and only stub the one path the card actually calls.
vi.mock("@multica/core/paths", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/paths")>(
      "@multica/core/paths",
    );
  return {
    ...actual,
    useWorkspacePaths: () => ({
      agentDetail: (id: string) => `/test/agents/${id}`,
    }),
  };
});

vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (url: string | null) => url,
}));

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
const mockRuntimes = vi.hoisted(() => ({ current: [] as unknown[] }));
const mockModels = vi.hoisted(() => ({ current: undefined as unknown }));
const mockPresence = vi.hoisted(() => ({ current: "loading" as unknown }));

// Distinguish the card's four queries by their key shape:
//   ["workspaces", wsId, "agents"]   — agent list
//   ["workspaces", wsId, "members"]  — member list
//   ["runtimes",   wsId, "list"]     — runtime list
//   ["runtimes",   "models", rtId]   — runtime model catalog (gated by enabled)
vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return {
    ...actual,
    useQuery: (opts: { queryKey: readonly unknown[]; enabled?: boolean }) => {
      const key = opts.queryKey;
      const root = key[0];
      const marker = key[2];
      if (root === "workspaces" && marker === "agents") {
        return { data: mockAgents.current, isLoading: false };
      }
      if (root === "workspaces" && marker === "members") {
        return { data: [], isLoading: false };
      }
      if (root === "runtimes" && key[1] === "models") {
        // Mirrors the real `enabled: Boolean(runtimeId)` gate: a disabled
        // catalog query (offline / no override) exposes no data.
        return { data: opts.enabled ? mockModels.current : undefined, isLoading: false };
      }
      if (root === "runtimes" && marker === "list") {
        return { data: mockRuntimes.current, isLoading: false };
      }
      return { data: undefined, isLoading: false };
    },
  };
});

vi.mock("@multica/core/agents", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/agents")>(
    "@multica/core/agents",
  );
  return {
    ...actual,
    useAgentPresenceDetail: () => mockPresence.current,
  };
});

import { AgentProfileCard } from "./agent-profile-card";

function makeAgent(overrides: Record<string, unknown> = {}) {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    runtime_id: "rt-1",
    name: "Walt",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local" as const,
    runtime_config: {},
    custom_args: [],
    visibility: "workspace" as const,
    status: "idle" as const,
    max_concurrent_tasks: 1,
    model: "claude-opus-4-8",
    thinking_level: "",
    owner_id: null,
    skills: [],
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function makeRuntime(overrides: Record<string, unknown> = {}) {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "Mac-Studio",
    runtime_mode: "local" as const,
    provider: "claude",
    launch_header: "",
    status: "online" as const,
    device_info: "",
    metadata: {},
    owner_id: null,
    visibility: "private" as const,
    last_seen_at: new Date().toISOString(),
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    ...overrides,
  };
}

// Catalog with the friendly label for the persisted token ("xhigh" → "Extra high").
const CATALOG = {
  models: [
    {
      id: "claude-opus-4-8",
      label: "Opus 4.8",
      default: true,
      thinking: {
        supported_levels: [
          { value: "high", label: "High" },
          { value: "xhigh", label: "Extra high" },
        ],
      },
    },
  ],
  supported: true,
};

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
  mockRuntimes.current = [makeRuntime()];
  mockModels.current = CATALOG;
  mockPresence.current = {
    availability: "online",
    workload: "idle",
    runningCount: 0,
    queuedCount: 0,
    capacity: 1,
  };
});

describe("AgentProfileCard model + thinking level", () => {
  it("shows the model id and no thinking chip when no override is set", () => {
    mockAgents.current = [makeAgent({ model: "claude-opus-4-8", thinking_level: "" })];

    renderCard();

    expect(screen.getByText(enAgents.profile_card.model_label)).toBeInTheDocument();
    expect(screen.getByText("claude-opus-4-8")).toBeInTheDocument();
    // An empty override renders no chip — neither a label nor a raw token.
    expect(screen.queryByText("Extra high")).toBeNull();
    expect(screen.queryByText("xhigh")).toBeNull();
  });

  it("resolves the thinking token to its catalog label when the runtime is online", () => {
    mockAgents.current = [makeAgent({ thinking_level: "xhigh" })];
    mockRuntimes.current = [makeRuntime({ status: "online" })];
    mockModels.current = CATALOG;

    renderCard();

    expect(screen.getByText("Extra high")).toBeInTheDocument();
  });

  it("falls back to the raw token when the catalog is unavailable (runtime offline)", () => {
    mockAgents.current = [makeAgent({ thinking_level: "xhigh" })];
    mockRuntimes.current = [makeRuntime({ status: "offline" })];

    renderCard();

    // Catalog query is disabled while offline → the raw token shows verbatim.
    expect(screen.getByText("xhigh")).toBeInTheDocument();
    expect(screen.queryByText("Extra high")).toBeNull();
  });

  it("shows the default label when the agent has no explicit model", () => {
    mockAgents.current = [makeAgent({ model: "", thinking_level: "" })];

    renderCard();

    expect(screen.getByText(enAgents.pickers.model_default)).toBeInTheDocument();
  });
});
