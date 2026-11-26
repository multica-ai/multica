import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { cleanup, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";
import type { SupportedLocale } from "@multica/core/i18n";

// The "By project" tab. The server decides what each viewer may see; these
// tests pin how the client renders what it is given: titles from the server,
// the no-project and restricted buckets, and the per-project query key.

const roleRef = vi.hoisted(() => ({ current: "owner" as string }));
const queryKeys = vi.hoisted(() => [] as unknown[][]);

const usage = (input: number) => [
  { provider: "anthropic", model: "claude-sonnet-4-6", input_tokens: input, output_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0 },
];

const BY_PROJECT = vi.hoisted(() => [
  { project_id: "__restricted_projects__", project_title: "", task_count: 5, usage: [] as unknown[] },
  { project_id: "__no_project__", project_title: "", task_count: 4, usage: [] as unknown[] },
  { project_id: "project-small", project_title: "Small", task_count: 1, usage: [] as unknown[] },
  { project_id: "project-apollo", project_title: "Apollo", task_count: 3, usage: [] as unknown[] },
]);

const PROJECT_TASKS = vi.hoisted(() => [
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
    usage: [] as unknown[],
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
      if (a === "workspaces") return { data: [], isPending: false };
      if (c === "by-project") return { data: BY_PROJECT, isPending: false };
      if (c === "project-tasks") return { data: PROJECT_TASKS, isPending: false, isError: false };
      return { data: undefined, isPending: true };
    },
  };
});

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

import { ProjectsTab } from "./projects-tab";

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/usage",
  searchParams: new URLSearchParams("tab=projects"),
  hash: "",
  getShareableUrl: (path: string) => `https://example.test${path}`,
};

function renderTab(locale?: SupportedLocale) {
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      <ProjectsTab
        wsId="ws-1"
        days={30}
        projectId={null}
        viewTZ="UTC"
        historyLimit={null}
        squadId="squad-1"
        agents={[{ id: "agent-1", name: "Agent One" } as never]}
        projects={[{ id: "project-apollo", icon: "🚀" }]}
      />
    </NavigationProvider>,
    locale ? { locale } : undefined,
  );
}

function projectRows(): HTMLElement[] {
  return within(screen.getByRole("list", { name: "Usage by project" })).getAllByRole("listitem");
}

describe("ProjectsTab", () => {
  beforeEach(() => {
    roleRef.current = "owner";
    queryKeys.length = 0;
    // Biggest spend first; the buckets' own spend must not lift them above it.
    BY_PROJECT[0]!.usage = usage(90_000);
    BY_PROJECT[1]!.usage = usage(80_000);
    BY_PROJECT[2]!.usage = usage(1_000);
    BY_PROJECT[3]!.usage = usage(5_000);
  });
  afterEach(() => cleanup());

  it("ranks projects by cost and keeps No project, then restricted, last", () => {
    renderTab();
    expect(projectRows().map((r) => r.textContent)).toEqual([
      expect.stringContaining("Apollo"),
      expect.stringContaining("Small"),
      expect.stringContaining("No project"),
      expect.stringContaining("Projects you can't see"),
    ]);
    const apollo = projectRows()[0] as HTMLElement;
    expect(apollo.textContent).toContain("🚀");
    expect(apollo.textContent).toContain("5K");
    expect(apollo.textContent).toMatch(/3$/);
    // "N projects" counts real projects, not the buckets.
    expect(screen.getByText("2 projects")).toBeInTheDocument();
    expect(screen.queryByText("You can only see your own usage.")).not.toBeInTheDocument();
  });

  it("queries under the page's range, project, squad, timezone and history limit", () => {
    renderTab();
    expect(queryKeys.find((k) => k[2] === "by-project")).toEqual([
      "dashboard", "ws-1", "by-project", 30, null, "UTC", null, "squad-1",
    ]);
  });

  it("tells a plain member they only see their own runs", () => {
    roleRef.current = "member";
    renderTab();
    expect(screen.getByText("You can only see your own usage.")).toBeInTheDocument();
  });

  it("never offers to expand the restricted bucket", () => {
    renderTab();
    const restricted = projectRows()[3] as HTMLElement;
    expect(within(restricted).queryByRole("button")).toBeNull();
    expect(restricted.textContent).not.toContain("__restricted_projects__");
  });

  it("expands a project, or No project, into its recent runs keyed by that project", async () => {
    const user = userEvent.setup();
    renderTab();
    expect(queryKeys.some((k) => k[2] === "project-tasks")).toBe(false);

    await user.click(screen.getByRole("button", { name: "Show recent runs for Apollo" }));
    expect(
      screen.getByRole("button", { name: "Hide recent runs for Apollo" }),
    ).toHaveAttribute("aria-expanded", "true");
    expect(queryKeys.find((k) => k[2] === "project-tasks")).toEqual([
      "dashboard", "ws-1", "project-tasks", 30, "project-apollo", "UTC", null, "squad-1",
    ]);
    const runs = within(screen.getByRole("list", { name: "Recent runs" })).getAllByRole("listitem");
    expect(runs).toHaveLength(1);
    expect(within(runs[0] as HTMLElement).getByRole("link", { name: /ACM-42/ })).toHaveAttribute(
      "href",
      "/acme/issues/ACM-42",
    );

    // Opening another row closes the first.
    await user.click(screen.getByRole("button", { name: "Show recent runs for No project" }));
    expect(screen.getByRole("button", { name: "Show recent runs for Apollo" })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
    expect(queryKeys.some((k) => k[2] === "project-tasks" && k[4] === "__no_project__")).toBe(true);
  });

  it("renders in Simplified Chinese", () => {
    renderTab("zh-Hans");
    expect(screen.getByText("无项目")).toBeInTheDocument();
    expect(screen.getByText("你无权查看的项目")).toBeInTheDocument();
    expect(screen.getByText("2 个项目")).toBeInTheDocument();
  });
});
