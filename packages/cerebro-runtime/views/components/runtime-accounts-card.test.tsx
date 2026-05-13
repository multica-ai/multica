import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { AgentRuntime } from "@multica/core/types/agent";
import type { CerebroAccount } from "@multica/core/api";

const mockUseCerebroAccountsList = vi.hoisted(() => vi.fn());

vi.mock("./use-cerebro-accounts", () => ({
  useCerebroAccountsList: mockUseCerebroAccountsList,
  useCerebroAccount: (id: string | null | undefined) => {
    const { data, isLoading } = mockUseCerebroAccountsList();
    if (isLoading) return { account: null, isLoading: true };
    if (!id || !data) return { account: null, isLoading: false };
    return {
      account:
        (data as CerebroAccount[]).find((a) => a.id === id) ?? null,
      isLoading: false,
    };
  },
}));

import { RuntimeAccountsCard } from "./runtime-accounts-card";

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
  last_seen_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const account: CerebroAccount = {
  id: "acc-1",
  workspace_id: "ws-1",
  provider: "claude",
  login_identity: "user@example.com",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  runtime_count: 1,
  available_runtime_count: 1,
  nearest_unpause_at: null,
  status: "available",
};

beforeEach(() => {
  mockUseCerebroAccountsList.mockReset();
});

describe("RuntimeAccountsCard", () => {
  it("renders only the runtime's own account when current_account_id resolves", () => {
    const otherAccount: CerebroAccount = {
      ...account,
      id: "acc-2",
      login_identity: "someone-else@example.com",
    };
    mockUseCerebroAccountsList.mockReturnValue({
      data: [account, otherAccount],
      isLoading: false,
    });

    render(
      <RuntimeAccountsCard
        runtime={{ ...baseRuntime, current_account_id: account.id }}
      />,
    );

    expect(screen.getByText("user@example.com")).toBeInTheDocument();
    expect(screen.getByText("claude")).toBeInTheDocument();
    expect(
      screen.queryByText("someone-else@example.com"),
    ).not.toBeInTheDocument();
  });

  it("shows the unknown-account empty state when current_account_id is null", () => {
    mockUseCerebroAccountsList.mockReturnValue({
      data: [account],
      isLoading: false,
    });

    render(
      <RuntimeAccountsCard
        runtime={{ ...baseRuntime, current_account_id: null }}
      />,
    );

    expect(
      screen.getByText(/Konto ukendt — daemon har ikke rapporteret endnu/),
    ).toBeInTheDocument();
    expect(screen.queryByText("user@example.com")).not.toBeInTheDocument();
  });

  it("falls back to unknown state when current_account_id is omitted (older API contract)", () => {
    mockUseCerebroAccountsList.mockReturnValue({
      data: [account],
      isLoading: false,
    });

    render(<RuntimeAccountsCard runtime={baseRuntime} />);

    expect(
      screen.getByText(/Konto ukendt — daemon har ikke rapporteret endnu/),
    ).toBeInTheDocument();
  });

  it("falls back to unknown state when current_account_id points at a missing row", () => {
    mockUseCerebroAccountsList.mockReturnValue({
      data: [account],
      isLoading: false,
    });

    render(
      <RuntimeAccountsCard
        runtime={{ ...baseRuntime, current_account_id: "acc-gone" }}
      />,
    );

    expect(
      screen.getByText(/Konto ukendt — daemon har ikke rapporteret endnu/),
    ).toBeInTheDocument();
  });

  it("renders the loading state until the list resolves", () => {
    mockUseCerebroAccountsList.mockReturnValue({
      data: undefined,
      isLoading: true,
    });

    render(
      <RuntimeAccountsCard
        runtime={{ ...baseRuntime, current_account_id: account.id }}
      />,
    );

    expect(screen.getByText("Indlæser…")).toBeInTheDocument();
  });
});
