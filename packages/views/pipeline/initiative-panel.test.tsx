/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type { InitiativeDetail, Pipeline } from "@multica/core/pipeline";
import type { Squad } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { InitiativePanel } from "./initiative-panel";

const api = vi.hoisted(() => ({
  getInitiative: vi.fn(),
  waiveInitiativeArtifact: vi.fn(),
  forceInitiative: vi.fn(),
  decideProposal: vi.fn(),
  listSquadMembers: vi.fn(),
  advanceInitiative: vi.fn(),
  attestInitiative: vi.fn(),
  sendBackInitiative: vi.fn(),
  shelveInitiative: vi.fn(),
  resumeInitiative: vi.fn(),
  closeInitiative: vi.fn(),
}));
const role = vi.hoisted(() => ({ value: "admin" as "owner" | "admin" | "member" }));
const toasts = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));

vi.mock("sonner", () => ({ toast: toasts }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useCurrentMember: () => ({ userId: "u-1", role: role.value, member: null, isLoading: false }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/acme/issues/${id}`,
    knowledge: () => "/acme/knowledge",
  }),
}));
vi.mock("@multica/core/workspace/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/hooks")>()),
  useActorName: () => ({ getActorName: (type: string, id: string) => `${type} ${id}` }),
}));
vi.mock("../navigation", () => ({
  AppLink: ({ children, ...props }: React.ComponentProps<"a">) => <a {...props}>{children}</a>,
}));
// The thread is IssueDetail's own; these tests check only that the panel embeds it.
vi.mock("../issues/components/issue-detail", () => ({
  IssueDetail: ({ issueId, variant }: { issueId: string; variant?: string }) => (
    <div data-testid="issue-detail" data-issue-id={issueId} data-variant={variant} />
  ),
}));

const PIPELINE: Pipeline = {
  id: "pl-1",
  name: "Default",
  is_default: true,
  stages: [
    {
      key: "validate",
      name: "Validate",
      owner_squad_id: "sq-research",
      objective: "",
      instructions: "",
      artifacts: [],
      exit: { mode: "gate", criteria: "A go or no-go." },
      sla_days: 14,
    },
    {
      key: "define",
      name: "Define",
      owner_squad_id: null,
      objective: "",
      instructions: "",
      artifacts: [],
      exit: { mode: "attest", criteria: "" },
      sla_days: null,
    },
  ],
};

const SQUADS = [{ id: "sq-research", name: "Research", archived_at: null }] as Squad[];

const DETAIL: InitiativeDetail = {
  initiative: {
    id: "in-1",
    identifier: "INI-1",
    title: "Invoicing for small firms",
    thesis: "Month-end close is slow for small firms.",
    pipeline_id: "pl-1",
    stage_key: "validate",
    owner_squad_id: "sq-research",
    status: "active",
    issue_id: "iss-1",
    build_project_id: null,
    stage_entered_at: "2026-10-20T00:00:00Z",
    days_in_stage: 13,
    sla_state: "stalled",
    artifacts_required: 2,
    artifacts_present: 1,
    pending_gate_proposal_id: "p-9",
  },
  artifacts: [
    { kind: "validation_report", stage_key: "validate", required: true, knowledge_link_id: "kl-1", waived: false, waived_note: "" },
    { kind: "competitor_profile", stage_key: "validate", required: true, knowledge_link_id: null, waived: false, waived_note: "" },
    { kind: "customer_insight", stage_key: "validate", required: false, knowledge_link_id: null, waived: false, waived_note: "" },
  ],
  events: [
    { id: "ev-1", kind: "created", from_stage: null, to_stage: "validate", actor_type: "member", actor_id: "u-1", note: "", created_at: "2026-10-20T00:00:00Z" },
    { id: "ev-2", kind: "attested", from_stage: "validate", to_stage: null, actor_type: "agent", actor_id: "a-1", note: "Ready for review", created_at: "2026-10-30T00:00:00Z" },
  ],
  gates: [{ id: "p-9", state: "pending", title: "Commit to a product?", issue_identifier: "MUL-42", created_at: "2026-10-31T00:00:00Z" }],
};

function renderPanel(pipeline: Pipeline = PIPELINE) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <InitiativePanel initiativeId="in-1" pipeline={pipeline} squads={SQUADS} />
    </QueryClientProvider>,
  );
}

describe("InitiativePanel", () => {
  beforeEach(() => {
    role.value = "admin";
    api.getInitiative.mockResolvedValue(DETAIL);
    api.waiveInitiativeArtifact.mockReset().mockResolvedValue(undefined);
    api.forceInitiative.mockReset().mockResolvedValue(undefined);
    api.decideProposal.mockReset().mockResolvedValue({});
    api.listSquadMembers.mockReset().mockResolvedValue([]);
    for (const transition of [
      api.advanceInitiative,
      api.attestInitiative,
      api.sendBackInitiative,
      api.shelveInitiative,
      api.resumeInitiative,
      api.closeInitiative,
    ]) {
      transition.mockReset().mockResolvedValue(DETAIL.initiative);
    }
    toasts.success.mockReset();
    toasts.error.mockReset();
  });
  afterEach(cleanup);

  it("shows the loading state before the detail arrives", () => {
    renderPanel();
    expect(screen.getByText("Loading initiative…")).toBeTruthy();
  });

  it("says so when the detail cannot be loaded", async () => {
    api.getInitiative.mockRejectedValue(new Error("network"));
    renderPanel();
    expect(await screen.findByText("This initiative could not be loaded.")).toBeTruthy();
  });

  it("shows the overview with the stage's exit rule and the stall", async () => {
    renderPanel();

    expect(await screen.findByText("Month-end close is slow for small firms.")).toBeTruthy();
    expect(screen.getByText("The boss decides")).toBeTruthy();
    expect(screen.getByText("A go or no-go.")).toBeTruthy();
    expect(screen.getByText("Past the stage SLA")).toBeTruthy();
  });

  it("lists the stage's artifacts with a link for each published one", async () => {
    renderPanel();

    expect(await screen.findByText("Validation report")).toBeTruthy();
    expect(screen.getByText("Published")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Open in knowledge" }).getAttribute("href")).toBe("/acme/knowledge");
    expect(screen.getByText("Competitor profile")).toBeTruthy();
    // The required competitor profile and the optional customer insight are both unpublished.
    expect(screen.getAllByText("Missing")).toHaveLength(2);
    expect(screen.getByText("Optional")).toBeTruthy();
  });

  it("offers Waive only to an owner or admin, and only for a missing required artifact", async () => {
    renderPanel();
    await screen.findByText("Competitor profile");
    expect(screen.getAllByRole("button", { name: "Waive" })).toHaveLength(1);
    cleanup();

    role.value = "member";
    renderPanel();
    await screen.findByText("Competitor profile");
    expect(screen.queryByRole("button", { name: "Waive" })).toBeNull();
  });

  it("requires a note before an artifact can be waived", async () => {
    renderPanel();
    fireEvent.click(await screen.findByRole("button", { name: "Waive" }));

    fireEvent.click(screen.getByRole("button", { name: "Waive artifact" }));
    expect(screen.getByRole("alert").textContent).toBe("A note is required to waive an artifact.");
    expect(api.waiveInitiativeArtifact).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("Why it is waived"), { target: { value: "Covered by the market report" } });
    fireEvent.click(screen.getByRole("button", { name: "Waive artifact" }));

    await waitFor(() =>
      expect(api.waiveInitiativeArtifact).toHaveBeenCalledWith("in-1", "competitor_profile", "Covered by the market report"),
    );
  });

  it("shows the Force move control to an admin, with the reason required", async () => {
    renderPanel();
    expect(await screen.findByRole("heading", { name: "Force move" })).toBeTruthy();
    const force = screen.getByRole("button", { name: "Force move" }) as HTMLButtonElement;
    expect(force.disabled).toBe(true);
  });

  it("hides the Force move control from a member", async () => {
    role.value = "member";
    renderPanel();
    await screen.findByText("Validation report");
    expect(screen.queryByRole("heading", { name: "Force move" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Force move" })).toBeNull();
  });

  it("approves a gate with one click and rejects only with a note", async () => {
    renderPanel();
    expect(await screen.findByText("Commit to a product?")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Reject" }));
    fireEvent.click(screen.getByRole("button", { name: "Reject gate" }));
    expect(screen.getByRole("alert").textContent).toBe("A note is required to reject a gate.");
    expect(api.decideProposal).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("Why it is rejected"), { target: { value: "Market is too small" } });
    fireEvent.click(screen.getByRole("button", { name: "Reject gate" }));
    await waitFor(() =>
      expect(api.decideProposal).toHaveBeenCalledWith("p-9", { decision: "reject", note: "Market is too small" }),
    );

    api.decideProposal.mockClear();
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() => expect(api.decideProposal).toHaveBeenCalledWith("p-9", { decision: "approve" }));
  });

  it("decides an operate review with one of its three outcomes", async () => {
    api.getInitiative.mockResolvedValue({
      ...DETAIL,
      gates: [
        { id: "p-30", state: "pending", gate_day: 30, title: "Review at 30 days", issue_identifier: "MUL-43", created_at: "2026-10-31T00:00:00Z" },
      ],
    });
    renderPanel();
    expect(await screen.findByText("Review at 30 days")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Scale" }));
    await waitFor(() => expect(api.decideProposal).toHaveBeenCalledWith("p-30", { decision: "approve", outcome: "scale" }));
  });

  it("hides gate decisions from a member", async () => {
    role.value = "member";
    renderPanel();
    expect(await screen.findByText("Commit to a product?")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Reject" })).toBeNull();
  });

  it("lists the stage timeline with each event's note", async () => {
    renderPanel();
    expect(await screen.findByText("Created")).toBeTruthy();
    expect(screen.getByText("Attested")).toBeTruthy();
    expect(screen.getByText("Ready for review")).toBeTruthy();
  });

  describe("stage transitions", () => {
    /** The detail at a given stage and status, with the squad membership the caller has. */
    function mockAt(stageKey: string, status: "active" | "shelved" = "active", squadMember = false) {
      api.getInitiative.mockResolvedValue({
        ...DETAIL,
        initiative: { ...DETAIL.initiative, stage_key: stageKey, status },
      });
      api.listSquadMembers.mockResolvedValue(
        squadMember ? [{ id: "m-1", squad_id: "sq-research", member_type: "member", member_id: "u-1", role: "", created_at: "" }] : [],
      );
    }

    it("offers no transition to a member outside the owning squad", async () => {
      role.value = "member";
      mockAt("define");
      renderPanel();
      await screen.findByText("Month-end close is slow for small firms.");
      await waitFor(() => expect(api.listSquadMembers).toHaveBeenCalledWith("sq-research"));
      expect(screen.queryByRole("region", { name: "Actions" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Attest stage" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Advance" })).toBeNull();
    });

    it("offers Attest and Advance to a squad member on an attest stage, and not the admin actions", async () => {
      role.value = "member";
      mockAt("define", "active", true);
      renderPanel();
      expect(await screen.findByRole("button", { name: "Attest stage" })).toBeTruthy();
      expect(screen.getByRole("button", { name: "Advance" })).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Send back" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Shelve" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Close" })).toBeNull();
    });

    it("withholds Advance from a squad member on a gate stage, because the gate decides", async () => {
      role.value = "member";
      mockAt("validate", "active", true);
      renderPanel();
      await screen.findByText("Month-end close is slow for small firms.");
      await waitFor(() => expect(api.listSquadMembers).toHaveBeenCalled());
      expect(screen.queryByRole("button", { name: "Advance" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Attest stage" })).toBeNull();
    });

    it("offers Attest and Advance on an attest stage", async () => {
      mockAt("define");
      renderPanel();
      expect(await screen.findByRole("button", { name: "Attest stage" })).toBeTruthy();
      expect(screen.getByRole("button", { name: "Advance" })).toBeTruthy();
    });

    it("offers Advance but not Attest on an auto stage", async () => {
      mockAt("define");
      const auto: Pipeline = {
        ...PIPELINE,
        stages: PIPELINE.stages.map((stage) =>
          stage.key === "define" ? { ...stage, exit: { mode: "auto", criteria: "" } } : stage,
        ),
      };
      renderPanel(auto);
      expect(await screen.findByRole("button", { name: "Advance" })).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Attest stage" })).toBeNull();
    });

    it("offers Send back only when the stage has an earlier stage, to an admin", async () => {
      mockAt("validate");
      renderPanel();
      await screen.findByText("Month-end close is slow for small firms.");
      expect(screen.queryByRole("button", { name: "Send back" })).toBeNull();
      cleanup();

      mockAt("define");
      renderPanel();
      expect(await screen.findByRole("button", { name: "Send back" })).toBeTruthy();
    });

    it("shows Resume only on a shelved initiative, and Shelve and Close only on a live one", async () => {
      mockAt("define", "shelved");
      renderPanel();
      expect(await screen.findByRole("button", { name: "Resume" })).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Shelve" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Close" })).toBeNull();
      cleanup();

      mockAt("define", "active");
      renderPanel();
      expect(await screen.findByRole("button", { name: "Shelve" })).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Resume" })).toBeNull();
    });

    it("confirms Shelve before it is sent, and Cancel sends nothing", async () => {
      mockAt("define");
      renderPanel();
      fireEvent.click(await screen.findByRole("button", { name: "Shelve" }));

      const dialog = await screen.findByRole("alertdialog");
      expect(within(dialog).getByText("Shelve this initiative?")).toBeTruthy();
      fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
      await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
      expect(api.shelveInitiative).not.toHaveBeenCalled();

      fireEvent.click(screen.getByRole("button", { name: "Shelve" }));
      const again = await screen.findByRole("alertdialog");
      fireEvent.change(within(again).getByLabelText("Note (optional)"), { target: { value: "Waiting on legal" } });
      fireEvent.click(within(again).getByRole("button", { name: "Shelve initiative" }));
      await waitFor(() => expect(api.shelveInitiative).toHaveBeenCalledWith("in-1", "Waiting on legal"));
      expect(toasts.success).toHaveBeenCalledWith("Initiative shelved.");
    });

    it("requires a reason before Close is sent", async () => {
      mockAt("define");
      renderPanel();
      fireEvent.click(await screen.findByRole("button", { name: "Close" }));

      const dialog = await screen.findByRole("alertdialog");
      fireEvent.click(within(dialog).getByRole("button", { name: "Close initiative" }));
      expect(within(dialog).getByRole("alert").textContent).toBe("A reason is required to close an initiative.");
      expect(api.closeInitiative).not.toHaveBeenCalled();

      fireEvent.change(within(dialog).getByLabelText("Reason"), { target: { value: "Sunset after the 30-day review" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Close initiative" }));
      await waitFor(() => expect(api.closeInitiative).toHaveBeenCalledWith("in-1", "Sunset after the 30-day review"));
    });

    it("requires a note before Send back is sent", async () => {
      mockAt("define");
      renderPanel();
      fireEvent.click(await screen.findByRole("button", { name: "Send back" }));

      fireEvent.click(screen.getByRole("button", { name: "Send back" }));
      expect(screen.getByRole("alert").textContent).toBe("A note is required to send an initiative back.");
      expect(api.sendBackInitiative).not.toHaveBeenCalled();

      fireEvent.change(screen.getByLabelText("Note for the leader"), { target: { value: "Re-check the price point" } });
      fireEvent.click(screen.getByRole("button", { name: "Send back" }));
      await waitFor(() => expect(api.sendBackInitiative).toHaveBeenCalledWith("in-1", "Re-check the price point"));
    });

    it("shows the sentence for the server's error code when a transition is refused", async () => {
      mockAt("define", "active", true);
      api.advanceInitiative.mockRejectedValue(
        new ApiError("required artifacts are missing", 409, "Conflict", { error: "x", code: "stage_not_ready" }),
      );
      renderPanel();
      fireEvent.click(await screen.findByRole("button", { name: "Advance" }));
      await waitFor(() =>
        expect(toasts.error).toHaveBeenCalledWith("The stage still needs its required artifacts."),
      );
    });

    it("falls back to the generic sentence for an error code it does not know", async () => {
      mockAt("define", "active", true);
      api.attestInitiative.mockRejectedValue(new ApiError("boom", 409, "Conflict", { error: "boom", code: "future_code" }));
      renderPanel();
      fireEvent.click(await screen.findByRole("button", { name: "Attest stage" }));
      await waitFor(() => expect(toasts.error).toHaveBeenCalledWith("Could not change the initiative."));
    });
  });

  it("embeds the backing issue's thread, read from the initiative's issue", async () => {
    renderPanel();
    const thread = await screen.findByTestId("issue-detail");
    expect(thread.getAttribute("data-issue-id")).toBe("iss-1");
    expect(thread.getAttribute("data-variant")).toBe("peek");
    expect(screen.getByRole("region", { name: "Backing issue thread" })).toBeTruthy();
  });
});
