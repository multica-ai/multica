import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { renderWithI18n } from "../test/i18n";
import { AppBottomBar } from "./bottom-bar";

interface TeamStatus {
  working: { id: string; name: string; avatar_url: string | null }[];
  stuck: { id: string }[];
  byAgent: Map<string, { queuedCount: number; runningCount?: number }>;
}

const h = vi.hoisted(() => ({
  chat: { floatingChatEnabled: true },
  isMobile: false,
  workspace: { id: "ws-test" } as { id: string } | null,
  team: { working: [], stuck: [], byAgent: new Map() } as TeamStatus,
  snapshot: vi.fn(),
  getIssue: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return {
    ...actual,
    api: { ...actual.api, getBaseUrl: () => "http://localhost:8080", getAgentTaskSnapshot: h.snapshot, getIssue: h.getIssue },
  };
});

vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector: (state: typeof h.chat) => unknown) => selector(h.chat),
    { getState: () => h.chat },
  ),
}));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => h.workspace,
  useWorkspacePaths: () => ({ chat: () => "/acme/chat", agents: () => "/acme/agents" }),
}));
vi.mock("@multica/ui/hooks/use-mobile", () => ({ useIsMobile: () => h.isMobile }));
vi.mock("./panels/team-status", () => ({ useTeamAgentStatus: () => h.team }));
vi.mock("./app-shell-context", () => ({ useAppShell: () => null }));
vi.mock("../chat/components/chat-bar", () => ({ ChatBar: () => <div data-testid="chat-bar" /> }));

function renderBar(pathname = "/acme/issues") {
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname,
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  return renderWithI18n(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <NavigationProvider value={adapter}>
        <AppBottomBar />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

function workingAgents(count: number) {
  h.team.working = Array.from({ length: count }, (_, i) => ({
    id: `a${i + 1}`, name: `Agent ${i + 1}`, avatar_url: null,
  }));
  h.team.byAgent = new Map(h.team.working.map((agent) => [agent.id, { runningCount: 1, queuedCount: 0 }]));
}

beforeEach(() => {
  h.chat.floatingChatEnabled = true;
  h.isMobile = false;
  h.workspace = { id: "ws-test" };
  h.team = { working: [], stuck: [], byAgent: new Map() };
  h.snapshot.mockReset().mockResolvedValue([]);
  h.getIssue.mockReset().mockResolvedValue({ title: "Review the activity overview" });
});

describe("AppBottomBar", () => {
  it("shows quick chat and no team status while the team is idle", () => {
    renderBar();

    expect(screen.getByRole("contentinfo", { name: "Status bar" })).toBeInTheDocument();
    expect(screen.getByTestId("chat-bar")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("counts the agents that are working and links to the team", () => {
    workingAgents(4);
    renderBar();

    const link = screen.getByRole("link", { name: "4 agents working" });
    expect(link).toHaveAttribute("href", "/acme/agents");
    expect(link.querySelector("[data-slot='avatar']")).not.toBeInTheDocument();
    expect(screen.queryByText("Agent 1")).not.toBeInTheDocument();
    expect(h.snapshot).not.toHaveBeenCalled();
  });

  it("previews members and their running issue on hover without showing completed work", async () => {
    const user = userEvent.setup();
    workingAgents(2);
    h.snapshot.mockResolvedValue([
      { id: "old", agent_id: "a1", status: "completed", issue_id: "old-issue" },
      { id: "queued", agent_id: "a1", status: "queued", issue_id: "queued-issue" },
      { id: "live", agent_id: "a1", status: "running", issue_id: "live-issue" },
      { id: "chat", agent_id: "a2", status: "running", issue_id: "", chat_session_id: "chat-1" },
    ]);
    renderBar();
    await user.hover(screen.getByRole("link", { name: "2 agents working" }));

    expect(await screen.findByText("Agent 1")).toBeInTheDocument();
    expect(screen.getByText("Agent 2")).toBeInTheDocument();
    expect(await screen.findByText("Review the activity overview")).toBeInTheDocument();
    expect(screen.getByText("1 task running")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Team" })).toHaveAttribute("href", "/acme/agents");
    expect(h.getIssue.mock.calls).toEqual([["live-issue"]]);
  });

  it("opens the member preview on keyboard focus and keeps the trigger navigable", async () => {
    const user = userEvent.setup();
    workingAgents(1);
    renderBar();
    const trigger = screen.getByRole("link", { name: "1 agent working" });

    await user.tab();
    expect(trigger).toHaveFocus();
    expect(await screen.findByText("Agent 1")).toBeInTheDocument();
    await user.tab();
    expect(screen.getByRole("link", { name: "Team" })).toHaveFocus();
    expect(within(trigger).queryByText("Agent 1")).not.toBeInTheDocument();
  });

  it("limits the preview to five members and links to the rest", async () => {
    const user = userEvent.setup();
    workingAgents(7);
    renderBar();
    await user.hover(screen.getByRole("link", { name: "7 agents working" }));

    expect(await screen.findByText("Agent 5")).toBeInTheDocument();
    expect(screen.queryByText("Agent 6")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Show 2 more…" })).toHaveAttribute("href", "/acme/agents");
  });

  it("adds up the tasks waiting on offline runtimes across agents", () => {
    h.team.stuck = [{ id: "a1" }, { id: "a2" }];
    h.team.byAgent = new Map([
      ["a1", { queuedCount: 2 }],
      ["a2", { queuedCount: 1 }],
    ]);
    renderBar();

    expect(
      screen.getByRole("link", { name: "3 tasks waiting on an offline runtime" }),
    ).toHaveAttribute("href", "/acme/agents");
  });

  it("drops quick chat on the Chat tab, which already shows it full-page", () => {
    renderBar("/acme/chat");
    expect(screen.queryByTestId("chat-bar")).not.toBeInTheDocument();
    expect(screen.getByRole("contentinfo")).toBeInTheDocument();
  });

  it("drops quick chat when the floating chat preference is off", () => {
    h.chat.floatingChatEnabled = false;
    renderBar();

    expect(screen.queryByTestId("chat-bar")).not.toBeInTheDocument();
  });

  it("renders nothing on a narrow window or without a workspace", () => {
    h.isMobile = true;
    expect(renderBar().container).toBeEmptyDOMElement();

    h.isMobile = false;
    h.workspace = null;
    expect(renderBar().container).toBeEmptyDOMElement();
  });
});
