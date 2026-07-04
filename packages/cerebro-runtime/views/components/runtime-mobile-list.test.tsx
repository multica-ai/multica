import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { AgentRuntime } from "@multica/core/types";

// The mobile card renders real UI cells (health, account, cost) that each pull
// their own context. Mock the leaf dependencies so the test isolates the ONE
// behaviour under test: enabled columns render as card rows, disabled ones do
// not. This is exactly the mobile regression Jesper reported (FIR-2669 review):
// on a phone the picker toggles produced no visible columns.

// i18n: return the leaf translation key so labels are assertable ("col_machine").
vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (sel: (proxy: unknown) => string) =>
      sel(
        new Proxy(
          {},
          { get: () => new Proxy({}, { get: (_t, k) => String(k) }) },
        ),
      ),
  }),
}));

// Feature flag on — the card only renders when the columns feature is enabled.
const mockUseFlagValue = vi.hoisted(() => vi.fn(() => true));
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFlagValue: mockUseFlagValue,
}));

vi.mock("@multica/views/navigation", () => ({
  AppLink: ({
    children,
    ...rest
  }: {
    children: React.ReactNode;
    href: string;
  }) => <a {...rest}>{children}</a>,
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ runtimeDetail: (id: string) => `/r/${id}` }),
}));

vi.mock("@multica/views/runtimes/components/provider-logo", () => ({
  ProviderLogo: () => null,
}));

vi.mock("@multica/views/runtimes/components/shared", () => ({
  HealthIcon: () => null,
  useHealthLabel: () => (h: string) => h,
}));

vi.mock("@multica/core/runtimes", () => ({
  deriveRuntimeHealth: () => "online",
  runtimeUsageOptions: () => ({ queryKey: ["usage"], queryFn: () => [] }),
}));

vi.mock("@tanstack/react-query", () => ({
  // CostValue calls useQuery; no usage rows -> the cost cell renders an em-dash.
  useQuery: () => ({ data: [] }),
}));

const mockUseCerebroAccount = vi.hoisted(() => vi.fn());
vi.mock("./use-cerebro-accounts", () => ({
  useCerebroAccount: mockUseCerebroAccount,
}));

import { RuntimeMobileList } from "./runtime-mobile-list";
import {
  useRuntimesViewStore,
  RUNTIME_DEFAULT_HIDDEN_COLUMNS,
  type RuntimeColumnKey,
} from "../runtime-view-store";

const runtime: AgentRuntime = {
  id: "runtime-1",
  workspace_id: "ws-1",
  daemon_id: "daemon-1",
  name: "claude-code (Jespers-MacBook)",
  runtime_mode: "local",
  provider: "claude",
  launch_header: "",
  status: "online",
  device_info: "host.local",
  metadata: { version: "2.1.5" },
  owner_id: null,
  sandbox_enabled: null,
  persona_sandbox: "",
  visibility: "private",
  timezone: "UTC",
  capabilities: {},
  last_seen_at: null,
  current_account_id: "acc-1",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} as AgentRuntime;

function setHidden(hidden: RuntimeColumnKey[]) {
  useRuntimesViewStore.setState({ hiddenColumns: hidden });
}

beforeEach(() => {
  mockUseFlagValue.mockReturnValue(true);
  mockUseCerebroAccount.mockReturnValue({
    account: { login_identity: "user@example.com", provider: "claude" },
    isLoading: false,
  });
  setHidden([...RUNTIME_DEFAULT_HIDDEN_COLUMNS]);
});

describe("RuntimeMobileList", () => {
  it("shows an enabled column as a card row (Machine reveals the computer name)", () => {
    // Enable the opt-in Machine column (default hidden).
    setHidden([]);
    render(<RuntimeMobileList rows={[{ runtime, agentCount: 3 }]} now={0} />);

    // The computer name (hostname) is now visible on mobile.
    expect(screen.getByText("Jespers-MacBook")).toBeInTheDocument();
    // The runtime's base name still heads the card.
    expect(screen.getByText("claude-code")).toBeInTheDocument();
    // Account + CLI columns (default on) render their values too.
    expect(screen.getByText("user@example.com")).toBeInTheDocument();
    expect(screen.getByText("2.1.5")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  it("hides a disabled column (Machine off -> no computer name on the card)", () => {
    setHidden(["machine", "cli"]);
    render(<RuntimeMobileList rows={[{ runtime, agentCount: 1 }]} now={0} />);

    expect(screen.queryByText("Jespers-MacBook")).not.toBeInTheDocument();
    expect(screen.queryByText("2.1.5")).not.toBeInTheDocument();
    // A still-enabled column (Account) remains visible.
    expect(screen.getByText("user@example.com")).toBeInTheDocument();
  });
});
