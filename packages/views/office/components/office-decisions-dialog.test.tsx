/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { issueKeys } from "@multica/core/issues/queries";
import type { Proposal } from "@multica/core/office";
import type { Squad } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { OfficeDecisionsDialog } from "./office-decisions-dialog";

const decide = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/office", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/office")>()),
  useDecideProposal: () => ({ mutate: decide.mutate, isPending: false }),
}));
// The rich body has its own tests; here it shows its text.
vi.mock("../../editor/readonly-content", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
}));

const SQUADS = [
  { id: "research", name: "Research", archived_at: null },
  { id: "product", name: "Build", archived_at: null },
] as Squad[];

function proposal(id: string, extra: Partial<Proposal> = {}): Proposal {
  return {
    id,
    workspace_id: "ws-1",
    squad_id: "research",
    issue_id: `issue-${id}`,
    issue_identifier: `MUL-${id}`,
    issue_status: "in_review",
    title: `Proposal ${id}`,
    handoff_squad_id: null,
    urgent: false,
    urgent_reason: "",
    state: "pending",
    created_by_type: "agent",
    created_by_id: "ada",
    decided_by: null,
    decided_at: null,
    decision_note: "",
    created_at: "2026-01-01T00:00:00Z",
    handoff: null,
    ...extra,
  } as Proposal;
}

describe("OfficeDecisionsDialog", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    decide.mutate.mockReset();
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(issueKeys.detail("ws-1", "issue-a"), { description: "Market body" });
    queryClient.setQueryData(issueKeys.detail("ws-1", "issue-b"), { description: "Launch body" });
  });
  afterEach(cleanup);

  function setup(props: Partial<Parameters<typeof OfficeDecisionsDialog>[0]> = {}) {
    const onClose = vi.fn();
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <OfficeDecisionsDialog
          open
          proposals={[proposal("a"), proposal("b", { squad_id: "product", handoff_squad_id: "product" })]}
          squads={SQUADS}
          canDecide
          onClose={onClose}
          {...props}
        />
      </QueryClientProvider>,
    );
    return { onClose };
  }

  it("shows the selected proposal's body, its squad, and the handoff", async () => {
    setup();

    expect(await screen.findByText("Market body")).toBeTruthy();
    expect(screen.getByText("From Research")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Approve" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Proposal b/ }));

    expect(await screen.findByText("Launch body")).toBeTruthy();
    expect(screen.getByText("Hands off to Build")).toBeTruthy();
  });

  it("says what an approval with a handoff will start", async () => {
    setup({ proposals: [proposal("b", { squad_id: "product", handoff_squad_id: "product" })] });

    expect(await screen.findByRole("button", { name: "Approve and start a project for Build" })).toBeTruthy();
  });

  it("approves with the note the boss wrote", async () => {
    setup();
    await screen.findByText("Market body");

    fireEvent.change(screen.getByRole("textbox"), { target: { value: "Go ahead" } });
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));

    expect(decide.mutate).toHaveBeenCalledWith(
      { id: "a", decision: "approve", note: "Go ahead" },
      expect.any(Object),
    );
  });

  it("rejects without a note when none is written", async () => {
    setup();
    await screen.findByText("Market body");

    fireEvent.click(screen.getByRole("button", { name: "Reject" }));

    expect(decide.mutate).toHaveBeenCalledWith({ id: "a", decision: "reject", note: undefined }, expect.any(Object));
  });

  it("reads only, for someone who cannot decide", async () => {
    setup({ canDecide: false });
    await screen.findByText("Market body");

    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Reject" })).toBeNull();
  });

  it("says when nothing is waiting", () => {
    setup({ proposals: [] });

    expect(screen.getByText("Nothing is waiting for you")).toBeTruthy();
  });
});
