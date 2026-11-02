/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import { proposalKeys, snoozeDateBounds, type Proposal } from "@multica/core/office";
import type { Squad } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { OfficeDecisionsDialog } from "./office-decisions-dialog";

const decide = vi.hoisted(() => ({ mutate: vi.fn() }));
const toasts = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
const revisions = vi.hoisted(() => ({ list: vi.fn() }));

vi.mock("sonner", () => ({ toast: toasts }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { listProposalRevisions: revisions.list },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/office", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/office")>()),
  useDecideProposal: () => ({ mutate: decide.mutate, isPending: false }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));
vi.mock("@multica/core/workspace/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/hooks")>()),
  useActorName: () => ({ getActorName: (type: string, id: string) => `${type} ${id}` }),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ children, ...props }: React.ComponentProps<"a">) => <a {...props}>{children}</a>,
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
    revision: 1,
    expires_at: null,
    snoozed_until: null,
    related: [],
    handoff: null,
    ...extra,
  } as Proposal;
}

const inDays = (days: number) => new Date(Date.now() + days * 24 * 60 * 60 * 1000).toISOString();

describe("OfficeDecisionsDialog", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    decide.mutate.mockReset();
    toasts.success.mockReset();
    toasts.error.mockReset();
    revisions.list.mockReset();
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

  /** Makes the next decision fail with the given refusal. */
  function refuse(status: number, code: string | null, message: string) {
    decide.mutate.mockImplementation((_vars, options: { onError: (error: Error) => void }) => {
      options.onError(new ApiError(message, status, "", code ? { code, error: message } : { error: message }));
    });
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
    expect(screen.queryByRole("button", { name: "Ask for changes" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Decide later" })).toBeNull();
  });

  it("says when nothing is waiting", () => {
    setup({ proposals: [] });

    expect(screen.getByText("Nothing is waiting for you")).toBeTruthy();
  });

  describe("ask for changes", () => {
    it("requires a note before it sends anything", async () => {
      setup();
      await screen.findByText("Market body");

      fireEvent.click(screen.getByRole("button", { name: "Ask for changes" }));

      expect(decide.mutate).not.toHaveBeenCalled();
      expect(screen.getByRole("alert").textContent).toBe("Write what should change first.");
      expect(screen.getByRole("textbox").getAttribute("aria-invalid")).toBe("true");
    });

    it("sends the note as a revise decision and clears the requirement once one is written", async () => {
      setup();
      await screen.findByText("Market body");

      fireEvent.click(screen.getByRole("button", { name: "Ask for changes" }));
      fireEvent.change(screen.getByRole("textbox"), { target: { value: "  Price the pilot per seat  " } });
      expect(screen.queryByRole("alert")).toBeNull();

      fireEvent.click(screen.getByRole("button", { name: "Ask for changes" }));

      expect(decide.mutate).toHaveBeenCalledWith(
        { id: "a", decision: "revise", note: "Price the pilot per seat" },
        expect.any(Object),
      );
    });

    it("refuses a decision the squad is already revising and says so", async () => {
      refuse(409, "proposal_revising", "the squad is revising this proposal");
      setup();
      await screen.findByText("Market body");

      fireEvent.click(screen.getByRole("button", { name: "Reject" }));

      expect(toasts.error).toHaveBeenCalledWith("The squad is already revising this proposal");
    });
  });

  describe("decide later", () => {
    it("limits the date to tomorrow through thirty days out, in UTC", async () => {
      setup();
      await screen.findByText("Market body");
      const bounds = snoozeDateBounds(new Date());

      fireEvent.click(screen.getByRole("button", { name: "Decide later" }));
      const input = screen.getByLabelText("Until") as HTMLInputElement;

      expect(input.min).toBe(bounds.min);
      expect(input.max).toBe(bounds.max);
    });

    it("keeps the confirm disabled outside the range and sends a date inside it", async () => {
      setup();
      await screen.findByText("Market body");
      const bounds = snoozeDateBounds(new Date());

      fireEvent.click(screen.getByRole("button", { name: "Decide later" }));
      const confirm = screen.getByRole("button", { name: "Set date" }) as HTMLButtonElement;
      expect(confirm.disabled).toBe(true);

      fireEvent.change(screen.getByLabelText("Until"), { target: { value: "2000-01-01" } });
      expect(confirm.disabled).toBe(true);

      fireEvent.change(screen.getByLabelText("Until"), { target: { value: bounds.max } });
      expect(confirm.disabled).toBe(false);
      fireEvent.click(confirm);

      expect(decide.mutate).toHaveBeenCalledWith(
        { id: "a", decision: "snooze", until: bounds.max },
        expect.any(Object),
      );
    });

    it("shows the server's message when the date is refused", async () => {
      refuse(400, "snooze_out_of_range", "until must be later than now and at most 30 days away");
      setup();
      await screen.findByText("Market body");
      const bounds = snoozeDateBounds(new Date());

      fireEvent.click(screen.getByRole("button", { name: "Decide later" }));
      fireEvent.change(screen.getByLabelText("Until"), { target: { value: bounds.min } });
      fireEvent.click(screen.getByRole("button", { name: "Set date" }));

      expect(toasts.error).toHaveBeenCalledWith("until must be later than now and at most 30 days away");
    });
  });

  describe("needs your decision", () => {
    it("hides a proposal put aside until a later date from the queue, and keeps it in the full list", async () => {
      setup({
        proposals: [proposal("a"), proposal("b", { squad_id: "product", snoozed_until: inDays(5) })],
      });
      await screen.findByText("Market body");

      const queue = screen.getByRole("list", { name: "Decisions" });
      expect(within(queue).queryByRole("button", { name: /Proposal b/ })).toBeNull();
      expect(within(queue).getByRole("button", { name: /Proposal a/ })).toBeTruthy();

      const later = screen.getByRole("list", { name: "Decide later" });
      expect(within(later).getByRole("button", { name: /Proposal b/ })).toBeTruthy();
    });

    it("says it is decided later, with the date, when a put-aside proposal is shown", async () => {
      setup({ proposals: [proposal("b", { snoozed_until: inDays(5) })] });

      expect(await screen.findByText(/Decide later until/)).toBeTruthy();
      expect(screen.getByRole("button", { name: "Decide later" }).getAttribute("aria-expanded")).toBe("false");
    });

    it("lists proposals sent back for changes under waiting on the squad, with their note and no decide controls", async () => {
      setup({
        proposals: [
          proposal("a"),
          proposal("r", { state: "revising", decision_note: "Cite the Q3 numbers", squad_id: "product" }),
        ],
      });
      await screen.findByText("Market body");

      const waiting = screen.getByRole("list", { name: "Waiting on the squad" });
      fireEvent.click(within(waiting).getByRole("button", { name: /Proposal r/ }));

      expect(await screen.findByText("Revising")).toBeTruthy();
      expect(screen.getByText("Build is revising it")).toBeTruthy();
      expect(screen.getByText("Cite the Q3 numbers")).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Ask for changes" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Decide later" })).toBeNull();
    });
  });

  describe("related prior decisions", () => {
    it("shows the related entries, with a link where an issue identifier exists", async () => {
      setup({
        proposals: [
          proposal("a", {
            related: [
              {
                kind: "proposal",
                id: "p0",
                title: "Pilot pricing",
                score: 0.8,
                state: "approved",
                issue_identifier: "MUL-3",
                created_at: "2026-10-01T00:00:00Z",
              },
              { kind: "decision", id: "d1", title: "Usage pricing memo", score: 0.91, state: null, issue_identifier: null, created_at: null },
            ],
          }),
        ],
      });
      await screen.findByText("Market body");

      expect(screen.getByRole("heading", { name: "Related prior decisions" })).toBeTruthy();
      expect(screen.getByText("Pilot pricing")).toBeTruthy();
      expect(screen.getByText("Approved")).toBeTruthy();
      expect(screen.getByRole("link", { name: "MUL-3" }).getAttribute("href")).toBe("/acme/issues/MUL-3");
      expect(screen.getByText("Usage pricing memo")).toBeTruthy();
    });

    it("shows no related section when nothing is related", async () => {
      setup();
      await screen.findByText("Market body");

      expect(screen.queryByText("Related prior decisions")).toBeNull();
    });
  });

  describe("revision history", () => {
    it("stays closed until opened, then shows each revision with its actor and body", async () => {
      queryClient.setQueryData(proposalKeys.revisions("ws-1", "a"), [
        { revision: 2, body: "Revised pricing", created_by_type: "agent", created_by_id: "ada", created_at: "2026-11-02T00:00:00Z" },
        { revision: 1, body: "First filing", created_by_type: "agent", created_by_id: "ada", created_at: "2026-11-01T00:00:00Z" },
      ]);
      setup();
      await screen.findByText("Market body");

      expect(screen.queryByText("Revised pricing")).toBeNull();
      const toggle = screen.getByRole("button", { name: "Revision history" });
      expect(toggle.getAttribute("aria-expanded")).toBe("false");

      fireEvent.click(toggle);

      expect(toggle.getAttribute("aria-expanded")).toBe("true");
      expect(screen.getByText(/Revision 2/)).toBeTruthy();
      expect(screen.getByText("Revised pricing")).toBeTruthy();
      expect(screen.getByText(/Revision 1/)).toBeTruthy();
      expect(screen.getAllByText(/agent ada/)).toHaveLength(2);
    });

    it("says when the history cannot be loaded", async () => {
      revisions.list.mockRejectedValue(new Error("network down"));
      setup();
      await screen.findByText("Market body");

      fireEvent.click(screen.getByRole("button", { name: "Revision history" }));

      expect(await screen.findByText("Couldn't load the revision history")).toBeTruthy();
    });
  });
});
