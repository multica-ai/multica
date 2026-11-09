import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { runtimeKeys } from "@multica/core/runtimes/queries";
import type { AgentRuntime } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { UsageLimitsBar } from "./usage-limits-bar";

const api = vi.hoisted(() => ({
  listRuntimes: vi.fn(),
  refreshRuntimeUsageLimits: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({ api }));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }) };
});

const NOW = new Date("2026-11-09T12:00:00Z");

const BASE_RUNTIME: AgentRuntime = {
  id: "",
  workspace_id: "ws-1",
  daemon_id: null,
  name: "",
  runtime_mode: "local",
  provider: "",
  launch_header: "",
  status: "online",
  device_info: "",
  metadata: {},
  owner_id: null,
  visibility: "private",
  shared_with: [],
  last_seen_at: null,
  created_at: "",
  updated_at: "",
};

function runtime(id: string, provider: string, usageLimits?: Record<string, unknown>, overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    ...BASE_RUNTIME,
    id,
    provider,
    metadata: usageLimits ? { usage_limits: usageLimits } : {},
    ...overrides,
  };
}

const claudeHome = runtime("rt-claude-home", "claude", {
  provider: "claude",
  account_id: "a",
  plan: "max",
  device: "pc-home",
  windows: [
    { id: "seven_day", window_minutes: 10080, used_percent: 41, resets_at: "2026-11-12T12:00:00Z" },
    { id: "five_hour", window_minutes: 300, used_percent: 86, resets_at: "2026-11-09T14:30:00Z" },
  ],
  updated_at: "2026-11-09T11:55:00Z",
  checked_at: "2026-11-09T11:55:00Z",
}, { daemon_id: "home" });

const claudeOld = runtime("rt-claude-old", "claude", {
  provider: "claude",
  account_id: "b",
  device: "pc-old",
  windows: [{ id: "seven_day", window_minutes: 10080, used_percent: 12, resets_at: "2026-11-13T00:00:00Z" }],
  updated_at: "2026-11-09T09:00:00Z",
  checked_at: "2026-11-09T11:58:00Z",
  error: "get_usage timed out",
}, { daemon_id: "old" });

function renderBar(runtimes: AgentRuntime[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(runtimeKeys.list("ws-1"), runtimes);
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <UsageLimitsBar />
    </QueryClientProvider>,
  );
}

describe("UsageLimitsBar", () => {
  beforeEach(() => {
    vi.useFakeTimers({ now: NOW, toFake: ["Date"] });
    api.refreshRuntimeUsageLimits.mockResolvedValue(undefined);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it("renders nothing until a daemon reports limits", () => {
    const { container } = renderBar([runtime("rt-gemini", "gemini")]);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows each window with its pace, reset time and forecast", () => {
    renderBar([claudeHome, claudeOld]);
    const bar = screen.getByRole("button", { name: "Usage limits" });
    expect(bar).toHaveTextContent("pc-home5h86%out in 25m2h 30m7d41%3d 0h");
    expect(bar).toHaveTextContent("pc-old7d12%3d 12h");
    expect(bar).not.toHaveTextContent("Weekly");
    expect(within(bar).getByText("86%")).toHaveClass("text-warning");
    expect(within(bar).getAllByRole("progressbar").map((meter) => meter.getAttribute("aria-valuenow"))).toEqual(["86", "41", "12"]);
  });

  it("does not forecast from stale numbers", () => {
    const staleHome = { ...claudeHome, metadata: { usage_limits: { ...claudeHome.metadata.usage_limits as object, error: "probe failed" } } };
    renderBar([staleHome]);
    expect(screen.getByRole("button", { name: "Usage limits" })).not.toHaveTextContent("out in");
  });

  it("lists every window with its reset time and the stale reason", async () => {
    renderBar([claudeHome, claudeOld]);
    fireEvent.click(screen.getByRole("button", { name: "Usage limits" }));

    const details = within(await screen.findByRole("dialog"));
    expect(details.getByText("Resets in 2h 30m")).toBeInTheDocument();
    expect(details.getByText("Runs out in 25m at this pace")).toBeInTheDocument();
    expect(details.getByText("Resets in 3d 0h")).toBeInTheDocument();
    expect(details.getAllByText("Weekly")).toHaveLength(2);
    expect(details.getByText("Last updated 3h ago · get_usage timed out")).toBeInTheDocument();
    expect(details.getAllByRole("progressbar").map((meter) => meter.getAttribute("aria-valuenow"))).toEqual(["86", "41", "12"]);
  });

  it("asks every machine with an online runtime to check now", async () => {
    renderBar([claudeHome, claudeOld, { ...claudeHome, id: "rt-claude-home-2" }]);
    fireEvent.click(screen.getByRole("button", { name: "Usage limits" }));
    fireEvent.click(await screen.findByRole("button", { name: "Check now" }));

    await waitFor(() => expect(api.refreshRuntimeUsageLimits).toHaveBeenCalledTimes(2));
    expect(api.refreshRuntimeUsageLimits.mock.calls.map(([id]) => id).sort()).toEqual(["rt-claude-home", "rt-claude-old"]);
  });
});
