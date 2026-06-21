import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ButtonHTMLAttributes, ReactNode } from "react";

const mockCerebroRequest = vi.hoisted(() => vi.fn());
const mockListAutopilots = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: { cerebroRequest: mockCerebroRequest, listAutopilots: mockListAutopilots },
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

// The Condition editor lives in a Radix Popover, which portals + relies on
// pointer APIs jsdom doesn't implement. We flatten it the same way the dropdown
// is flattened: a context threads the `open`/`onOpenChange` the component drives,
// the trigger toggles it, and the content renders only while open — so opening,
// editing and saving a condition can be driven directly.
vi.mock("@multica/ui/components/ui/popover", async () => {
  const React = await import("react");
  const Ctx = React.createContext<{ open: boolean; onOpenChange: (v: boolean) => void }>({
    open: false,
    onOpenChange: () => {},
  });
  return {
    Popover: ({
      open,
      onOpenChange,
      children,
    }: {
      open?: boolean;
      onOpenChange?: (v: boolean) => void;
      children: ReactNode;
    }) => (
      <Ctx.Provider value={{ open: !!open, onOpenChange: onOpenChange ?? (() => {}) }}>
        {children}
      </Ctx.Provider>
    ),
    PopoverTrigger: ({
      children,
      onClick,
      ...props
    }: { children: ReactNode } & ButtonHTMLAttributes<HTMLButtonElement>) => {
      const { open, onOpenChange } = React.useContext(Ctx);
      return (
        <button
          type="button"
          {...props}
          onClick={(e) => {
            onClick?.(e);
            onOpenChange(!open);
          }}
        >
          {children}
        </button>
      );
    },
    PopoverContent: ({ children }: { children: ReactNode }) => {
      const { open } = React.useContext(Ctx);
      return open ? <div>{children}</div> : null;
    },
    PopoverTitle: ({ children }: { children: ReactNode }) => <div>{children}</div>,
    PopoverDescription: ({ children }: { children: ReactNode }) => <div>{children}</div>,
    PopoverHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  };
});

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
  // The table no longer lists autopilots (System is its own actor page, not a
  // picker); the mock stays defined so the api shape is complete.
  mockListAutopilots.mockReset();
  mockListAutopilots.mockResolvedValue({ autopilots: [] });
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

  it("names the blocking group instead of lying with 'Override on Agent' (TECH-3287 hul 2/5)", async () => {
    // create_issue: the agent layer says Allow, but a group denies it. The old
    // code printed "Override on Agent" — the lie Jesper hit. It must now name the
    // group AND its owner so the admin knows where to change it.
    mockCerebroRequest.mockResolvedValue({
      tools: [
        {
          tool_key: "create_issue",
          title: "Create issue",
          category: "Issues",
          source: "builtin",
          layers: { runtime: null, agent: "allow", group: "deny", user: null },
          effective: { setting: "deny", decided_by: "group", capped_by: "group", reason: "Capped by group" },
          capped_by_groups: [{ name: "All members", owner: "Jesper Hvejsel" }],
        },
      ],
    });
    renderTable("agent");
    const row = await screen.findByTestId("tool-row-create_issue");
    expect(
      within(row).getByText("Capped by group All members (ejer: Jesper Hvejsel)"),
    ).toBeInTheDocument();
    expect(within(row).queryByText("Override on Agent")).not.toBeInTheDocument();
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
    const classFilter = screen.getByRole("combobox", { name: "Filter by class" });
    expect(classFilter).toHaveTextContent("All classes");
    expect(classFilter).not.toHaveTextContent("__all__");
    await user.click(classFilter);
    await user.click(await screen.findByRole("option", { name: /MCP · Slack/ }));
    expect(classFilter).toHaveTextContent("MCP · Slack");
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

  it("a repo capability with a concrete rule shows read/checkout/push action chips (no host)", async () => {
    const user = userEvent.setup();
    // repo.push carries a concrete agent rule so the When editor can open.
    mockCerebroRequest.mockResolvedValue({
      tools: [
        repoCap("repo.read", "Read code"),
        repoCap("repo.checkout", "Check out"),
        repoCap("repo.push", "Push changes", "allow", { agent: "allow" }),
      ],
    });
    renderRepoTable();
    const group = await screen.findByTestId(`repo-group-${REPO_URL}`);
    await user.click(within(group).getByRole("button", { expanded: false }));
    const push = screen.getByTestId(`repo-cap-repo.push-${REPO_URL}`);
    await user.click(within(push).getByTestId("condition-control-repo.push"));
    const editor = within(await screen.findByTestId("condition-editor-repo.push"));
    // Preset action chips for the repo verb model.
    expect(editor.getByLabelText("Action read")).toBeInTheDocument();
    expect(editor.getByLabelText("Action checkout")).toBeInTheDocument();
    expect(editor.getByLabelText("Action push")).toBeInTheDocument();
    // A repo capability is not host-bound → no host section.
    expect(editor.queryByLabelText("Add host")).not.toBeInTheDocument();
  });

  it("writes the repo condition with the toggled preset actions and the repo resource", async () => {
    const user = userEvent.setup();
    mockCerebroRequest.mockResolvedValue({
      tools: [repoCap("repo.push", "Push changes", "allow", { agent: "allow" })],
    });
    renderRepoTable();
    const group = await screen.findByTestId(`repo-group-${REPO_URL}`);
    await user.click(within(group).getByRole("button", { expanded: false }));
    const push = screen.getByTestId(`repo-cap-repo.push-${REPO_URL}`);
    await user.click(within(push).getByTestId("condition-control-repo.push"));
    const editor = within(await screen.findByTestId("condition-editor-repo.push"));
    await user.click(editor.getByLabelText("Action push"));
    await user.click(editor.getByRole("button", { name: "Save" }));
    await waitFor(() => {
      const put = findPutCalls().at(-1);
      const body = JSON.parse((put![1] as RequestInit).body as string);
      expect(body).toMatchObject({
        tool_key: "repo.push",
        layer: "agent",
        subject_id: "agent-1",
        setting: "allow",
        resource_pattern: REPO_URL,
      });
      expect(body.condition.actions).toEqual(["push"]);
    });
  });
});

// FIR-1609 — the WHEN layer (Condition) sits beside the Decision pill. A
// condition only narrows when a rule applies; it never moves Allow/Ask/Deny, and
// it can only attach to a concrete rule already authored on this page's layer.
describe("ToolPolicyTable (Condition editor)", () => {
  const COND_TABLE = {
    tools: [
      {
        // Concrete agent rule (allow) carrying a host condition.
        tool_key: "web_fetch",
        title: "Fetch a URL",
        category: "Built-in tools",
        source: "builtin",
        layers: { runtime: null, agent: "allow", group: null, user: null },
        conditions: {
          workspace: null,
          runtime: null,
          agent: { host_allowlist: ["firtal.com", "*.firtal.com"], actions: [], expr: "" },
          user: null,
        },
        effective: { setting: "allow", decided_by: "agent", capped_by: "", reason: "" },
      },
      {
        // No override on the agent layer → nothing for a condition to refine.
        tool_key: "list_issues",
        title: "List issues",
        category: "Built-in tools",
        source: "builtin",
        layers: { runtime: null, agent: null, group: null, user: null },
        effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
      },
    ],
  };

  beforeEach(() => {
    mockCerebroRequest.mockReset();
    mockCerebroRequest.mockResolvedValue(COND_TABLE);
  });

  function renderCondTable() {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return render(
      <QueryClientProvider client={qc}>
        <ToolPolicyTable wsId="ws-1" view="agent" subjectId="agent-1" runtimeId="rt-1" userId="user-1" />
      </QueryClientProvider>,
    );
  }

  it("shows the persisted condition as the trigger summary", async () => {
    renderCondTable();
    const row = await screen.findByTestId("tool-row-web_fetch");
    // "firtal.com +1" — first host plus the count of the rest.
    expect(within(row).getByTestId("condition-control-web_fetch")).toHaveTextContent(
      "firtal.com +1",
    );
  });

  it("shows the When control on a generic gated tool (CEL is available everywhere)", async () => {
    // FIR-1708 C: WHEN must be available everywhere (no trimming). list_issues has
    // no host/action facet, but it is chain-gated, so the CEL escape hatch applies
    // and the control must render. The fallback heuristic (no enforced_conditions
    // on the mock) keeps CEL on, matching an older backend.
    renderCondTable();
    const row = await screen.findByTestId("tool-row-list_issues");
    expect(within(row).getByTestId("condition-control-list_issues")).toBeInTheDocument();
  });

  it("hides the When control when the server reports no enforced condition kinds", async () => {
    // A capability the chain does not gate (managed-externally) reports
    // enforced_conditions: [] — no WHEN kind can bite, so the control is hidden.
    mockCerebroRequest.mockResolvedValueOnce({
      tools: [
        {
          tool_key: "manage_runtime",
          title: "Manage runtime",
          category: "Runtimes",
          source: "platform",
          managed_externally: true,
          enforced_conditions: [],
          layers: { runtime: null, agent: null, group: null, user: null },
          effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
        },
      ],
    });
    renderCondTable();
    const row = await screen.findByTestId("tool-row-manage_runtime");
    expect(within(row).queryByTestId("condition-control-manage_runtime")).not.toBeInTheDocument();
  });

  it("renders only the server-reported condition kinds (action hidden, CEL shown)", async () => {
    // web_fetch reports host + cel but NOT action → the host section and the CEL
    // escape hatch render, but no action chips, even though the row is host-bound.
    const user = userEvent.setup();
    mockCerebroRequest.mockResolvedValue({
      tools: [
        {
          tool_key: "web_fetch",
          title: "Fetch a URL",
          category: "Built-in tools",
          source: "builtin",
          enforced_conditions: ["host", "cel"],
          layers: { runtime: null, agent: "allow", group: null, user: null },
          conditions: {
            workspace: null,
            runtime: null,
            agent: { host_allowlist: ["firtal.com"], actions: [], expr: "" },
            user: null,
          },
          effective: { setting: "allow", decided_by: "agent", capped_by: "", reason: "" },
        },
      ],
    });
    renderCondTable();
    const row = await screen.findByTestId("tool-row-web_fetch");
    await user.click(within(row).getByTestId("condition-control-web_fetch"));
    const editor = within(await screen.findByTestId("condition-editor-web_fetch"));
    // host kind reported → host input present; cel reported → Advanced (CEL) shown.
    expect(editor.getByLabelText("Add host")).toBeInTheDocument();
    expect(editor.getByText("Advanced (CEL)")).toBeInTheDocument();
    // action kind NOT reported → no preset action chips.
    expect(editor.queryByLabelText(/^Action /)).not.toBeInTheDocument();
  });

  it("shows the Host allow-list section only on a host-bound tool (web_fetch)", async () => {
    const user = userEvent.setup();
    renderCondTable();
    const row = await screen.findByTestId("tool-row-web_fetch");
    await user.click(within(row).getByTestId("condition-control-web_fetch"));
    const editor = within(await screen.findByTestId("condition-editor-web_fetch"));
    // web_fetch has the host facet → the host input is present.
    expect(editor.getByLabelText("Add host")).toBeInTheDocument();
    // …and no actions facet for web_fetch, so no preset action chips.
    expect(editor.queryByLabelText(/^Action /)).not.toBeInTheDocument();
  });

  it("disables the When control (concrete-rule hint) on a meaningful tool with no rule", async () => {
    // A host-bound tool with no concrete rule on this layer keeps the disabled
    // "When" hint (the facet IS meaningful), pointing the admin at the Decision.
    mockCerebroRequest.mockResolvedValue({
      tools: [
        {
          tool_key: "web_fetch",
          title: "Fetch a URL",
          category: "Built-in tools",
          source: "builtin",
          layers: { runtime: null, agent: null, group: null, user: null },
          effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
        },
      ],
    });
    renderCondTable();
    const row = await screen.findByTestId("tool-row-web_fetch");
    const control = within(row).getByTestId("condition-control-web_fetch");
    expect(control).toBeDisabled();
    expect(control).toHaveTextContent("When");
  });

  it("editing a condition writes the unchanged Decision plus the condition", async () => {
    const user = userEvent.setup();
    renderCondTable();
    const row = await screen.findByTestId("tool-row-web_fetch");
    // Open the editor (seeded from the persisted host list), add a second host.
    await user.click(within(row).getByTestId("condition-control-web_fetch"));
    const editor = within(await screen.findByTestId("condition-editor-web_fetch"));
    // Enter adds the host (the same path the Add button drives).
    await user.type(editor.getByLabelText("Add host"), "docs.anthropic.com{Enter}");
    await user.click(editor.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const put = findPutCalls().at(-1);
      expect(put).toBeTruthy();
      const body = JSON.parse((put![1] as RequestInit).body as string);
      expect(body).toMatchObject({
        tool_key: "web_fetch",
        layer: "agent",
        subject_id: "agent-1",
        setting: "allow",
      });
      expect(body.condition.host_allowlist).toEqual([
        "firtal.com",
        "*.firtal.com",
        "docs.anthropic.com",
      ]);
    });
  });

  it("clearing a condition writes the Decision with condition: null", async () => {
    const user = userEvent.setup();
    renderCondTable();
    const row = await screen.findByTestId("tool-row-web_fetch");
    await user.click(within(row).getByTestId("condition-control-web_fetch"));
    const editor = within(await screen.findByTestId("condition-editor-web_fetch"));
    await user.click(editor.getByRole("button", { name: "Clear" }));
    await waitFor(() => {
      const put = findPutCalls().at(-1);
      const body = JSON.parse((put![1] as RequestInit).body as string);
      expect(body).toMatchObject({ tool_key: "web_fetch", layer: "agent", setting: "allow" });
      expect(body.condition).toBeNull();
    });
  });
});

describe("ToolPolicyTable (firtal_registry data sources — FIR-1609 Phase 5)", () => {
  it("surfaces per-data-source rows under a Data sources button, not as flat rows", async () => {
    // The server folds each registry data source into a per-resource row under
    // firtal_registry. They must NOT appear as flat catalog rows or repo groups —
    // they belong in the firtal_registry row's "Data sources" sheet.
    mockCerebroRequest.mockResolvedValue({
      tools: [
        {
          tool_key: "firtal_registry",
          resource_pattern: "",
          title: "Firtal Data Registry",
          category: "Built-in tools",
          source: "builtin",
          layers: { runtime: null, agent: "allow", group: null, user: null },
          effective: { setting: "allow", decided_by: "agent", capped_by: "", reason: "" },
        },
        {
          tool_key: "firtal_registry",
          resource_pattern: "ds-orders",
          title: "Orders",
          category: "Data sources",
          source: "registry-data-source",
          layers: { runtime: null, agent: "allow", group: null, user: null },
          effective: { setting: "allow", decided_by: "agent", capped_by: "", reason: "" },
        },
        {
          tool_key: "firtal_registry",
          resource_pattern: "ds-finance",
          title: "Finance",
          category: "Data sources",
          source: "registry-data-source",
          layers: { runtime: null, agent: "deny", group: null, user: null },
          effective: { setting: "deny", decided_by: "agent", capped_by: "", reason: "" },
        },
      ],
    });
    renderTable("agent");

    // The capability row renders, with a "Data sources (2)" button.
    const capRow = await screen.findByTestId("tool-row-firtal_registry");
    expect(within(capRow).getByText("Firtal Data Registry")).toBeInTheDocument();
    expect(
      within(capRow).getAllByRole("button", { name: /Data sources \(2\)/ }).length,
    ).toBeGreaterThan(0);

    // The per-source rows are NOT flat catalog rows of their own.
    expect(screen.queryByText("Orders")).not.toBeInTheDocument();
    expect(screen.queryByText("Finance")).not.toBeInTheDocument();
  });
});

// FIR-1692 — System is a first-class ACTOR, not a stacked layer. The autopilot's
// own permissions page renders the table with view="system", subjectId=<autopilot
// id>: it sends that id as system_id, authors layer="system" keyed on the
// autopilot through the page's OWN Decision/When columns, and there is no picker
// bar or extra System column hanging off the agent/member pages anymore.
describe("ToolPolicyTable (System actor — FIR-1692)", () => {
  const SYS_TABLE = {
    tools: [
      {
        tool_key: "web_fetch",
        title: "Fetch a URL",
        category: "Built-in tools",
        source: "builtin",
        layers: { workspace: null, runtime: null, agent: null, group: null, user: null, system: "allow" },
        conditions: {
          workspace: null,
          runtime: null,
          agent: null,
          user: null,
          system: { host_allowlist: ["firtal.com"], actions: [], expr: "" },
        },
        effective: { setting: "allow", decided_by: "system", capped_by: "", reason: "" },
      },
    ],
  };

  beforeEach(() => {
    mockCerebroRequest.mockReset();
    mockCerebroRequest.mockResolvedValue(SYS_TABLE);
    mockListAutopilots.mockReset();
    mockListAutopilots.mockResolvedValue({ autopilots: [] });
  });

  function renderSystemActor(autopilotId = "ap-1") {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return render(
      <QueryClientProvider client={qc}>
        <ToolPolicyTable wsId="ws-1" view="system" subjectId={autopilotId} />
      </QueryClientProvider>,
    );
  }

  it("the System actor page sends the autopilot id as system_id", async () => {
    renderSystemActor("ap-1");
    await waitFor(() => {
      const get = mockCerebroRequest.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === undefined && String(c[0]).includes("system_id=ap-1"),
      );
      expect(get).toBeTruthy();
    });
  });

  it("never renders the old System-layer picker bar (it is gone from every page)", async () => {
    renderSystemActor("ap-1");
    await screen.findByRole("table");
    expect(screen.queryByTestId("system-layer-bar")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Scope the System layer to an autopilot")).not.toBeInTheDocument();
  });

  it("the page's Decision column writes layer=system keyed on the autopilot", async () => {
    const user = userEvent.setup();
    renderSystemActor("ap-1");
    const body = within(await screen.findByRole("table"));
    await user.click(body.getByLabelText(/^Decision:/));
    await user.click(body.getByRole("menuitem", { name: "Deny" }));

    await waitFor(() => {
      const put = findPutCalls().at(-1);
      expect(put).toBeTruthy();
      expect(JSON.parse((put![1] as RequestInit).body as string)).toMatchObject({
        tool_key: "web_fetch",
        layer: "system",
        subject_id: "ap-1",
        setting: "deny",
      });
    });
  });

  it("the When editor refines the system rule, writing layer=system with the condition", async () => {
    const user = userEvent.setup();
    renderSystemActor("ap-1");
    const body = within(await screen.findByRole("table"));
    // The persisted system condition is summarised on the trigger.
    expect(body.getByTestId("condition-control-web_fetch")).toHaveTextContent("firtal.com");
    await user.click(body.getByTestId("condition-control-web_fetch"));
    const editor = within(await screen.findByTestId("condition-editor-web_fetch"));
    await user.type(editor.getByLabelText("Add host"), "docs.anthropic.com{Enter}");
    await user.click(editor.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const put = findPutCalls().at(-1);
      const reqBody = JSON.parse((put![1] as RequestInit).body as string);
      expect(reqBody).toMatchObject({ tool_key: "web_fetch", layer: "system", subject_id: "ap-1", setting: "allow" });
      expect(reqBody.condition.host_allowlist).toEqual(["firtal.com", "docs.anthropic.com"]);
    });
  });
});

describe("ToolPolicyTable runtime/multica tab split (FIR-1708 D(a))", () => {
  const SPLIT = {
    tools: [
      {
        tool_key: "tools:Bash",
        title: "Bash",
        category: "Built-in tools",
        source: "runtime_report",
        layers: { runtime: null, agent: null, group: null, user: null },
        effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
      },
      {
        tool_key: "manage_runtime",
        title: "Manage runtime",
        category: "Runtimes",
        source: "platform",
        layers: { runtime: null, agent: null, group: null, user: null },
        effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
      },
      {
        tool_key: "add_comment",
        title: "Add comment",
        category: "Issues",
        source: "builtin",
        layers: { runtime: null, agent: null, group: null, user: null },
        effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
      },
    ],
  };

  function renderTab(tabFilter: "runtime" | "multica") {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return render(
      <QueryClientProvider client={qc}>
        <ToolPolicyTable
          wsId="ws-1"
          view="workspace"
          subjectId="ws-1"
          runtimeId="rt-1"
          userId="user-1"
          tabFilter={tabFilter}
        />
      </QueryClientProvider>,
    );
  }

  it("Runtime tab shows runtime-reported tools, not only runtime-admin actions", async () => {
    mockCerebroRequest.mockResolvedValue(SPLIT);
    renderTab("runtime");
    const table = await tableBody();
    // The regression fix: a runtime-reported tool now appears on the Runtime tab.
    expect(table.getByText("Bash")).toBeInTheDocument();
    // Runtime-admin platform actions stay on the Runtime tab too.
    expect(table.getByText("Manage runtime")).toBeInTheDocument();
    // A generic platform tool belongs to the Multica tab, not here.
    expect(table.queryByText("Add comment")).not.toBeInTheDocument();
  });

  it("Multica tab excludes runtime-reported tools and runtime-admin actions", async () => {
    mockCerebroRequest.mockResolvedValue(SPLIT);
    renderTab("multica");
    const table = await tableBody();
    expect(table.getByText("Add comment")).toBeInTheDocument();
    expect(table.queryByText("Bash")).not.toBeInTheDocument();
    expect(table.queryByText("Manage runtime")).not.toBeInTheDocument();
  });
});
