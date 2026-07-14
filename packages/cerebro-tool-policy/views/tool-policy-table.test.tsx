import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ButtonHTMLAttributes, ReactNode } from "react";

const mockCerebroRequest = vi.hoisted(() => vi.fn());
const mockListAutopilots = vi.hoisted(() => vi.fn());
const mockListCerebroGroups = vi.hoisted(() => vi.fn());
const mockUseFeatureFlag = vi.hoisted(() => vi.fn((_key: string) => false));
const mockToast = vi.hoisted(() => ({
  warning: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
  success: vi.fn(),
}));

// FIR-2706 follow-up: writes a higher layer overrides must toast an explanation
// instead of failing silently — the mock lets tests assert the message.
vi.mock("sonner", () => ({ toast: mockToast }));

vi.mock("@multica/cerebro-feature-flags", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/cerebro-feature-flags")>();
  return { ...actual, useFeatureFlag: (key: string) => mockUseFeatureFlag(key) };
});

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      cerebroRequest: mockCerebroRequest,
      listAutopilots: mockListAutopilots,
      listCerebroGroups: mockListCerebroGroups,
    },
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

import { ToolPolicyTable, ToolPolicyTabs, futileWriteWarning } from "./tool-policy-table";
import type { ToolPolicyRow } from "../core";

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
  mockListCerebroGroups.mockReset();
  mockListCerebroGroups.mockResolvedValue([]);
  // Credentials feature is OFF by default; the credential-specific tests opt in.
  mockUseFeatureFlag.mockReset();
  mockUseFeatureFlag.mockReturnValue(false);
});

// The desktop table and the mobile cards both render in jsdom (CSS hides one),
// so row-level assertions scope to the <table> to avoid double matches.
async function tableBody() {
  return within(await screen.findByRole("table"));
}

describe("ToolPolicyTable (capability catalog)", () => {
  it("renders one flat row per tool with its type and resolved-by layer", async () => {
    renderTable("agent");
    const table = await tableBody();
    expect(table.getByText("Post Slack message")).toBeInTheDocument();
    expect(table.getByText("Restart deploy")).toBeInTheDocument();
    expect(table.getByText("List issues")).toBeInTheDocument();
    // The Type column (FIR-2281): a scanned tool reads "Runtime", a built-in reads
    // "Multica". The old Class column ("MCP · Slack") is gone.
    expect(table.getAllByText("Runtime").length).toBeGreaterThan(0);
    expect(table.getAllByText("Multica").length).toBeGreaterThan(0);
    expect(table.queryByText("MCP · Slack")).not.toBeInTheDocument();
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
      within(row).getByText("Capped by group All members (owner: Jesper Hvejsel)"),
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

  it("a type filter narrows the catalog to that permission type", async () => {
    const user = userEvent.setup();
    renderTable("agent");
    const table = await tableBody();
    // The Type chips replace the old Class select (FIR-2281). Pick Runtime — only
    // the runtime-reported tool (Post Slack message, source "scan") survives.
    await user.click(screen.getByRole("button", { name: "Runtime", pressed: false }));
    expect(table.getByText("Post Slack message")).toBeInTheDocument();
    expect(table.queryByText("Restart deploy")).not.toBeInTheDocument();
    expect(table.queryByText("List issues")).not.toBeInTheDocument();
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

  it("renders each repo capability with ONE Decision pill and no separate When button (FIR-2706 same-design)", async () => {
    const user = userEvent.setup();
    renderRepoTable();
    const group = await screen.findByTestId(`repo-group-${REPO_URL}`);
    await user.click(within(group).getByRole("button", { expanded: false }));
    const checkout = screen.getByTestId(`repo-cap-repo.checkout-${REPO_URL}`);
    // Exactly one control on the bar: the Decision pill. The old standalone When
    // button (condition-control-*) is gone — When now lives inside the pill.
    expect(within(checkout).getByLabelText(/^Decision:/)).toBeInTheDocument();
    expect(within(checkout).queryByTestId("condition-control-repo.checkout")).not.toBeInTheDocument();
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
    await user.click(within(checkout).getByTestId("catalog-decision-repo.checkout-ask"));
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
    // The When editor now lives inside the row's single Decision pill popover
    // (FIR-2706 — one control per row), so open the pill to reach it.
    await user.click(within(push).getByLabelText(/^Decision:/));
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
    // The When editor now lives inside the row's single Decision pill popover
    // (FIR-2706 — one control per row), so open the pill to reach it.
    await user.click(within(push).getByLabelText(/^Decision:/));
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

  it("uses a group picker for Manage group permissions and writes group_id", async () => {
    const user = userEvent.setup();
    mockListCerebroGroups.mockResolvedValue([
      {
        id: "group-finance",
        workspace_id: "ws-1",
        name: "Finance",
        description: null,
        created_by: null,
        created_at: "2026-07-07T00:00:00Z",
        updated_at: "2026-07-07T00:00:00Z",
      },
      {
        id: "group-support",
        workspace_id: "ws-1",
        name: "Customer service",
        description: null,
        created_by: null,
        created_at: "2026-07-07T00:00:00Z",
        updated_at: "2026-07-07T00:00:00Z",
      },
    ]);
    mockCerebroRequest.mockResolvedValue({
      tools: [
        {
          tool_key: "manage_group_overrides",
          title: "Manage group permissions",
          category: "Permissions",
          source: "platform",
          enforced_conditions: ["arg", "cel"],
          layers: { runtime: null, agent: "allow", group: null, user: null },
          conditions: { workspace: null, runtime: null, agent: null, user: null },
          effective: { setting: "allow", decided_by: "agent", capped_by: "", reason: "" },
        },
      ],
    });

    renderCondTable();
    const row = await screen.findByTestId("tool-row-manage_group_overrides");
    await user.click(within(row).getByTestId("condition-control-manage_group_overrides"));
    const editor = within(await screen.findByTestId("condition-editor-manage_group_overrides"));

    expect(await editor.findByLabelText("Search groups")).toBeInTheDocument();
    expect(editor.getByText("Groups")).toBeInTheDocument();
    await user.click(await editor.findByRole("button", { name: /Finance/ }));
    await user.click(editor.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const put = findPutCalls().at(-1);
      expect(put).toBeTruthy();
      const body = JSON.parse((put![1] as RequestInit).body as string);
      expect(body).toMatchObject({
        tool_key: "manage_group_overrides",
        layer: "agent",
        subject_id: "agent-1",
        setting: "allow",
      });
      expect(body.condition.arg_allowlist).toEqual([
        { arg: "group_id", values: ["group-finance"] },
      ]);
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

describe("ToolPolicyTable Permissions/Connections tab split (FIR-2281, FIR-2706)", () => {
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
      {
        tool_key: "connection:slack",
        title: "Slack workspace",
        category: "Connections",
        source: "connection",
        layers: { runtime: null, agent: null, group: null, user: null },
        effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
      },
      {
        tool_key: "repo.read",
        resource_pattern: "github.com/firtal-group/repo-a",
        title: "Read code",
        category: "repo",
        source: "repo",
        layers: { runtime: null, agent: null, group: null, user: null },
        effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
      },
    ],
  };

  function renderTab(tabFilter: "permissions" | "repos" | "connections") {
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

  it("opens permission details only while the detail feature flag is enabled", async () => {
    const onOpenPermission = vi.fn();
    mockCerebroRequest.mockResolvedValue(SPLIT);
    mockUseFeatureFlag.mockImplementation(
      (key: string) =>
        key === "cerebro_tool_policy" || key === "cerebro_permission_detail",
    );
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { rerender } = render(
      <QueryClientProvider client={qc}>
        <ToolPolicyTable
          wsId="ws-1"
          view="workspace"
          subjectId="ws-1"
          tabFilter="permissions"
          onOpenPermission={onOpenPermission}
        />
      </QueryClientProvider>,
    );

    await userEvent.click(
      await screen.findByRole("button", { name: "Open details for Bash" }),
    );
    expect(onOpenPermission).toHaveBeenCalledWith("tools:Bash");

    mockUseFeatureFlag.mockImplementation(
      (key: string) => key === "cerebro_tool_policy",
    );
    rerender(
      <QueryClientProvider client={qc}>
        <ToolPolicyTable
          wsId="ws-1"
          view="workspace"
          subjectId="ws-1"
          tabFilter="permissions"
          onOpenPermission={onOpenPermission}
        />
      </QueryClientProvider>,
    );

    expect(
      screen.queryByRole("button", { name: "Open details for Bash" }),
    ).not.toBeInTheDocument();
  });

  it("Permissions tab shows every flat capability (Multica + Runtime), not connections", async () => {
    mockCerebroRequest.mockResolvedValue(SPLIT);
    renderTab("permissions");
    const table = await tableBody();
    // Runtime-reported tools, runtime-admin actions, and generic Multica tools all
    // live on the one Permissions tab now — the old Runtime/Multica tabs are gone.
    expect(table.getByText("Bash")).toBeInTheDocument();
    expect(table.getByText("Manage runtime")).toBeInTheDocument();
    expect(table.getByText("Add comment")).toBeInTheDocument();
    // The Type column distinguishes them in-row instead of across tabs.
    expect(table.getAllByText("Runtime").length).toBeGreaterThan(0);
    expect(table.getAllByText("Multica").length).toBeGreaterThan(0);
    // A connection is a resource, not a flat permission — never on this tab.
    expect(table.queryByText("Slack workspace")).not.toBeInTheDocument();
    // Neither is a repo.
    expect(screen.queryByTestId("repo-policy-section")).not.toBeInTheDocument();
  });

  it("Connections tab shows connections and excludes the flat permissions and repos", async () => {
    mockCerebroRequest.mockResolvedValue(SPLIT);
    renderTab("connections");
    const table = await tableBody();
    expect(table.getByText("Slack workspace")).toBeInTheDocument();
    expect(table.getAllByText("Connections").length).toBeGreaterThan(0);
    expect(table.queryByText("Bash")).not.toBeInTheDocument();
    expect(table.queryByText("Add comment")).not.toBeInTheDocument();
    expect(screen.queryByTestId("repo-policy-section")).not.toBeInTheDocument();
  });

  it("Repos tab shows the repo group and excludes connections and flat permissions", async () => {
    mockCerebroRequest.mockResolvedValue(SPLIT);
    renderTab("repos");
    expect(
      await screen.findByTestId("repo-group-github.com/firtal-group/repo-a"),
    ).toBeInTheDocument();
    expect(screen.queryByText("Slack workspace")).not.toBeInTheDocument();
    expect(screen.queryByText("Bash")).not.toBeInTheDocument();
    expect(screen.queryByText("Add comment")).not.toBeInTheDocument();
  });

  // FIR-2640 review — on mobile the connection row's controls (the Configure
  // button + the Decision pill) must stack vertically, Decision on top and
  // Configure UNDER it (flex-col-reverse), so the wide "Configure" button stops
  // stealing the row's width and the capability name stays readable.
  it("stacks the mobile card controls so Configure sits under Allow", async () => {
    mockCerebroRequest.mockResolvedValue(SPLIT);
    renderTab("connections");
    await tableBody();
    const card = document.querySelector(
      '[data-testid="tool-card-connection:slack"]',
    );
    expect(card).not.toBeNull();
    // The right-hand control column stacks (reverse so the Decision pill — the
    // last child — renders on top and the Configure button below it).
    expect(card?.querySelector(".flex-col-reverse")).not.toBeNull();
  });
});

// FIR-1479 + FIR-2281: credentials are a resource type, rendered as per-box rows
// inside the Connections tab (FIR-2706 split repos out into their own tab, but
// credentials stayed alongside connections). Each row carries a resource_pattern
// ("cerebro-credential:<id>"), so a decision MUST be written scoped to that box —
// the prior inline rendering dropped the scope and the toggle silently no-op'd.
describe("ToolPolicyTable — Credentials in the Connections tab (FIR-1479)", () => {
  // A single-action box (Agent Vault boxes expose only "Use secret") renders as a
  // plain permission row — resource agentvault-vault:<name>, one reveal capability.
  const vaultRow = (
    name: string,
    setting: "allow" | "ask" | "deny" = "deny",
  ) => ({
    tool_key: "credential.reveal",
    resource_pattern: `agentvault-vault:${name}`,
    title: "Use secret",
    category: name,
    source: "credential",
    layers: { workspace: null, runtime: null, agent: null, group: null, user: null },
    effective: { setting, decided_by: "", capped_by: "", reason: "" },
  });

  // A multi-action box (a cerebro-stored credential) renders as a foldable group:
  // all five capability rows under cerebro-credential:<id>.
  const CRED_CAPS = [
    ["credential.reveal", "Use secret"],
    ["credential.read_redacted", "Read redacted"],
    ["credential.rotate", "Rotate"],
    ["credential.revoke", "Revoke"],
    ["credential.attach", "Attach to resource"],
  ] as const;
  const credBox = (id: string, name: string) =>
    CRED_CAPS.map(([tool_key, title]) => ({
      tool_key,
      resource_pattern: `cerebro-credential:${id}`,
      title,
      category: name,
      source: "credential",
      layers: { workspace: null, runtime: null, agent: null, group: null, user: null },
      effective: { setting: "deny", decided_by: "", capped_by: "", reason: "" },
    }));

  const BUILTIN_ROW = {
    tool_key: "list_issues",
    resource_pattern: "",
    title: "List issues",
    category: "Built-in tools",
    source: "builtin",
    layers: { workspace: null, runtime: null, agent: null, group: null, user: null },
    effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
  };

  const VAULT_TABLE = {
    tools: [BUILTIN_ROW, vaultRow("bigquery"), vaultRow("cloudflare")],
  };
  const FOLDABLE_TABLE = {
    tools: [BUILTIN_ROW, ...credBox("box-1", "stored-key")],
  };

  function renderCredentials() {
    // Credentials only render once the workspace turns the feature on (FIR-1479).
    mockUseFeatureFlag.mockReturnValue(true);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return render(
      <QueryClientProvider client={qc}>
        <ToolPolicyTable
          wsId="ws-1"
          view="agent"
          subjectId="agent-1"
          runtimeId="rt-1"
          userId="user-1"
          tabFilter="connections"
        />
      </QueryClientProvider>,
    );
  }

  it("shows one row per Agent Vault box (by box name) and excludes non-credential tools", async () => {
    mockCerebroRequest.mockResolvedValue(VAULT_TABLE);
    renderCredentials();
    expect(await screen.findByTestId("credential-group-agentvault-vault:bigquery")).toBeInTheDocument();
    expect(screen.getByText("bigquery")).toBeInTheDocument();
    expect(screen.getByText("cloudflare")).toBeInTheDocument();
    // A builtin tool is not a credential → never appears on the credentials tab.
    expect(screen.queryByText("List issues")).not.toBeInTheDocument();
  });

  it("sets a single-action vault box decision scoped to its resource_pattern (plain row)", async () => {
    const user = userEvent.setup();
    mockCerebroRequest.mockResolvedValue(VAULT_TABLE);
    renderCredentials();
    // A vault box (only "Use secret") is a plain row — no fold, one decision.
    const box = await screen.findByTestId("credential-group-agentvault-vault:bigquery");
    await user.click(within(box).getByLabelText(/^Decision:/));
    await user.click(within(box).getByTestId("catalog-decision-credential.reveal-allow"));

    await waitFor(() => {
      const put = findPutCalls().at(-1);
      expect(put).toBeTruthy();
      expect(JSON.parse((put![1] as RequestInit).body as string)).toMatchObject({
        tool_key: "credential.reveal",
        setting: "allow",
        resource_pattern: "agentvault-vault:bigquery",
      });
    });
  });

  it("renders a multi-action credential as a foldable group; expand to author one action", async () => {
    const user = userEvent.setup();
    mockCerebroRequest.mockResolvedValue(FOLDABLE_TABLE);
    renderCredentials();
    const group = await screen.findByTestId("credential-group-cerebro-credential:box-1");
    // Foldable (5 actions): open via the box-named button, then set one capability.
    await user.click(within(group).getByRole("button", { name: "stored-key" }));
    const cap = await within(group).findByTestId(
      "credential-cap-credential.rotate-cerebro-credential:box-1",
    );
    await user.click(within(cap).getByLabelText(/^Decision:/));
    await user.click(within(cap).getByTestId("catalog-decision-credential.rotate-allow"));

    await waitFor(() => {
      const put = findPutCalls().at(-1);
      expect(put).toBeTruthy();
      expect(JSON.parse((put![1] as RequestInit).body as string)).toMatchObject({
        tool_key: "credential.rotate",
        setting: "allow",
        resource_pattern: "cerebro-credential:box-1",
      });
    });
  });

  it("cascades a multi-action box via the group header, scoped to the box", async () => {
    const user = userEvent.setup();
    mockCerebroRequest.mockResolvedValue(FOLDABLE_TABLE);
    renderCredentials();
    const group = await screen.findByTestId("credential-group-cerebro-credential:box-1");
    await user.click(within(group).getByLabelText(/^Credential decision:/));
    await user.click(within(group).getByRole("menuitem", { name: /^Allow/ }));

    await waitFor(() => {
      const cascaded = findPutCalls().some((c) => {
        const body = JSON.parse((c[1] as RequestInit).body as string);
        return body.resource_pattern === "cerebro-credential:box-1" && body.setting === "allow";
      });
      expect(cascaded).toBe(true);
    });
  });

  it("shows nothing on the Connections tab when only flat permissions exist", async () => {
    mockCerebroRequest.mockResolvedValue({ tools: [BUILTIN_ROW] });
    renderCredentials();
    // A builtin tool is a permission, not a resource → the Connections tab is empty.
    expect(await screen.findByText(/No tools match these filters/)).toBeInTheDocument();
    expect(screen.queryByText("List issues")).not.toBeInTheDocument();
  });
});

describe("ToolPolicyTabs — three tabs (FIR-2281, FIR-2706)", () => {
  function renderTabs() {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return render(
      <QueryClientProvider client={qc}>
        <ToolPolicyTabs wsId="ws-1" view="agent" subjectId="agent-1" runtimeId="rt-1" userId="user-1" />
      </QueryClientProvider>,
    );
  }

  it("renders exactly the Permissions, Repos and Connections tabs", async () => {
    renderTabs();
    expect(await screen.findByRole("tab", { name: "Permissions" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Repos" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Connections" })).toBeInTheDocument();
    // The old five tabs (and the FIR-2281 combined "Resources" tab) are gone.
    expect(screen.queryByRole("tab", { name: "Multica" })).not.toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Credentials" })).not.toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Resources" })).not.toBeInTheDocument();
  });
});

describe("ToolPolicyTable — redesigned capability cards (FIR-2670 #8, FIR-2706)", () => {
  function renderRedesigned(view: "agent" | "runtime" = "agent") {
    // The capability catalog is now gated on the permissions flag itself
    // (cerebro_tool_policy) rather than the agent-page preview (FIR-2706). Only
    // that flag is on; every other flag stays off.
    mockUseFeatureFlag.mockImplementation(
      (key: string) => key === "cerebro_tool_policy",
    );
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return render(
      <QueryClientProvider client={qc}>
        <ToolPolicyTable wsId="ws-1" view={view} subjectId="agent-1" runtimeId="rt-1" userId="user-1" />
      </QueryClientProvider>,
    );
  }

  it("renders grouped capability cards instead of the flat table when the flag is on", async () => {
    renderRedesigned("agent");
    // The capability-card container replaces the <table>.
    expect(await screen.findByTestId("capability-catalog")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    // Every tool still shows, just grouped into cards.
    expect(screen.getByText("Post Slack message")).toBeInTheDocument();
    expect(screen.getByText("Restart deploy")).toBeInTheDocument();
    expect(screen.getByText("List issues")).toBeInTheDocument();
  });

  it("keeps the write path: picking a decision still writes the agent layer", async () => {
    const user = userEvent.setup();
    renderRedesigned("agent");
    const slackRow = await screen.findByTestId("tool-card-slack.post_message");
    // FIR-2706 — the row now carries a SINGLE combined control: the Decision pill
    // opens a popover whose choices are buttons (and which also holds When), not a
    // dropdown menu + a separate When button on the bar.
    await user.click(within(slackRow).getByLabelText(/^Decision:/));
    await user.click(within(slackRow).getByTestId("catalog-decision-slack.post_message-ask"));
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

  it("gives the row a single combined control — no separate When button on the bar", async () => {
    const user = userEvent.setup();
    renderRedesigned("agent");
    const slackRow = await screen.findByTestId("tool-card-slack.post_message");
    // The row bar carries the single Decision toggle and NOT a standalone When
    // button — that was the mobile-crowding source (FIR-2706).
    expect(within(slackRow).getByLabelText(/^Decision:/)).toBeInTheDocument();
    expect(
      within(slackRow).queryByTestId("condition-control-slack.post_message"),
    ).not.toBeInTheDocument();
    // Opening the toggle reveals the decision choices as buttons inside one popover
    // (the same popover that also holds the When editor).
    await user.click(within(slackRow).getByLabelText(/^Decision:/));
    expect(
      within(slackRow).getByTestId("catalog-decision-slack.post_message-deny"),
    ).toBeInTheDocument();
  });
});

// FIR-2706 follow-up — a write the server accepts but a higher layer overrides
// used to fail silently: the pill snapped back with only a hover tooltip.
describe("silent-failure feedback (FIR-2706 follow-up)", () => {
  const NO_LAYERS = { workspace: null, runtime: null, agent: null, group: null, user: null, system: null };
  const NO_CONDITIONS = { workspace: null, runtime: null, agent: null, user: null, system: null };
  function cappedRow(
    effective: Partial<ToolPolicyRow["effective"]> = {},
    extra: Partial<ToolPolicyRow> = {},
  ): ToolPolicyRow {
    return {
      tool_key: "slack.post_message",
      resource_pattern: "",
      title: "Post Slack message",
      category: "MCP · Slack",
      source: "scan",
      managed_externally: false,
      layers: { ...NO_LAYERS },
      conditions: { ...NO_CONDITIONS },
      effective: {
        setting: "deny",
        decided_by: "user",
        capped_by: "user",
        reason: "Capped by user",
        openable: false,
        ...effective,
      },
      capped_by_groups: [],
      ...extra,
    };
  }

  beforeEach(() => {
    mockToast.warning.mockReset();
    mockToast.info.mockReset();
  });

  describe("futileWriteWarning", () => {
    it("names the blocking layer and where to change it when a looser write cannot take effect", () => {
      const msg = futileWriteWarning(cappedRow(), "agent", "allow");
      expect(msg).toContain('"Allow" was saved on the Agent layer');
      expect(msg).toContain('the decision stays "Deny"');
      expect(msg).toContain("User blocks it");
      expect(msg).toContain("ceiling");
    });

    it("names the blocking group and its owner", () => {
      const msg = futileWriteWarning(
        cappedRow(
          { decided_by: "group", capped_by: "group" },
          { capped_by_groups: [{ name: "Support", owner: "Jesper Hvejsel" }] },
        ),
        "agent",
        "ask",
      );
      expect(msg).toContain("group Support (owner: Jesper Hvejsel) blocks it");
      expect(msg).toContain("Settings → Groups");
    });

    it("is silent when the write tightens — deny always bites", () => {
      expect(futileWriteWarning(cappedRow({ setting: "ask" }), "agent", "deny")).toBeNull();
    });

    it("is silent when nothing locks the row", () => {
      expect(
        futileWriteWarning(
          cappedRow({ setting: "allow", decided_by: "", capped_by: "", reason: "" }),
          "agent",
          "allow",
        ),
      ).toBeNull();
    });

    it("is silent when the choice matches the effective verdict's restrictiveness", () => {
      // Workspace decided Ask; writing Ask on the agent layer is consistent, not futile.
      expect(
        futileWriteWarning(
          cappedRow({ setting: "ask", decided_by: "workspace", capped_by: "" }),
          "agent",
          "ask",
        ),
      ).toBeNull();
    });

    // FIR-3091 punkt 1: a Group Allow on an openable workspace Deny actually opens
    // it, so there must be NO false "no effect" warning.
    it("is silent when a Group opens an openable workspace Deny", () => {
      const row = cappedRow(
        { setting: "deny", decided_by: "workspace", capped_by: "", openable: true },
        {
          layers: {
            workspace: "deny",
            runtime: null,
            agent: null,
            group: null,
            user: null,
            system: null,
          },
        },
      );
      expect(futileWriteWarning(row, "group", "allow")).toBeNull();
    });

    // A workspace Disable is a real, unopenable floor — the warning must still fire.
    it("still warns when the workspace layer is Disable", () => {
      const row = cappedRow(
        { setting: "deny", decided_by: "workspace", capped_by: "", openable: true },
        {
          layers: {
            workspace: "disable",
            runtime: null,
            agent: null,
            group: null,
            user: null,
            system: null,
          },
        },
      );
      const msg = futileWriteWarning(row, "group", "allow");
      expect(msg).not.toBeNull();
    });
  });

  it("shows the lock explanation inside the decision menu, not only as a hover tooltip", async () => {
    renderTable("agent");
    const slackRow = await screen.findByTestId("tool-row-slack.post_message");
    // The dropdown mock renders content inline, so the visible in-menu banner
    // (Case 1 attribution: futile local override beneath the user cap) is
    // queryable directly.
    expect(
      within(slackRow).getByText(/Your Agent setting has no effect — blocked by User/),
    ).toBeInTheDocument();
  });

  it("toasts an explanation when an accepted write is overridden by a higher layer", async () => {
    const user = userEvent.setup();
    renderTable("agent");
    const slackRow = await screen.findByTestId("tool-row-slack.post_message");
    await user.click(within(slackRow).getByLabelText(/^Decision:/));
    await user.click(within(slackRow).getByRole("menuitem", { name: "Allow" }));

    await waitFor(() => {
      expect(mockToast.warning).toHaveBeenCalledWith(
        expect.stringContaining("has no effect"),
        expect.anything(),
      );
    });
    expect(mockToast.warning).toHaveBeenCalledWith(
      expect.stringContaining("because User blocks it"),
      expect.anything(),
    );
  });

  it("does not toast when the write takes effect", async () => {
    const user = userEvent.setup();
    renderTable("agent");
    // list_issues is unlocked (effective allow, nothing decides it) — writing
    // Deny tightens and must produce no warning.
    const row = await screen.findByTestId("tool-row-list_issues");
    await user.click(within(row).getByLabelText(/^Decision:/));
    await user.click(within(row).getByRole("menuitem", { name: "Deny" }));

    await waitFor(() => {
      expect(findPutCalls().length).toBeGreaterThan(0);
    });
    expect(mockToast.warning).not.toHaveBeenCalled();
  });
});
