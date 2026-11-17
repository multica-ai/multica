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
  receiptDeliveryBytes,
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

describe("delivery byte categories", () => {
  it("separates direct input and files without counting embedded or unavailable channels", () => {
    expect(receiptDeliveryBytes({ ...receipt, sources: [
      { kind: "runtime_brief", bytes: 100, delivery: "inline", load: "delivered" },
      { kind: "agent_instructions", bytes: 70, delivery: "embedded", parent: "runtime_brief", load: "delivered" },
      { kind: "task_prompt", bytes: 10, delivery: "prompt", load: "delivered" },
      { kind: "skill", bytes: 1500, delivery: "file", load: "verified" },
      { kind: "skill", bytes: 2500, delivery: "file", load: "unverified" },
      { kind: "runtime_brief", bytes: 200, delivery: "file", path: "AGENTS.md", load: "unverified" },
      { kind: "existing_instructions", bytes: 300, delivery: "file", path: "AGENTS.md", load: "verified" },
      { kind: "skill", bytes: 20, delivery: "inline", load: "delivered" },
      { kind: "future", bytes: 40, delivery: "file", load: "unknown" },
      { kind: "skill", bytes: 9999, delivery: "none", load: "not_delivered" },
      { kind: "future", bytes: 9999, delivery: "new_channel", load: "unknown" },
    ] })).toEqual({ direct: 130, files: 4540, instructionFiles: 500, skillPackages: 4000, otherFiles: 40 });
    expect(receiptDeliveryBytes({ ...receipt, sources: [] })).toEqual({
      direct: 0, files: 0, instructionFiles: 0, skillPackages: 0, otherFiles: 0,
    });
  });
});

describe("ContextReceiptView", () => {
  it("lists every source with its size, delivery and load state", () => {
    renderWithI18n(<ContextReceiptView receipt={receipt} />);
    const view = screen.getByTestId("context-receipt");

    // Embedded rows are not counted again in the total.
    expect(within(view).getByText("3 sources")).toBeTruthy();
    expect(within(view).getByText("Direct input: 3 KB")).toBeTruthy();
    expect(within(view).getByText("Files on disk: 38 KB")).toBeTruthy();
    expect(within(view).getByText("Instruction files: 19 KB")).toBeTruthy();
    expect(within(view).getByText("Skill packages: 19 KB")).toBeTruthy();
    expect(within(view).queryByText(/Other files:/)).toBeNull();
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

  it("names a complete skill package rather than implying its entry file has the package size", () => {
    const multifile: ContextReceipt = { ...receipt, sources: [
      { kind: "runtime_brief", bytes: 28672, delivery: "file", path: "AGENTS.md", load: "unverified" },
      { kind: "agent_instructions", bytes: 8192, delivery: "embedded", parent: "runtime_brief", load: "unverified" },
      { kind: "task_prompt", bytes: 5120, delivery: "prompt", load: "delivered" },
      { kind: "skill", name: "short", bytes: 23552, delivery: "file", path: ".agents/skills/short/SKILL.md", load: "unverified" },
      { kind: "skill", name: "multica-platform", bytes: 155524, sha256: "cb8cc2a583f2", delivery: "file", path: ".agents/skills/multica-platform/SKILL.md", load: "unverified" },
    ] };
    renderWithI18n(<ContextReceiptView receipt={multifile} />);
    expect(screen.getByText("4 sources")).toBeTruthy();
    expect(screen.getByText("Direct input: 5 KB")).toBeTruthy();
    expect(screen.getByText("Files on disk: 203 KB")).toBeTruthy();
    expect(screen.getByText("Instruction files: 28 KB")).toBeTruthy();
    expect(screen.getByText("Skill packages: 175 KB")).toBeTruthy();
    const row = screen.getByText("Skill: multica-platform").closest("tr")!;
    expect(within(row).getByText("152 KB")).toBeTruthy();
    expect(within(row).getByText("Package")).toBeTruthy();
    expect(within(row).getByText(".agents/skills/multica-platform/")).toBeTruthy();
    expect(within(row).queryByText(/SKILL\.md/)).toBeNull();
    expect(within(row).getByText("cb8cc2a583f2")).toBeTruthy();
    expect(within(row).getByText("Not verified")).toBeTruthy();
  });

  it.each([
    [String.raw`C:\DSH\skills\review\SKILL.md`, String.raw`C:\DSH\skills\review` + "\\"],
    ["$CODEX_HOME/skills/review/SKILL.md", "$CODEX_HOME/skills/review/"],
    ["<QwenPaw workspace>/skills/review/SKILL.md", "<QwenPaw workspace>/skills/review/"],
    ["SKILL.md", "."],
    ["native://custom-skill", "native://custom-skill"],
    ["native://SKILL.md", "native://SKILL.md"],
    ["native://record?id=/SKILL.md", "native://record?id=/SKILL.md"],
  ])("preserves the path representation for %s", (path, expected) => {
    renderWithI18n(<ContextReceiptView receipt={{ ...receipt, sources: [
      { kind: "skill", name: "review", bytes: 5120, delivery: "file", path, load: "unverified" },
    ] }} />);
    const row = screen.getByText("Skill: review").closest("tr")!;
    const delivery = within(row).getByText(expected);
    expect(delivery.textContent).toBe(expected);
    expect(delivery).toHaveAttribute("title", expected);
    if (/[/\\]/.test(expected)) {
      const breaks = delivery.querySelectorAll("wbr");
      expect(breaks.length).toBeGreaterThan(0);
      for (const br of breaks) {
        const prefix = document.createRange();
        prefix.selectNodeContents(delivery);
        prefix.setEndBefore(br);
        expect(prefix.toString()).toMatch(/[/\\]$/);
      }
    }
    expect(within(row).getByText("Package")).toBeTruthy();
  });

  it("keeps unfamiliar file kinds in the disk total and labels their own subtotal", () => {
    renderWithI18n(<ContextReceiptView receipt={{ ...receipt, sources: [
      { kind: "future", bytes: 2048, delivery: "file", load: "unknown" },
    ] }} />);
    expect(screen.getByText("Files on disk: 2 KB")).toBeTruthy();
    expect(screen.getByText("Other files: 2 KB")).toBeTruthy();
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
    expect(screen.getByText("直接输入：3 KB")).toBeTruthy();
    expect(screen.getByText("磁盘文件：38 KB")).toBeTruthy();
    expect(screen.getByText("运行说明文件：19 KB")).toBeTruthy();
    expect(screen.getByText("skill 包：19 KB")).toBeTruthy();
    expect(screen.getByText("包总量")).toBeTruthy();
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
