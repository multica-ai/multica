/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type { InstructionRevision } from "@multica/core/agents";
import type { Agent } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { AgentInstructionRevisions } from "./agent-instruction-revisions";

const api = vi.hoisted(() => ({
  listAgentInstructionRevisions: vi.fn(),
  restoreAgentInstructionRevision: vi.fn(),
}));
const toasts = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));

vi.mock("sonner", () => ({ toast: toasts }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/hooks")>()),
  useActorName: () => ({ getActorName: (type: string, id: string) => `${type} ${id}` }),
}));

const CURRENT = "Be concise.\nAsk before spending.";

function agent(over: Partial<Agent> = {}): Agent {
  return { id: "ag-1", name: "Ada", instructions: CURRENT, ...over } as Agent;
}

function revision(number: number, extra: Partial<InstructionRevision> = {}): InstructionRevision {
  return {
    id: `rev-${number}`,
    agent_id: "ag-1",
    revision: number,
    instructions: `Text of revision ${number}`,
    source: "human",
    proposal_id: null,
    actor_type: "member",
    actor_id: "u-1",
    created_at: "2026-11-01T00:00:00Z",
    ...extra,
  };
}

function renderPanel(props: { agent?: Agent; canEdit?: boolean } = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <AgentInstructionRevisions agent={props.agent ?? agent()} canEdit={props.canEdit ?? true} />
    </QueryClientProvider>,
  );
}

async function openHistory() {
  fireEvent.click(screen.getByRole("button", { name: "Show revisions" }));
}

describe("AgentInstructionRevisions", () => {
  beforeEach(() => {
    api.listAgentInstructionRevisions.mockReset();
    api.restoreAgentInstructionRevision.mockReset();
    toasts.success.mockReset();
    toasts.error.mockReset();
  });
  afterEach(cleanup);

  it("loads the history only once it is opened", async () => {
    api.listAgentInstructionRevisions.mockResolvedValue([revision(1)]);
    renderPanel();

    expect(api.listAgentInstructionRevisions).not.toHaveBeenCalled();
    await openHistory();
    expect(await screen.findByText("Revision 1")).toBeTruthy();
    expect(api.listAgentInstructionRevisions).toHaveBeenCalledWith("ag-1");
  });

  it("lists each revision with its source, author and time, and marks the current one", async () => {
    api.listAgentInstructionRevisions.mockResolvedValue([
      revision(3, { source: "rollback", actor_type: "member", actor_id: "u-2", instructions: CURRENT }),
      revision(2, { source: "proposal", actor_type: "agent", actor_id: "ag-9" }),
      revision(1, { source: "human" }),
    ]);
    renderPanel();
    await openHistory();

    const first = (await screen.findByText("Revision 3")).closest("li") as HTMLElement;
    expect(within(first).getByText("Restored")).toBeTruthy();
    expect(within(first).getByText("member u-2")).toBeTruthy();
    expect(within(first).getByText("Current")).toBeTruthy();
    expect(within(first).queryByRole("button", { name: "Restore revision 3" })).toBeNull();

    const second = screen.getByText("Revision 2").closest("li") as HTMLElement;
    expect(within(second).getByText("From an approved proposal")).toBeTruthy();
    expect(within(second).getByText("agent ag-9")).toBeTruthy();

    const third = screen.getByText("Revision 1").closest("li") as HTMLElement;
    expect(within(third).getByText("Edited")).toBeTruthy();
    expect(within(third).getByRole("button", { name: "Restore revision 1" })).toBeTruthy();
  });

  it("restores an earlier revision and reports it", async () => {
    api.listAgentInstructionRevisions.mockResolvedValue([revision(2, { instructions: CURRENT }), revision(1)]);
    api.restoreAgentInstructionRevision.mockResolvedValue(undefined);
    renderPanel();
    await openHistory();

    fireEvent.click(await screen.findByRole("button", { name: "Restore revision 1" }));

    await waitFor(() => expect(toasts.success).toHaveBeenCalledWith("Restored revision 1"));
    expect(api.restoreAgentInstructionRevision).toHaveBeenCalledWith("ag-1", 1);
    expect(toasts.error).not.toHaveBeenCalled();
  });

  it("says so when the agent already has the revision's instructions", async () => {
    api.listAgentInstructionRevisions.mockResolvedValue([revision(2, { instructions: CURRENT }), revision(1)]);
    api.restoreAgentInstructionRevision.mockRejectedValue(new ApiError("conflict", 409, "Conflict"));
    renderPanel();
    await openHistory();

    fireEvent.click(await screen.findByRole("button", { name: "Restore revision 1" }));

    await waitFor(() =>
      expect(toasts.error).toHaveBeenCalledWith("The agent already has these instructions."),
    );
  });

  it("shows no restore action to someone who cannot edit the agent", async () => {
    api.listAgentInstructionRevisions.mockResolvedValue([revision(2), revision(1)]);
    renderPanel({ canEdit: false });
    await openHistory();

    expect(await screen.findByText("Revision 1")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Restore revision/ })).toBeNull();
  });

  it("shows the text of a revision when asked", async () => {
    api.listAgentInstructionRevisions.mockResolvedValue([revision(1, { instructions: "Keep replies short." })]);
    renderPanel();
    await openHistory();

    expect(screen.queryByText("Keep replies short.")).toBeNull();
    fireEvent.click(await screen.findByRole("button", { name: "Show text" }));
    expect(screen.getByText("Keep replies short.")).toBeTruthy();
  });

  // Revision text is the agent's instructions, which a human or an agent wrote
  // and an imported document can carry. It is shown as text: markup in it never
  // becomes an element (spec 4.9, inert Markdown).
  it("shows markup in a revision as text, never as elements", async () => {
    const hostile = '<img src=x onerror="window.__revisionXss = true"><script>window.__revisionXss = true</script>';
    api.listAgentInstructionRevisions.mockResolvedValue([revision(1, { instructions: hostile })]);
    renderPanel();
    await openHistory();

    fireEvent.click(await screen.findByRole("button", { name: "Show text" }));
    expect(screen.getByText(hostile)).toBeTruthy();
    expect(document.querySelector("img")).toBeNull();
    expect(document.querySelector("script")).toBeNull();
    expect((window as unknown as { __revisionXss?: boolean }).__revisionXss).toBeUndefined();
  });

  it("says when the history cannot be loaded", async () => {
    api.listAgentInstructionRevisions.mockRejectedValue(new Error("network down"));
    renderPanel();
    await openHistory();

    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toBe("Couldn't load the revision history.");
  });
});
