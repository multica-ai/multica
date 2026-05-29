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
});
