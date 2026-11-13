// @vitest-environment jsdom

import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactElement } from "react";
import { api } from "@multica/core/api";
import type { ContextReceipt } from "@multica/core/types/agent";
import { renderWithI18n } from "../../test/i18n";
import {
  ContextReceiptButton,
  ContextReceiptView,
  loadTone,
  receiptWarnings,
  topLevelSources,
} from "./context-receipt";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("@multica/core/api", () => ({
  api: { getTaskContextReceipt: vi.fn() },
}));

afterEach(() => {
  cleanup();
  vi.mocked(api.getTaskContextReceipt).mockReset();
});

const receipt: ContextReceipt = {
  schema_version: 1,
  runtime: "kiro",
  runtime_version: "2.34.0",
  model: "claude-opus",
  prompt_bytes: 2640,
  brief_bytes: 19456,
  brief_delivery: "file",
  resumed_session: true,
  mcp_servers: [],
  sources: [
    {
      kind: "runtime_brief",
      bytes: 19456,
      sha256: "cdc096ed0000",
      delivery: "file",
      path: "AGENTS.md",
      load: "verified_other_version",
      verified_version: "2.13.0",
    },
    {
      kind: "agent_instructions",
      bytes: 810,
      sha256: "4b1e9c000000",
      delivery: "embedded",
      parent: "runtime_brief",
      load: "verified_other_version",
    },
    { kind: "task_prompt", bytes: 2640, delivery: "prompt", load: "delivered" },
    {
      kind: "skill",
      name: "pr-review",
      bytes: 19412,
      delivery: "file",
      path: ".kiro/skills/pr-review/SKILL.md",
      load: "unverified",
    },
  ],
};

function withClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe("receipt helpers", () => {
  it("grades load states", () => {
    expect(loadTone("delivered")).toBe("ok");
    expect(loadTone("verified")).toBe("ok");
    expect(loadTone("verified_other_version")).toBe("warn");
    expect(loadTone("unverified")).toBe("warn");
    expect(loadTone("not_delivered")).toBe("warn");
    expect(loadTone("something_new")).toBe("muted");
  });

  it("warns only on top-level sources", () => {
    expect(topLevelSources(receipt).map((s) => s.kind)).toEqual(["runtime_brief", "task_prompt", "skill"]);
    // The embedded instructions share the brief's state; warning on both
    // would say the same thing twice.
    expect(receiptWarnings(receipt).map((s) => s.kind)).toEqual(["runtime_brief", "skill"]);
  });
});

describe("ContextReceiptView", () => {
  it("lists every source with its size, delivery and load state", () => {
    renderWithI18n(<ContextReceiptView receipt={receipt} />);
    const view = screen.getByTestId("context-receipt");

    // Embedded rows are not counted again in the total.
    expect(within(view).getByText("3 sources · 41 KB")).toBeTruthy();
    expect(within(view).getByText("Kiro 2.34.0")).toBeTruthy();
    expect(within(view).getByText(/Resumed a prior session/)).toBeTruthy();
    expect(within(view).getByText("None")).toBeTruthy();

    const rows = within(view).getAllByRole("row").slice(1);
    expect(rows).toHaveLength(4);
    expect(within(rows[0]!).getByText("Runtime brief")).toBeTruthy();
    expect(within(rows[0]!).getByText("AGENTS.md")).toBeTruthy();
    expect(within(rows[0]!).getByText("Verified on 2.13.0 only")).toBeTruthy();
    expect(within(rows[1]!).getByText(/Agent instructions/)).toBeTruthy();
    expect(within(rows[1]!).getByText("Same as runtime brief")).toBeTruthy();
    expect(within(rows[2]!).getByText("Turn input")).toBeTruthy();
    expect(within(rows[2]!).getByText("Delivered")).toBeTruthy();
    expect(within(rows[3]!).getByText("Skill: pr-review")).toBeTruthy();
    expect(within(rows[3]!).getByText("Not verified")).toBeTruthy();

    // The warning box names exactly the sources we cannot vouch for.
    expect(within(view).getByText("Runtime brief: Verified on 2.13.0 only")).toBeTruthy();
    expect(within(view).getByText("Skill: pr-review: Not verified")).toBeTruthy();
  });

  it("shows no warning box when every source is confirmed", () => {
    const clean: ContextReceipt = {
      ...receipt,
      resumed_session: false,
      mcp_servers: ["github"],
      sources: [
        { kind: "runtime_brief", bytes: 100, delivery: "inline", load: "delivered" },
        { kind: "task_prompt", bytes: 10, delivery: "prompt", load: "delivered" },
      ],
    };
    renderWithI18n(<ContextReceiptView receipt={clean} />);
    expect(screen.getByText("System prompt")).toBeTruthy();
    expect(screen.getByText("New session")).toBeTruthy();
    expect(screen.getByText("github")).toBeTruthy();
    expect(screen.queryByText(/: Not verified/)).toBeNull();
  });

  it("renders in Chinese", () => {
    renderWithI18n(<ContextReceiptView receipt={receipt} />, { locale: "zh-Hans" });
    expect(screen.getByText("上下文回执")).toBeTruthy();
    expect(screen.getByText("技能：pr-review")).toBeTruthy();
    expect(screen.getAllByText("仅在 2.13.0 验证过").length).toBeGreaterThan(0);
  });
});

describe("ContextReceiptButton", () => {
  it("renders nothing for a run without a receipt", async () => {
    vi.mocked(api.getTaskContextReceipt).mockResolvedValue({ receipt: null });
    const { container } = withClient(<ContextReceiptButton taskId="task-1" open />);
    await waitFor(() => expect(api.getTaskContextReceipt).toHaveBeenCalledWith("task-1"));
    expect(container.querySelector("button")).toBeNull();
  });

  it("does not fetch while the dialog is closed", () => {
    withClient(<ContextReceiptButton taskId="task-1" open={false} />);
    expect(api.getTaskContextReceipt).not.toHaveBeenCalled();
  });

  it("opens the receipt from the header button", async () => {
    vi.mocked(api.getTaskContextReceipt).mockResolvedValue({ receipt, recorded_at: "2026-11-11T11:29:00Z" });
    withClient(<ContextReceiptButton taskId="task-1" open />);
    const button = await screen.findByRole("button", { name: "Context received" });
    await userEvent.click(button);
    expect(await screen.findByTestId("context-receipt")).toBeTruthy();
  });
});
