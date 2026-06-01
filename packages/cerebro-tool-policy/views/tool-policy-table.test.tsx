import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ButtonHTMLAttributes, ReactNode } from "react";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: { cerebroRequest: mockCerebroRequest },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// Base UI dropdown menus render through a portal and are awkward to drive in
// jsdom, so we flatten them — the trigger and items render inline as plain
// buttons. This is the established pattern across the repo's view tests, and it
// lets us assert the decision-pill behaviour (open → choose → write) directly.
vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({
    children,
    ...props
  }: {
    children: ReactNode;
  } & ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button type="button" {...props}>
      {children}
    </button>
  ),
  DropdownMenuContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({
    children,
    onClick,
  }: {
    children: ReactNode;
    onClick?: () => void;
  }) => (
    <button type="button" role="menuitem" onClick={onClick}>
      {children}
    </button>
  ),
}));

import { ToolPolicyTable } from "./tool-policy-table";

const TABLE = {
  tools: [
    {
      tool_key: "slack.post_message",
      title: "Post Slack message",
      category: "MCP · Slack",
      source: "scan",
      layers: { runtime: "allow", agent: "allow", group: null, user: "deny" },
      effective: { setting: "deny", decided_by: "user", capped_by: "user", reason: "Capped by user" },
    },
    {
      tool_key: "deploy_restart",
      title: "Restart deploy",
      category: "Built-in tools",
      source: "builtin",
      layers: { runtime: null, agent: "ask", group: null, user: null },
      effective: { setting: "ask", decided_by: "agent", capped_by: "", reason: "Ask by agent" },
    },
    {
      tool_key: "list_issues",
      title: "List issues",
      category: "Built-in tools",
      source: "builtin",
      layers: { runtime: null, agent: null, group: null, user: null },
      effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
    },
  ],
};

function renderTable(view: "agent" | "runtime" = "agent") {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ToolPolicyTable wsId="ws-1" view={view} subjectId="agent-1" runtimeId="rt-1" userId="user-1" />
    </QueryClientProvider>,
  );
}

function findPutCalls() {
  return mockCerebroRequest.mock.calls.filter(
    (c) => (c[1] as RequestInit | undefined)?.method === "PUT",
  );
}

beforeEach(() => {
  mockCerebroRequest.mockReset();
  mockCerebroRequest.mockResolvedValue(TABLE);
});

// The desktop table and the mobile cards both render in jsdom (CSS hides one),
// so row-level assertions scope to the <table> to avoid double matches.
async function tableBody() {
  return within(await screen.findByRole("table"));
}

describe("ToolPolicyTable (capability catalog)", () => {
  it("renders one flat row per tool with its class, side effect and resolved-by layer", async () => {
    renderTable("agent");
    const table = await tableBody();
    expect(table.getByText("Post Slack message")).toBeInTheDocument();
    expect(table.getByText("Restart deploy")).toBeInTheDocument();
    expect(table.getByText("List issues")).toBeInTheDocument();
    // Class label appears, never a raw id.
    expect(table.getAllByText("MCP · Slack").length).toBeGreaterThan(0);
    // The all-inherited row has no override on this level → inherited from the
    // workspace default at the root of the chain.
    const listRow = screen.getByTestId("tool-row-list_issues");
    expect(
      within(listRow).getByText("Inherited from Workspace default"),
    ).toBeInTheDocument();
  });

  it("labels each row as an override on this level or inherited from another", async () => {
    renderTable("agent");
    // deploy_restart has layers.agent="ask" → an override authored on the agent.
    const agentOverride = await screen.findByTestId("tool-row-deploy_restart");
    expect(within(agentOverride).getByText("Override on Agent")).toBeInTheDocument();
    // list_issues has no setting on any layer → inherited from the root default.
    const inherited = screen.getByTestId("tool-row-list_issues");
    expect(
      within(inherited).getByText("Inherited from Workspace default"),
    ).toBeInTheDocument();
  });

  it("on the runtime page the same row reads as inherited, not an override", async () => {
    // deploy_restart is set on the agent layer, not the runtime layer. Viewed
    // from the runtime page it must read as inherited (from the agent), proving
    // the origin badge is relative to the level the page authors.
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ToolPolicyTable wsId="ws-1" view="runtime" subjectId="rt-1" />
      </QueryClientProvider>,
    );
    const row = await screen.findByTestId("tool-row-deploy_restart");
    expect(within(row).getByText("Inherited from Agent")).toBeInTheDocument();
    expect(within(row).queryByText("Override on Runtime")).not.toBeInTheDocument();
  });

  it("writes the chosen setting to the agent layer when a decision is picked", async () => {
    const user = userEvent.setup();
    renderTable("agent");
    const slackRow = await screen.findByTestId("tool-row-slack.post_message");
    // Open the decision pill (current verdict = Deny) and choose Ask.
    await user.click(within(slackRow).getByLabelText(/^Decision:/));
    await user.click(within(slackRow).getByRole("menuitem", { name: "Ask" }));

    await waitFor(() => {
      const put = findPutCalls().at(-1);
      expect(put).toBeTruthy();
      expect(JSON.parse((put![1] as RequestInit).body as string)).toEqual({
        tool_key: "slack.post_message",
        layer: "agent",
        subject_id: "agent-1",
        setting: "ask",
      });
    });
  });

  it("choosing Inherit clears the layer via DELETE", async () => {
    const user = userEvent.setup();
    renderTable("agent");
    const slackRow = await screen.findByTestId("tool-row-slack.post_message");
    await user.click(within(slackRow).getByLabelText(/^Decision:/));
    await user.click(within(slackRow).getByRole("menuitem", { name: /Inherit/ }));
    await waitFor(() => {
      const del = mockCerebroRequest.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === "DELETE",
      );
      expect(del).toBeTruthy();
      expect(String(del![0])).toContain("layer=agent");
    });
  });

  it("the Deny decision filter keeps only tools whose effective verdict is deny", async () => {
    const user = userEvent.setup();
    renderTable("agent");
    const table = await tableBody();
    // Decision filter chips are pressable; pick Deny.
    await user.click(screen.getByRole("button", { name: "Deny", pressed: false }));
    expect(table.getByText("Post Slack message")).toBeInTheDocument();
    expect(table.queryByText("Restart deploy")).not.toBeInTheDocument();
    expect(table.queryByText("List issues")).not.toBeInTheDocument();
  });

  it("a class filter narrows the catalog to that capability class", async () => {
    const user = userEvent.setup();
    renderTable("agent");
    const table = await tableBody();
    await user.click(screen.getByRole("button", { name: /MCP · Slack/ }));
    expect(table.getByText("Post Slack message")).toBeInTheDocument();
    expect(table.queryByText("Restart deploy")).not.toBeInTheDocument();
  });

  it("runtime view writes to the runtime layer", async () => {
    const user = userEvent.setup();
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ToolPolicyTable wsId="ws-1" view="runtime" subjectId="rt-1" />
      </QueryClientProvider>,
    );
    const slackRow = await screen.findByTestId("tool-row-slack.post_message");
    await user.click(within(slackRow).getByLabelText(/^Decision:/));
    await user.click(within(slackRow).getByRole("menuitem", { name: "Allow" }));
    await waitFor(() => {
      const put = findPutCalls().at(-1);
      expect(JSON.parse((put![1] as RequestInit).body as string)).toMatchObject({
        layer: "runtime",
        subject_id: "rt-1",
        setting: "allow",
      });
    });
  });

  // FIR-2284 Bid 5 — the three new surfaces author their own chain layer.
  function renderView(view: "workspace" | "group" | "member", subjectId: string) {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return render(
      <QueryClientProvider client={qc}>
        <ToolPolicyTable wsId="ws-1" view={view} subjectId={subjectId} />
      </QueryClientProvider>,
    );
  }

  it("workspace view writes to the workspace root layer keyed on the workspace", async () => {
    const user = userEvent.setup();
    renderView("workspace", "ws-1");
    const slackRow = await screen.findByTestId("tool-row-slack.post_message");
    await user.click(within(slackRow).getByLabelText(/^Decision:/));
    await user.click(within(slackRow).getByRole("menuitem", { name: "Deny" }));
    await waitFor(() => {
      const put = findPutCalls().at(-1);
      expect(JSON.parse((put![1] as RequestInit).body as string)).toMatchObject({
        layer: "workspace",
        subject_id: "ws-1",
        setting: "deny",
      });
    });
  });

  it("group view writes to the group layer and fetches the table for that group", async () => {
    const user = userEvent.setup();
    renderView("group", "grp-7");
    // The table GET carries the group_id for the Effective chain.
    await waitFor(() => {
      const get = mockCerebroRequest.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === undefined,
      );
      expect(String(get![0])).toContain("group_id=grp-7");
    });
    const slackRow = await screen.findByTestId("tool-row-slack.post_message");
    await user.click(within(slackRow).getByLabelText(/^Decision:/));
    await user.click(within(slackRow).getByRole("menuitem", { name: "Allow" }));
    await waitFor(() => {
      const put = findPutCalls().at(-1);
      expect(JSON.parse((put![1] as RequestInit).body as string)).toMatchObject({
        layer: "group",
        subject_id: "grp-7",
        setting: "allow",
      });
    });
  });

  it("member view writes to the user (ceiling) layer keyed on the member's user id", async () => {
    const user = userEvent.setup();
    renderView("member", "user-42");
    await waitFor(() => {
      const get = mockCerebroRequest.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === undefined,
      );
      expect(String(get![0])).toContain("user_id=user-42");
    });
    const slackRow = await screen.findByTestId("tool-row-slack.post_message");
    await user.click(within(slackRow).getByLabelText(/^Decision:/));
    await user.click(within(slackRow).getByRole("menuitem", { name: "Deny" }));
    await waitFor(() => {
      const put = findPutCalls().at(-1);
      expect(JSON.parse((put![1] as RequestInit).body as string)).toMatchObject({
        layer: "user",
        subject_id: "user-42",
        setting: "deny",
      });
    });
  });
});

// FIR-2505 slice 3 — repos render as collapsible groups, not flat tool rows, and
// the group header cascades one choice to read/checkout/push.
describe("ToolPolicyTable (repo groups)", () => {
  const REPO_URL = "github.com/firtal-group/repo-a";
  const repoCap = (
    tool_key: string,
    title: string,
    setting: "allow" | "ask" | "deny" = "allow",
    layers: Partial<Record<string, string | null>> = {},
  ) => ({
    tool_key,
    resource_pattern: REPO_URL,
    title,
    category: "repo",
    source: "repo",
    layers: { workspace: null, runtime: null, agent: null, group: null, user: null, ...layers },
    effective: { setting, decided_by: "", capped_by: "", reason: "" },
  });

  const REPO_TABLE = {
    tools: [
      {
        tool_key: "add_comment",
        resource_pattern: "",
        title: "Add comment",
        category: "Issues",
        source: "builtin",
        layers: { workspace: null, runtime: null, agent: null, group: null, user: null },
        effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
      },
      repoCap("repo.read", "Read code"),
      repoCap("repo.checkout", "Check out"),
      repoCap("repo.push", "Push changes"),
    ],
  };

  beforeEach(() => {
    mockCerebroRequest.mockReset();
    mockCerebroRequest.mockResolvedValue(REPO_TABLE);
  });

  function renderRepoTable(view: "agent" | "workspace" = "agent", subjectId = "agent-1") {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return render(
      <QueryClientProvider client={qc}>
        <ToolPolicyTable wsId="ws-1" view={view} subjectId={subjectId} runtimeId="rt-1" userId="user-1" />
      </QueryClientProvider>,
    );
  }

  it("renders repos as a collapsible group, not as flat rows in the tool table", async () => {
    renderRepoTable();
    expect(await screen.findByTestId("repo-policy-section")).toBeInTheDocument();
    expect(screen.getByTestId(`repo-group-${REPO_URL}`)).toBeInTheDocument();
    // The capability-wide tool is still in the flat catalog…
    expect(await screen.findByTestId("tool-row-add_comment")).toBeInTheDocument();
    // …but the repo capabilities never appear as flat tool rows.
    expect(screen.queryByTestId("tool-row-repo.checkout")).not.toBeInTheDocument();
  });

  it("expanding a repo reveals its read / checkout / push capabilities", async () => {
    const user = userEvent.setup();
    renderRepoTable();
    const group = await screen.findByTestId(`repo-group-${REPO_URL}`);
    // Collapsed by default: capability rows are not rendered yet.
    expect(screen.queryByTestId(`repo-cap-repo.read-${REPO_URL}`)).not.toBeInTheDocument();
    await user.click(within(group).getByRole("button", { expanded: false }));
    expect(screen.getByTestId(`repo-cap-repo.read-${REPO_URL}`)).toBeInTheDocument();
    expect(screen.getByTestId(`repo-cap-repo.checkout-${REPO_URL}`)).toBeInTheDocument();
    expect(screen.getByTestId(`repo-cap-repo.push-${REPO_URL}`)).toBeInTheDocument();
  });

  it("setting the repo group cascades the choice to all three capabilities with the repo as resource", async () => {
    const user = userEvent.setup();
    renderRepoTable();
    const group = await screen.findByTestId(`repo-group-${REPO_URL}`);
    // Header pill shows the shared verdict (all allow) and cascades on change.
    await user.click(within(group).getByLabelText(/^Repository decision:/));
    await user.click(within(group).getByRole("menuitem", { name: /Deny/ }));
    await waitFor(() => {
      const puts = findPutCalls().map((c) => JSON.parse((c[1] as RequestInit).body as string));
      const forRepo = puts.filter((b) => b.resource_pattern === REPO_URL && b.setting === "deny");
      const tools = new Set(forRepo.map((b) => b.tool_key));
      expect(tools).toEqual(new Set(["repo.read", "repo.checkout", "repo.push"]));
      for (const b of forRepo) {
        expect(b).toMatchObject({ layer: "agent", subject_id: "agent-1" });
      }
    });
  });

  it("a single repo capability writes with its repo as the resource_pattern", async () => {
    const user = userEvent.setup();
    renderRepoTable();
    const group = await screen.findByTestId(`repo-group-${REPO_URL}`);
    await user.click(within(group).getByRole("button", { expanded: false }));
    const checkout = screen.getByTestId(`repo-cap-repo.checkout-${REPO_URL}`);
    await user.click(within(checkout).getByLabelText(/^Decision:/));
    await user.click(within(checkout).getByRole("menuitem", { name: "Ask" }));
    await waitFor(() => {
      const put = findPutCalls().at(-1);
      expect(JSON.parse((put![1] as RequestInit).body as string)).toEqual({
        tool_key: "repo.checkout",
        layer: "agent",
        subject_id: "agent-1",
        setting: "ask",
        resource_pattern: REPO_URL,
      });
    });
  });
});
