import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { AgentRuntime } from "@multica/core/types/agent";
import type { CerebroAccount } from "@multica/core/api";

const mockUseCerebroAccount = vi.hoisted(() => vi.fn());

vi.mock("./use-cerebro-accounts", () => ({
  useCerebroAccount: mockUseCerebroAccount,
}));

import { RuntimeAccountCell } from "./runtime-account-cell";

const baseRuntime: AgentRuntime = {
  id: "runtime-1",
  workspace_id: "ws-1",
  daemon_id: "daemon-1",
  name: "test-runtime",
  runtime_mode: "local",
  provider: "claude",
  launch_header: "",
  status: "online",
  device_info: "host.local",
  metadata: {},
  owner_id: null,
  sandbox_enabled: null,
  persona_sandbox: "",
  visibility: "private",
  timezone: "UTC",
  capabilities: {},
  last_seen_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const account: CerebroAccount = {
  id: "acc-1",
  workspace_id: "ws-1",
  provider: "claude",
  login_identity: "user@example.com",
  usage_window_pct: null,
  throttled_until: null,
  tokens_5h: 0,
  tokens_7d: 0,
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
  mockUseCerebroAccount.mockReset();
});

describe("RuntimeAccountCell", () => {
  it("renders em-dash for cloud runtimes and skips the account lookup", () => {
    mockUseCerebroAccount.mockReturnValue({ account: null, isLoading: false });

    const { container } = render(
      <RuntimeAccountCell
        runtime={{
          ...baseRuntime,
          runtime_mode: "cloud",
          current_account_id: account.id,
        }}
      />,
    );

    expect(container.textContent).toBe("—");
    expect(screen.queryByText("Konto ukendt")).not.toBeInTheDocument();
    // Cloud short-circuit must pass null to skip the workspace-wide account
    // fetch — otherwise we'd waste a request per cloud runtime row.
    expect(mockUseCerebroAccount).toHaveBeenCalledWith(null);
  });

  it("renders em-dash while the account list is loading", () => {
    mockUseCerebroAccount.mockReturnValue({ account: null, isLoading: true });

    const { container } = render(
      <RuntimeAccountCell
        runtime={{ ...baseRuntime, current_account_id: account.id }}
      />,
    );

    expect(container.textContent).toBe("—");
  });

  it("renders the unknown-account state when the account cannot be resolved", () => {
    mockUseCerebroAccount.mockReturnValue({ account: null, isLoading: false });

    render(
      <RuntimeAccountCell
        runtime={{ ...baseRuntime, current_account_id: null }}
      />,
    );

    expect(screen.getByText("Konto ukendt")).toBeInTheDocument();
  });

  it("renders the login identity and provider when the account resolves", () => {
    mockUseCerebroAccount.mockReturnValue({ account, isLoading: false });

    render(
      <RuntimeAccountCell
        runtime={{ ...baseRuntime, current_account_id: account.id }}
      />,
    );

    expect(screen.getByText("user@example.com")).toBeInTheDocument();
    expect(screen.getByText("claude")).toBeInTheDocument();
  });
});
