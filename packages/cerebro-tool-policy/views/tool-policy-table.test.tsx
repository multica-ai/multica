import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

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

import { ToolPolicyTable } from "./tool-policy-table";

const TABLE = {
  tools: [
    {
      tool_key: "slack.post_message",
      title: "Post Slack message",
      category: "Slack",
      source: "scan",
      layers: { runtime: "allow", agent: "allow", group: null, user: "deny" },
      effective: { setting: "deny", decided_by: "user", capped_by: "user", reason: "Capped by user" },
    },
    {
      tool_key: "deploy_restart",
      title: "Restart deploy",
      category: "Deploy",
      source: "builtin",
      layers: { runtime: null, agent: "ask", group: null, user: null },
      effective: { setting: "ask", decided_by: "agent", capped_by: "", reason: "Ask by agent" },
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

beforeEach(() => {
  mockCerebroRequest.mockReset();
  mockCerebroRequest.mockResolvedValue(TABLE);
});

describe("ToolPolicyTable", () => {
  it("renders one row per tool with the Effective verdict and the capped-by reason", async () => {
    renderTable("agent");
    expect(await screen.findByText("Post Slack message")).toBeInTheDocument();
    expect(screen.getByText("Restart deploy")).toBeInTheDocument();
    // The capped verdict explains itself in plain language.
    expect(screen.getByText("Capped by user")).toBeInTheDocument();
    // Agent view shows the "This agent" column.
    expect(screen.getByText("This agent")).toBeInTheDocument();
  });

  it("writes the chosen setting to the agent layer when a control is clicked", async () => {
    const user = userEvent.setup();
    renderTable("agent");
    const slackRow = (await screen.findByText("slack.post_message")).closest("tr")!;
    // The agent column is the editable segmented control; pick Deny.
    const denyButtons = within(slackRow).getAllByRole("button", { name: "Deny" });
    await user.click(denyButtons[denyButtons.length - 1]!);

    await waitFor(() => {
      const putCall = mockCerebroRequest.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === "PUT",
      );
      expect(putCall).toBeTruthy();
      expect(JSON.parse((putCall![1] as RequestInit).body as string)).toEqual({
        tool_key: "slack.post_message",
        layer: "agent",
        subject_id: "agent-1",
        setting: "deny",
      });
    });
  });

  it("the Needs approval tab keeps only tools whose effective verdict is ask", async () => {
    const user = userEvent.setup();
    renderTable("agent");
    await screen.findByText("Post Slack message");
    await user.click(screen.getByRole("tab", { name: "Needs approval" }));
    expect(screen.getByText("Restart deploy")).toBeInTheDocument();
    expect(screen.queryByText("Post Slack message")).not.toBeInTheDocument();
  });

  it("runtime view writes to the runtime layer and hides the This agent column", async () => {
    const user = userEvent.setup();
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ToolPolicyTable wsId="ws-1" view="runtime" subjectId="rt-1" />
      </QueryClientProvider>,
    );
    await screen.findByText("Post Slack message");
    expect(screen.queryByText("This agent")).not.toBeInTheDocument();

    const slackRow = screen.getByText("slack.post_message").closest("tr")!;
    const askButtons = within(slackRow).getAllByRole("button", { name: "Ask" });
    await user.click(askButtons[0]!);
    await waitFor(() => {
      const putCall = mockCerebroRequest.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === "PUT",
      );
      expect(JSON.parse((putCall![1] as RequestInit).body as string)).toMatchObject({
        layer: "runtime",
        subject_id: "rt-1",
        setting: "ask",
      });
    });
  });
});
