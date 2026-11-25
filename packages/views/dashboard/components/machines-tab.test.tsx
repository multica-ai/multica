import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { SupportedLocale } from "@multica/core/i18n";
import type { AgentRuntime, DashboardUsageByRuntime } from "@multica/core/types";
import { api } from "@multica/core/api";
import { renderWithI18n } from "../../test/i18n";
import { MachinesTab } from "./machines-tab";

import en from "../../locales/en/usage.json";
import es from "../../locales/es/usage.json";
import fr from "../../locales/fr/usage.json";
import ja from "../../locales/ja/usage.json";
import ko from "../../locales/ko/usage.json";
import ru from "../../locales/ru/usage.json";
import zhHans from "../../locales/zh-Hans/usage.json";
import zhHant from "../../locales/zh-Hant/usage.json";

// The "By machine" tab. The server decides which runtimes may be named; these
// tests pin how the client renders what it is given: runtimes grouped into
// machines and named as on the Runtimes page, the owner line, the restricted
// and deleted buckets, and the error state when the runtime list fails.

type MachinesCopy = {
  machines: Record<"title" | "header_machine" | "other_machines" | "deleted_machines", string>;
};

const COPY: Record<SupportedLocale, MachinesCopy> = {
  en,
  es,
  fr,
  ja,
  ko,
  ru,
  "zh-Hans": zhHans,
  "zh-Hant": zhHant,
};

vi.mock("@multica/core/api", () => ({
  api: {
    getDashboardUsageByRuntime: vi.fn(),
    listRuntimes: vi.fn(),
    listMembers: vi.fn(),
  },
}));

vi.mock("@multica/core/auth", () => {
  const state = () => ({ user: { id: "user-me" } });
  const useAuthStore = Object.assign(
    (sel?: (s: ReturnType<typeof state>) => unknown) => (sel ? sel(state()) : state()),
    { getState: state },
  );
  return { useAuthStore };
});

const DOLLAR = 10_000_000_000;

function runtime(overrides: Partial<AgentRuntime>): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Claude (build-box)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: "user-alice",
    visibility: "public",
    last_seen_at: null,
    created_at: "2026-05-17T11:00:00Z",
    updated_at: "2026-05-17T11:00:00Z",
    ...overrides,
  };
}

function usage(runtimeId: string, tokens: number, taskCount: number, dollars: number): DashboardUsageByRuntime {
  return {
    runtime_id: runtimeId,
    task_count: taskCount,
    usage: [
      {
        provider: "claude",
        model: "unpriced-test-model",
        input_tokens: tokens,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        cost_usd_ticks: dollars * DOLLAR,
        uncosted_input_tokens: 0,
        uncosted_output_tokens: 0,
        uncosted_cache_read_tokens: 0,
        uncosted_cache_write_tokens: 0,
      },
    ],
  };
}

let client: QueryClient;

function renderTab(locale: SupportedLocale = "en") {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <MachinesTab
        wsId="ws-1"
        days={7}
        projectId="project-1"
        viewTZ="Asia/Shanghai"
        historyLimit={null}
        squadId="squad-1"
      />
    </QueryClientProvider>,
    { locale },
  );
}

beforeEach(() => {
  vi.mocked(api.listRuntimes).mockResolvedValue([
    runtime({ id: "rt-claude", provider: "claude", name: "Claude (build-box)", custom_name: "Team box" }),
    runtime({ id: "rt-codex", provider: "codex", name: "Codex (build-box)", custom_name: "Team box" }),
    runtime({ id: "rt-mine", daemon_id: "daemon-2", name: "Claude (my-laptop)", owner_id: "user-me" }),
  ]);
  vi.mocked(api.listMembers).mockResolvedValue([
    { user_id: "user-alice", name: "Alice" },
  ] as Awaited<ReturnType<typeof api.listMembers>>);
  vi.mocked(api.getDashboardUsageByRuntime).mockResolvedValue([
    usage("rt-claude", 100, 2, 1),
    usage("rt-codex", 300, 1, 3),
    usage("rt-mine", 50, 4, 2),
    usage("rt-gone", 5, 1, 0),
    usage("__restricted_runtimes__", 7, 3, 0),
  ]);
});

afterEach(() => {
  cleanup();
  client?.clear();
  vi.clearAllMocks();
});

describe("MachinesTab", () => {
  it("groups runtimes into machines and expands one into its runtimes", async () => {
    const user = userEvent.setup();
    renderTab();

    const teamBox = await screen.findByRole("button", { name: "Show runtimes on Team box" });
    // Two runtimes, one machine: tokens, cost and runs are summed.
    expect(within(teamBox).getByText("400")).toBeInTheDocument();
    expect(within(teamBox).getByText("$4.00")).toBeInTheDocument();
    expect(within(teamBox).getByText("3")).toBeInTheDocument();
    // Owner named as on the Runtimes page, "You" for the viewer's own machine.
    expect(within(teamBox).getByText("Alice")).toBeInTheDocument();
    const mine = screen.getByRole("button", { name: "Show runtimes on my-laptop" });
    expect(within(mine).getByText("You")).toBeInTheDocument();
    expect(screen.getByText("2 machines")).toBeInTheDocument();

    // Buckets come last and cannot be expanded.
    const items = within(screen.getByRole("list", { name: "Usage by machine" })).getAllByRole("listitem");
    expect(items.at(-2)).toHaveTextContent("Deleted machines");
    expect(items.at(-1)).toHaveTextContent("Other machines");
    expect(within(items.at(-1)!).queryByRole("button")).toBeNull();

    await user.click(teamBox);
    const expanded = screen.getByRole("button", { name: "Hide runtimes on Team box" });
    expect(expanded).toHaveAttribute("aria-expanded", "true");
    const runtimes = within(screen.getByRole("list", { name: "Runtimes on Team box" })).getAllByRole("listitem");
    expect(runtimes.map((li) => li.textContent)).toEqual([
      expect.stringContaining("300"),
      expect.stringContaining("100"),
    ]);
    expect(runtimes[0]).toHaveTextContent("Codex");

    await user.click(expanded);
    expect(screen.queryByRole("list", { name: "Runtimes on Team box" })).toBeNull();

    expect(api.getDashboardUsageByRuntime).toHaveBeenCalledWith(
      expect.objectContaining({ days: 7, project_id: "project-1", squad_id: "squad-1", tz: "Asia/Shanghai" }),
    );
  });

  it.each(Object.keys(COPY) as SupportedLocale[])("renders its copy in %s", async (locale) => {
    renderTab(locale);
    expect(await screen.findByText(COPY[locale].machines.title)).toBeInTheDocument();
    expect(screen.getByText(COPY[locale].machines.header_machine)).toBeInTheDocument();
    expect(screen.getByText(COPY[locale].machines.other_machines)).toBeInTheDocument();
    expect(screen.getByText(COPY[locale].machines.deleted_machines)).toBeInTheDocument();
  });

  it("shows an error rather than calling every machine deleted when the runtime list fails", async () => {
    vi.mocked(api.listRuntimes).mockRejectedValue(new Error("offline"));
    renderTab();
    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't load machine usage.");
    expect(screen.queryByText("Deleted machines")).toBeNull();
  });

  it("shows the empty state for a window with no usage", async () => {
    vi.mocked(api.getDashboardUsageByRuntime).mockResolvedValue([]);
    renderTab();
    expect(await screen.findByText("No machine usage in this window.")).toBeInTheDocument();
  });
});
