import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { cleanup, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";
import type { SupportedLocale } from "@multica/core/i18n";

// The "By member" tab. The server decides what each viewer may see; these
// tests pin how the client renders what it is given: names resolved from the
// member list, the unattributed / former-member / restricted-agent buckets,
// chat runs without a title, and the per-member query key.

const roleRef = vi.hoisted(() => ({ current: "owner" as string }));
const queryKeys = vi.hoisted(() => [] as unknown[][]);

const BY_MEMBER = vi.hoisted(() => [
  // Alice: two models, folded into one row.
  {
    user_id: "user-alice",
    task_count: 3,
    usage: [
      { provider: "anthropic", model: "claude-sonnet-4-6", input_tokens: 1_000, output_tokens: 1_000, cache_read_tokens: 0, cache_write_tokens: 0 },
      { provider: "anthropic", model: "claude-haiku-4-5", input_tokens: 500, output_tokens: 500, cache_read_tokens: 0, cache_write_tokens: 0 },
    ],
  },
  {
    user_id: "user-bob",
    task_count: 7,
    usage: [
      { provider: "anthropic", model: "claude-sonnet-4-6", input_tokens: 9_000, output_tokens: 1_000, cache_read_tokens: 0, cache_write_tokens: 0 },
    ],
  },
  // Someone who has since left the workspace.
  {
    user_id: "user-gone",
    task_count: 1,
    usage: [
      { provider: "anthropic", model: "claude-sonnet-4-6", input_tokens: 100, output_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0 },
    ],
  },
  {
    user_id: "__unattributed__",
    task_count: 2,
    usage: [
      { provider: "anthropic", model: "claude-sonnet-4-6", input_tokens: 50_000, output_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0 },
    ],
  },
]);

const ALICE_TASKS = vi.hoisted(() => [
  {
    task_id: "task-issue",
    agent_id: "agent-1",
    status: "completed",
    kind: "issue",
    issue_id: "issue-uuid-1",
    issue_identifier: "ACM-42",
    issue_title: "Fix the login page",
    chat_session_id: null,
    chat_title: null,
    last_used_at: new Date().toISOString(),
    usage: [
      { provider: "anthropic", model: "claude-sonnet-4-6", input_tokens: 1_000, output_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0 },
    ],
  },
  // Someone else's chat as seen by an admin: no title, no session id.
  {
    task_id: "task-chat",
    agent_id: "__restricted_agents__",
    status: "completed",
    kind: "chat",
    issue_id: null,
    issue_identifier: null,
    issue_title: null,
    chat_session_id: null,
    chat_title: null,
    last_used_at: new Date().toISOString(),
    usage: [],
  },
  // A run about a private project the viewer cannot see: no issue fields.
  {
    task_id: "task-hidden",
    agent_id: "agent-1",
    status: "completed",
    kind: "other",
    issue_id: null,
    issue_identifier: null,
    issue_title: null,
    chat_session_id: null,
    chat_title: null,
    last_used_at: new Date().toISOString(),
    usage: [],
  },
]);

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>("@tanstack/react-query");
  return {
    ...actual,
    useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
      queryKeys.push(opts.queryKey);
      const [a, , c] = opts.queryKey;
      if (a === "workspaces" && c === "members") {
        return {
          data: [
            { user_id: "user-alice", name: "Alice", role: roleRef.current },
            { user_id: "user-bob", name: "Bob", role: "member" },
          ],
          isPending: false,
        };
      }
      if (a === "workspaces") return { data: [], isPending: false };
      if (c === "by-member") return { data: BY_MEMBER, isPending: false };
      if (c === "member-tasks") return { data: ALICE_TASKS, isPending: false, isError: false };
      return { data: undefined, isPending: true };
    },
  };
});

// ActorAvatar resolves names through the current workspace.
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "https://example.test" },
}));
vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>("@multica/core/paths");
  return {
    ...actual,
    useWorkspacePaths: () => ({
      issueDetail: (ref: string) => `/acme/issues/${ref}`,
      chatSession: (id: string) => `/acme/chat/${id}`,
      agentDetail: (id: string) => `/acme/agents/${id}`,
      memberDetail: (id: string) => `/acme/members/${id}`,
    }),
  };
});
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({
    userId: "user-alice",
    role: roleRef.current,
    member: null,
    isLoading: false,
  }),
}));
vi.mock("@multica/core/runtimes/custom-pricing-store", () => {
  const state = () => ({ pricings: {} });
  const useCustomPricingStore = Object.assign(
    (sel?: (s: ReturnType<typeof state>) => unknown) => (sel ? sel(state()) : state()),
    { getState: state },
  );
  return { useCustomPricingStore };
});

import { MembersTab } from "./members-tab";

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/usage",
  searchParams: new URLSearchParams("tab=members"),
  hash: "",
  getShareableUrl: (path: string) => `https://example.test${path}`,
};

function renderTab(locale?: SupportedLocale) {
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      <MembersTab
        wsId="ws-1"
        days={30}
        projectId="project-1"
        viewTZ="UTC"
        historyLimit={null}
        squadId="squad-1"
        agents={[{ id: "agent-1", name: "Agent One" } as never]}
      />
    </NavigationProvider>,
    locale ? { locale } : undefined,
  );
}

function memberRows(): HTMLElement[] {
  return within(screen.getByRole("list", { name: "Usage by member" })).getAllByRole("button");
}

describe("MembersTab", () => {
  beforeEach(() => {
    roleRef.current = "owner";
    queryKeys.length = 0;
  });
  afterEach(() => cleanup());

  it("folds models per member, ranks by cost and keeps unattributed last", () => {
    renderTab();
    const rows = memberRows();
    expect(rows.map((r) => r.textContent)).toEqual([
      expect.stringContaining("Bob"),
      expect.stringContaining("Alice"),
      expect.stringContaining("Former member"),
      expect.stringContaining("Unattributed"),
    ]);
    // Alice: 2,000 + 1,000 tokens across two models, 3 runs (the server's
    // distinct count, not the number of usage rows).
    const alice = rows[1] as HTMLElement;
    expect(alice.textContent).toContain("3K");
    expect(alice.textContent).toMatch(/3$/);
    // "N members" counts people, not the unattributed bucket.
    expect(screen.getByText("3 members")).toBeInTheDocument();
    expect(screen.getByText("Runs with usage")).toBeInTheDocument();
    expect(screen.queryByText("You can only see your own usage.")).not.toBeInTheDocument();
  });

  it("queries under the page's range, project, squad, timezone and history limit", () => {
    renderTab();
    const byMember = queryKeys.find((k) => k[2] === "by-member");
    expect(byMember).toEqual(["dashboard", "ws-1", "by-member", 30, "project-1", "UTC", null, "squad-1"]);
  });

  it("tells a plain member they only see their own row", () => {
    roleRef.current = "member";
    renderTab();
    expect(screen.getByText("You can only see your own usage.")).toBeInTheDocument();
  });

  it("expands a member into their recent runs, keyed by that member", async () => {
    const user = userEvent.setup();
    renderTab();
    expect(queryKeys.some((k) => k[2] === "member-tasks")).toBe(false);
    const alice = screen.getByRole("button", { name: "Show recent runs for Alice" });
    expect(alice).toHaveAttribute("aria-expanded", "false");
    await user.click(alice);
    expect(
      screen.getByRole("button", { name: "Hide recent runs for Alice" }),
    ).toHaveAttribute("aria-expanded", "true");

    const tasksKey = queryKeys.find((k) => k[2] === "member-tasks");
    expect(tasksKey?.slice(-2)).toEqual(["user", "user-alice"]);
    expect(tasksKey?.slice(3, 8)).toEqual([30, "project-1", "UTC", null, "squad-1"]);

    const runs = within(screen.getByRole("list", { name: "Recent runs" })).getAllByRole("listitem");
    expect(runs).toHaveLength(3);

    const issueLink = within(runs[0] as HTMLElement).getByRole("link", { name: /ACM-42/ });
    expect(issueLink).toHaveAttribute("href", "/acme/issues/ACM-42");
    expect(issueLink.textContent).toContain("Fix the login page");
    expect(runs[0]?.textContent).toContain("Agent One");

    // Another member's chat: plain "Chat", no link; hidden agent shown as a bucket.
    const chat = runs[1] as HTMLElement;
    expect(within(chat).queryByRole("link")).toBeNull();
    expect(chat.textContent).toContain("Chat");
    expect(chat.textContent).toContain("Other agents");
    expect(chat.textContent).not.toContain("__restricted_agents__");

    // A run whose issue the server withheld names nothing.
    const hidden = runs[2] as HTMLElement;
    const hrefs = within(hidden).queryAllByRole("link").map((l) => l.getAttribute("href"));
    expect(hrefs.some((h) => h?.includes("/issues/"))).toBe(false);
    expect(hidden.textContent).toContain("Other run");
  });

  it("renders in Simplified Chinese", () => {
    renderTab("zh-Hans");
    expect(screen.getByText("有用量的运行")).toBeInTheDocument();
    expect(screen.getByText("未归属")).toBeInTheDocument();
    expect(screen.getByText("已离开的成员")).toBeInTheDocument();
  });
});
