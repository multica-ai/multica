import "@multica/cerebro-types";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import type { CerebroAccount } from "@multica/core/api";
import type { Agent, AgentRuntime } from "@multica/core/types";

const mocks = vi.hoisted(() => ({
  account: null as CerebroAccount | null,
  enabled: true,
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => mocks.enabled,
}));

vi.mock("./use-cerebro-accounts", () => ({
  useCerebroAccount: () => ({ account: mocks.account, isLoading: false }),
}));

import {
  AgentProfileAccount,
  AgentProfileSettings,
} from "./agent-profile-tooltip-details";

const agent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Sofie",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "private",
  status: "idle",
  max_concurrent_tasks: 3,
  model: "claude-opus-5",
  thinking_level: "high",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

const runtime: AgentRuntime = {
  id: "runtime-1",
  workspace_id: "ws-1",
  daemon_id: "daemon-1",
  name: "sofie.local",
  runtime_mode: "local",
  provider: "claude",
  launch_header: "",
  status: "online",
  device_info: "sofie.local",
  metadata: {},
  owner_id: null,
  sandbox_enabled: null,
  visibility: "private",
  timezone: "UTC",
  capabilities: {},
  last_seen_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  current_account_id: "account-1",
};

const account: CerebroAccount = {
  id: "account-1",
  workspace_id: "ws-1",
  provider: "claude",
  login_identity: "sofie@firtal.com",
  usage_window_pct: null,
  throttled_until: null,
  usage_5h_pct: 27,
  usage_5h_resets_at: "2099-01-01T02:15:00Z",
  usage_7d_pct: 59,
  usage_7d_resets_at: "2099-01-04T04:00:00Z",
  tokens_5h: 12_400,
  tokens_7d: 1_800_000,
  extra_spend_on: false,
  paused_manual: false,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  runtime_count: 1,
  available_runtime_count: 1,
  nearest_unpause_at: null,
  status: "available",
};

beforeEach(() => {
  mocks.enabled = true;
  mocks.account = account;
});

describe("agent profile tooltip details", () => {
  it("shows model, thinking level, and concurrency", () => {
    render(<AgentProfileSettings agent={agent} />);

    expect(screen.getByText("claude-opus-5")).toBeInTheDocument();
    expect(screen.getByText("high")).toBeInTheDocument();
    expect(screen.getByText("3 tasks")).toBeInTheDocument();
  });

  it("shows honest defaults when model and thinking are inherited", () => {
    render(
      <AgentProfileSettings
        agent={{ ...agent, model: "", thinking_level: "", max_concurrent_tasks: 1 }}
      />,
    );

    expect(screen.getByText("Default")).toBeInTheDocument();
    expect(screen.getByText("Follow CLI config")).toBeInTheDocument();
    expect(screen.getByText("1 task")).toBeInTheDocument();
  });

  it("shows account identity, status, remaining usage, tokens, and reset times", () => {
    render(<AgentProfileAccount runtime={runtime} />);

    const details = screen.getByLabelText("Agent account status and usage");
    expect(within(details).getByText("sofie@firtal.com")).toBeInTheDocument();
    expect(within(details).getByText("claude")).toBeInTheDocument();
    expect(within(details).getByText("Available")).toBeInTheDocument();
    expect(within(details).getByText("73% left")).toBeInTheDocument();
    expect(within(details).getByText("41% left")).toBeInTheDocument();
    expect(within(details).getByText("12.4k tok used")).toBeInTheDocument();
    expect(within(details).getByText("1.8M tok used")).toBeInTheDocument();
    expect(within(details).getAllByText(/^resets in /)).toHaveLength(2);
  });

  it("shows weekly-only data without inventing a five-hour value and surfaces throttling", () => {
    mocks.account = {
      ...account,
      status: "throttled",
      usage_window_pct: 73,
      usage_5h_pct: null,
      usage_7d_pct: 73,
    };

    render(<AgentProfileAccount runtime={runtime} />);

    expect(screen.getByText("Throttled")).toBeInTheDocument();
    expect(screen.getByText("No data yet")).toBeInTheDocument();
    expect(screen.getByText("27% left")).toBeInTheDocument();
  });

  it("hides account data when no account resolves and restores model-only details when disabled", () => {
    mocks.account = null;
    const { rerender } = render(<AgentProfileAccount runtime={runtime} />);
    expect(screen.queryByLabelText("Agent account status and usage")).not.toBeInTheDocument();

    mocks.enabled = false;
    rerender(<AgentProfileSettings agent={agent} />);
    expect(screen.getByText("claude-opus-5")).toBeInTheDocument();
    expect(screen.queryByText("high")).not.toBeInTheDocument();
    expect(screen.queryByText("3 tasks")).not.toBeInTheDocument();
  });
});
