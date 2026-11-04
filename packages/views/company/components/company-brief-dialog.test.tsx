/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { autopilotKeys } from "@multica/core/autopilots";
import {
  companySetupKeys,
  proposalKeys,
  type Brief,
  type CompanySetupReadiness,
  type Proposal,
} from "@multica/core/company";
import { knowledgeKeys } from "@multica/core/knowledge";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Autopilot, Squad } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { CompanyBriefDialog } from "./company-brief-dialog";

const mocks = vi.hoisted(() => ({
  decide: vi.fn(),
  trigger: vi.fn(),
  save: vi.fn(),
  push: vi.fn(),
  chat: {
    floatingChatEnabled: true,
    setSelectedAgentId: vi.fn(),
    setActiveSession: vi.fn(),
    appendToInputDraft: vi.fn(),
    setOpen: vi.fn(),
  },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/chat", () => ({
  DRAFT_NEW_SESSION: "__new__",
  useChatStore: Object.assign(() => undefined, { getState: () => mocks.chat }),
}));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ chat: () => "/acme/chat" }),
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ pathname: "/acme/issues", push: mocks.push }),
}));

vi.mock("@multica/core/company", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/company")>()),
  useDecideProposal: () => ({ mutate: mocks.decide, isPending: false }),
}));

vi.mock("@multica/core/autopilots", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/autopilots")>()),
  useTriggerAutopilot: () => ({ mutate: mocks.trigger, isPending: false }),
}));

vi.mock("@multica/core/knowledge", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/knowledge")>()),
  useCreateKnowledgeDocument: () => ({ mutate: mocks.save, isPending: false }),
}));

// The rich body has its own tests; here it shows its text.
vi.mock("../../editor/readonly-content", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
}));

const READY: CompanySetupReadiness = {
  runtime_online: true,
  secretary_id: "sec-1",
  executive_floor: true,
  autopilot_daily: true,
  autopilot_weekly_paused: true,
  ready: true,
};

const BODY = [
  "## Headline",
  "Two decisions wait for you.",
  "",
  "## Needs your decision",
  "Approve the pricing test.",
].join("\n");

function brief(id: string, patch: Partial<Brief> = {}): Brief {
  return {
    id,
    workspace_id: "ws-1",
    kind: "daily",
    period_start: "",
    period_end: "",
    title: `Brief ${id}`,
    body: BODY,
    proposal_ids: [],
    created_by_type: "agent",
    created_by_id: "sec-1",
    created_at: new Date().toISOString(),
    ...patch,
  };
}

function proposal(id: string, patch: Partial<Proposal> = {}): Proposal {
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
    ...patch,
  } as Proposal;
}

function autopilot(patch: Partial<Autopilot> = {}): Autopilot {
  return {
    id: "ap-1",
    workspace_id: "ws-1",
    title: "Daily brief",
    description: null,
    assignee_type: "agent",
    assignee_id: "sec-1",
    status: "active",
    execution_mode: "run_only",
    issue_title_template: null,
    created_by_type: "member",
    created_by_id: "m1",
    last_run_at: null,
    created_at: "",
    updated_at: "",
    ...patch,
  };
}

describe("CompanyBriefDialog", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.clearAllMocks();
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(companySetupKeys.all("ws-1"), READY);
    queryClient.setQueryData(autopilotKeys.list("ws-1"), { autopilots: [autopilot()], total: 1 });
    queryClient.setQueryData(workspaceKeys.squads("ws-1"), [] as Squad[]);
    queryClient.setQueryData(knowledgeKeys.bases("ws-1"), {
      offered: true,
      state: "ready",
      knowledge_bases: [{ id: "kb-1", key: "decisions", name: "Decisions", visibility: "workspace", position: 0 }],
    });
    queryClient.setQueryData(proposalKeys.detail("ws-1", "p1"), proposal("p1"));
    queryClient.setQueryData(proposalKeys.detail("ws-1", "p2"), proposal("p2", { state: "approved" }));
  });
  afterEach(cleanup);

  function setup(props: Partial<Parameters<typeof CompanyBriefDialog>[0]> = {}) {
    const onClose = vi.fn();
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <CompanyBriefDialog open briefs={[brief("b1")]} canManage onClose={onClose} {...props} />
      </QueryClientProvider>,
    );
    return { onClose };
  }

  it("opens on the brief a link names instead of the latest one", async () => {
    const earlier = brief("b0", { title: "Earlier brief", body: "## Headline\nThe old news." });
    setup({ briefs: [brief("b1"), earlier], focusBriefId: "b0" });

    expect(await screen.findByText("The old news.")).toBeTruthy();
  });

  it("renders the latest brief's body by section, with an Ask on each", () => {
    setup();
    expect(screen.getByRole("heading", { name: "Brief b1" })).toBeTruthy();
    expect(screen.getByText("Two decisions wait for you.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Ask about Needs your decision" })).toBeTruthy();
  });

  it("lets an owner approve a proposal from its card", () => {
    setup({ briefs: [brief("b1", { proposal_ids: ["p1"] })] });
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    expect(mocks.decide.mock.calls[0]?.[0]).toEqual({ id: "p1", decision: "approve", note: undefined });
  });

  it("shows a decided proposal as read-only", () => {
    setup({ briefs: [brief("b1", { proposal_ids: ["p2"] })] });
    expect(screen.getByText("Approved")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
  });

  it("hides the decision controls from a member", () => {
    setup({ canManage: false, briefs: [brief("b1", { proposal_ids: ["p1"] })] });
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
    expect(screen.getByText("Waiting for a decision")).toBeTruthy();
  });

  it("saves the brief to the decisions knowledge base, published, as a brief source", () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Save to knowledge" }));
    expect(mocks.save).toHaveBeenCalledWith(
      expect.objectContaining({
        kb: "decisions",
        title: "Brief b1",
        content: BODY,
        source_type: "brief",
        source_id: "b1",
        publish: true,
      }),
      expect.any(Object),
    );
  });

  it("disables Save to knowledge when knowledge is not ready", () => {
    queryClient.setQueryData(knowledgeKeys.bases("ws-1"), { offered: true, state: "offline", knowledge_bases: [] });
    setup();
    expect((screen.getByRole("button", { name: "Save to knowledge" }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("Knowledge is not set up for this workspace")).toBeTruthy();
  });

  it("prefills the Secretary's chat with the section and closes the dialog", () => {
    const { onClose } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Ask about Headline" }));
    expect(mocks.chat.setSelectedAgentId).toHaveBeenCalledWith("sec-1");
    expect(mocks.chat.setActiveSession).toHaveBeenCalledWith(null);
    expect(mocks.chat.appendToInputDraft).toHaveBeenCalledWith(
      "__new__",
      expect.stringContaining('Tell me more about "Headline"'),
    );
    expect(mocks.chat.appendToInputDraft.mock.calls[0]?.[1]).toContain("Two decisions wait for you.");
    expect(mocks.chat.setOpen).toHaveBeenCalledWith(true);
    expect(onClose).toHaveBeenCalled();
  });

  it("asks for changes from the card, and only with a note", () => {
    setup({ briefs: [brief("b1", { proposal_ids: ["p1"] })] });

    fireEvent.click(screen.getByRole("button", { name: "Ask for changes" }));
    expect(mocks.decide).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toBe("Write what should change first.");

    fireEvent.change(screen.getByRole("textbox"), { target: { value: "Add the cost" } });
    fireEvent.click(screen.getByRole("button", { name: "Ask for changes" }));
    expect(mocks.decide.mock.calls[0]?.[0]).toEqual({ id: "p1", decision: "revise", note: "Add the cost" });
  });

  it("hides a proposal put aside with Decide later until its date, as the decision queue does", () => {
    queryClient.setQueryData(
      proposalKeys.detail("ws-1", "p4"),
      proposal("p4", {
        snoozed_until: new Date(Date.now() + 3 * 24 * 60 * 60 * 1000).toISOString(),
        related: [
          { kind: "decision", id: "d1", title: "Usage pricing memo", score: 0.9, state: null, issue_identifier: null, created_at: null },
        ],
      }),
    );
    setup({ briefs: [brief("b1", { proposal_ids: ["p4"] })] });

    expect(screen.queryByText("Proposal p4")).toBeNull();
    expect(screen.queryByText(/Decide later until/)).toBeNull();
    expect(screen.queryByText("Usage pricing memo")).toBeNull();
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Revision history" })).toBeNull();
  });

  it("shows a proposal again on the brief once its snooze date has passed", () => {
    queryClient.setQueryData(
      proposalKeys.detail("ws-1", "p7"),
      proposal("p7", { snoozed_until: new Date(Date.now() - 60 * 1000).toISOString() }),
    );
    setup({ briefs: [brief("b1", { proposal_ids: ["p7"] })] });

    expect(screen.getByText("Proposal p7")).toBeTruthy();
    expect(screen.queryByText(/Decide later until/)).toBeNull();
    expect(screen.getByRole("button", { name: "Approve" })).toBeTruthy();
  });

  it("shows a revising proposal with its squad and note, and no decide controls", () => {
    queryClient.setQueryData(workspaceKeys.squads("ws-1"), [{ id: "research", name: "Research", archived_at: null }] as Squad[]);
    queryClient.setQueryData(
      proposalKeys.detail("ws-1", "p5"),
      proposal("p5", { state: "revising", squad_id: "research", decision_note: "Cite the pilot" }),
    );
    setup({ briefs: [brief("b1", { proposal_ids: ["p5"] })] });

    expect(screen.getByText("Revising")).toBeTruthy();
    expect(screen.getByText("Research is revising it")).toBeTruthy();
    expect(screen.getByText("Cite the pilot")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Ask for changes" })).toBeNull();
  });

  it("shows an expired proposal as expired, with no decide controls for an owner", () => {
    queryClient.setQueryData(proposalKeys.detail("ws-1", "p6"), proposal("p6", { state: "expired" }));
    setup({ briefs: [brief("b1", { proposal_ids: ["p6"] })] });

    expect(screen.getByText("Expired")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Decide later" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Ask for changes" })).toBeNull();
  });

  describe("with no brief today", () => {
    const yesterday = new Date();
    yesterday.setDate(yesterday.getDate() - 1);
    const older = [brief("b0", { title: "Older brief", created_at: yesterday.toISOString() })];

    it("shows the empty state and runs the Secretary's autopilot from Generate now", () => {
      setup({ briefs: older });
      expect(screen.getByRole("heading", { name: "No brief today" })).toBeTruthy();
      fireEvent.click(screen.getByRole("button", { name: "Generate now" }));
      expect(mocks.trigger).toHaveBeenCalledWith("ap-1", expect.any(Object));
    });

    it("disables Generate now and says why when the Secretary has no autopilot", () => {
      queryClient.setQueryData(autopilotKeys.list("ws-1"), { autopilots: [], total: 0 });
      setup({ briefs: older });
      expect((screen.getByRole("button", { name: "Generate now" }) as HTMLButtonElement).disabled).toBe(true);
      expect(screen.getByText("The Secretary has no daily brief autopilot")).toBeTruthy();
    });

    it("lists earlier briefs and shows one when picked", () => {
      setup({ briefs: [brief("b2", { title: "Newest brief" }), ...older] });
      expect(screen.getByRole("heading", { name: "Newest brief" })).toBeTruthy();
      fireEvent.click(screen.getByRole("button", { name: /Older brief/ }));
      expect(screen.getByRole("heading", { name: "Older brief" })).toBeTruthy();
      expect(screen.queryByRole("heading", { name: "Newest brief" })).toBeNull();
    });
  });

  it("shows an opportunity's thesis on its card in the brief", () => {
    queryClient.setQueryData(
      proposalKeys.detail("ws-1", "p3"),
      proposal("p3", {
        kind: "opportunity",
        title: "Invoicing for small firms",
        payload: { thesis: "Small firms close their books late.", market: "SMB finance", evidence_doc_ids: [], target_stage: "validate" },
      }),
    );
    setup({ briefs: [brief("b1", { proposal_ids: ["p3"] })] });

    expect(screen.getByText("Small firms close their books late.")).toBeTruthy();
    expect(screen.getByText("Opportunity")).toBeTruthy();
    expect(screen.getByText("No evidence attached.")).toBeTruthy();
  });
});
