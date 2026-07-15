// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RunDetailDrawer } from "./run-detail-drawer";

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: { rows: [] }, isLoading: false }),
}));

afterEach(cleanup);

describe("RunDetailDrawer", () => {
  it("matches the mockup rail sections and primary trace action", () => {
    render(
      <RunDetailDrawer
        workspaceId="workspace"
        timezone="UTC"
        onClose={vi.fn()}
        run={{
          run: "run-1",
          source: "issue",
          person: "Lone",
          status: "completed",
          provider: "openai",
          model: "gpt-5",
          runtime: "Codex Local",
          debug_link: "/issues/issue-1?run=run-1",
          reference_label: "Lone",
          trace: "/trace/run-1",
        }}
      />,
    );

    expect(screen.getByText("Debug links")).toBeTruthy();
    expect(screen.getByText("Execution")).toBeTruthy();
    expect(screen.getByText("Usage & economics")).toBeTruthy();
    expect(screen.getByText("Skills used")).toBeTruthy();
    expect(screen.getByText("Trace preview")).toBeTruthy();
    expect(screen.getByRole("link", { name: /LoneIssue \/ thread contextOpen →/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: /Triggering commentRun source conversationOpen thread →/ })).toBeTruthy();
    const execution = screen.getByText("Execution").closest("section")!;
    expect([...execution.querySelectorAll("span")].map((node) => node.textContent)).toEqual([
      "Runtime", "Codex Local", "Provider", "openai", "Model", "gpt-5", "Duration", "0s",
    ]);
    expect(screen.getByRole("link", { name: "Open full Trace →" }).className).toContain("bg-[#6557d8]");
  });

  it("uses the debug link for the primary trace action when analytics only has a trace id", () => {
    render(
      <RunDetailDrawer
        workspaceId="workspace"
        timezone="UTC"
        onClose={vi.fn()}
        run={{
          run: "run-1",
          source: "issue",
          person: "Lone",
          status: "completed",
          debug_link: "/issues/issue-1?run=run-1",
          trace: "trace-id-1",
        }}
      />,
    );

    expect(screen.getByRole("link", { name: "Open full Trace →" }).getAttribute("href")).toBe(
      "/issues/issue-1?run=run-1",
    );
  });
});
