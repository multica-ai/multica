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

import {
  SimpleToolPolicyTable,
  classifyTool,
  groupRows,
} from "./simple-tool-policy-table";
import type { ToolPolicyRow } from "../core";

const row = (over: Partial<ToolPolicyRow> = {}): ToolPolicyRow => ({
  tool_key: "read_files",
  title: "Læs filer",
  category: "tools",
  source: "scan",
  layers: { runtime: null, agent: null, group: null, user: null },
  effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
  ...over,
});

const TABLE = {
  tools: [
    row({ tool_key: "read_files", title: "Læs filer", category: "tools" }),
    row({
      tool_key: "bash",
      title: "Kør kommandoer",
      category: "tools",
      layers: { runtime: null, agent: "allow", group: null, user: null },
    }),
    row({
      tool_key: "web_fetch",
      title: "Hent fra nettet",
      category: "Web",
      layers: { runtime: null, agent: "ask", group: null, user: null },
      effective: { setting: "ask", decided_by: "agent", capped_by: "", reason: "" },
    }),
    row({
      tool_key: "delete_shared",
      title: "Slet delte ressourcer",
      category: "tools",
      layers: { runtime: null, agent: "deny", group: null, user: null },
      effective: { setting: "deny", decided_by: "agent", capped_by: "", reason: "" },
    }),
  ],
};

function renderTable() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <SimpleToolPolicyTable
        wsId="ws-1"
        agentId="agent-1"
        runtimeId="rt-1"
        userId="user-1"
      />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  mockCerebroRequest.mockReset();
  mockCerebroRequest.mockResolvedValue(TABLE);
});

describe("classifyTool", () => {
  it("buckets tools into the four sections by keyword, destructive first", () => {
    expect(classifyTool(row({ tool_key: "read_issue", title: "Læs issue" }))).toBe(
      "read",
    );
    expect(classifyTool(row({ tool_key: "bash", title: "Kør kommandoer" }))).toBe(
      "execute",
    );
    expect(classifyTool(row({ tool_key: "web_fetch", title: "Hent" }))).toBe(
      "fetch",
    );
    expect(
      classifyTool(row({ tool_key: "git_force_push", title: "Force-push" })),
    ).toBe("destructive");
    // A delete tool whose title also says "fetch" still lands in destructive.
    expect(
      classifyTool(row({ tool_key: "delete_after_fetch", title: "fetch" })),
    ).toBe("destructive");
  });
});

describe("groupRows", () => {
  it("returns sections in display order and drops empty ones", () => {
    const sections = groupRows(TABLE.tools);
    expect(sections.map((s) => s.def.key)).toEqual([
      "read",
      "execute",
      "fetch",
      "destructive",
    ]);
    expect(sections.find((s) => s.def.key === "destructive")!.def.danger).toBe(
      true,
    );
  });
});

describe("SimpleToolPolicyTable", () => {
  it("renders the four Danish section headers and groups rows under them", async () => {
    renderTable();
    expect(await screen.findByText("Læs filer")).toBeInTheDocument();
    expect(screen.getByText("Læs")).toBeInTheDocument();
    expect(screen.getByText("Udfør")).toBeInTheDocument();
    expect(screen.getByText("Hent")).toBeInTheDocument();
    expect(screen.getByText("Destruktivt")).toBeInTheDocument();
  });

  it("writes the chosen verdict to the agent layer when a toggle is clicked", async () => {
    const user = userEvent.setup();
    renderTable();
    await screen.findByText("Læs filer");
    const readRow = screen.getByTestId("tool-row-read_files");
    await user.click(within(readRow).getByRole("button", { name: "Bloker" }));

    await waitFor(() => {
      const putCall = mockCerebroRequest.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === "PUT",
      );
      expect(putCall).toBeTruthy();
      expect(JSON.parse((putCall![1] as RequestInit).body as string)).toEqual({
        tool_key: "read_files",
        layer: "agent",
        subject_id: "agent-1",
        setting: "deny",
      });
    });
  });

  it("highlights the agent override when set, else the inherited Effective verdict", async () => {
    renderTable();
    await screen.findByText("Kør kommandoer");
    // bash has an explicit agent="allow" override.
    const bashRow = screen.getByTestId("tool-row-bash");
    expect(within(bashRow).getByRole("button", { name: "Tillad" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    // read_files has no override → falls back to Effective allow.
    const readRow = screen.getByTestId("tool-row-read_files");
    expect(within(readRow).getByRole("button", { name: "Tillad" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("fails closed: an unknown Effective verdict with no override highlights Bloker, never Tillad", async () => {
    mockCerebroRequest.mockResolvedValue({
      tools: [
        {
          tool_key: "mystery_tool",
          title: "Ukendt evne",
          category: "tools",
          source: "scan",
          layers: { runtime: null, agent: null, group: null, user: null },
          // Drifted/unknown effective — the core schema downgrades this to deny.
          effective: { setting: "wat", decided_by: "", capped_by: "", reason: "" },
        },
      ],
    });
    renderTable();
    const mysteryRow = await screen.findByTestId("tool-row-mystery_tool");
    expect(
      within(mysteryRow).getByRole("button", { name: "Bloker" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(
      within(mysteryRow).getByRole("button", { name: "Tillad" }),
    ).toHaveAttribute("aria-pressed", "false");
  });
});
