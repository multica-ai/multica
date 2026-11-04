/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { issueKeys } from "@multica/core/issues/queries";
import type { Proposal, ProposalKind } from "@multica/core/company";
import type { Squad } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { CompanyDecisionsDialog } from "./company-decisions-dialog";

const api = vi.hoisted(() => ({
  getInitiative: vi.fn(),
  listProposalRevisions: vi.fn(),
}));
const decide = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/company", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/company")>()),
  useDecideProposal: () => ({ mutate: decide.mutate, isPending: false }),
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
vi.mock("../../navigation", () => ({
  AppLink: ({ children, ...props }: React.ComponentProps<"a">) => <a {...props}>{children}</a>,
}));
vi.mock("../../editor/readonly-content", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
}));

const SQUADS = [{ id: "research", name: "Research", archived_at: null }] as Squad[];

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
    kind: "decision" as ProposalKind,
    payload: {},
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

const INITIATIVE_DETAIL = {
  initiative: { id: "in-1", identifier: "INI-1", title: "Invoicing", stage_key: "validate" },
  artifacts: [
    { kind: "validation_report", stage_key: "validate", required: true, knowledge_link_id: "kl-1", waived: false, waived_note: "" },
    { kind: "competitor_profile", stage_key: "validate", required: true, knowledge_link_id: null, waived: false, waived_note: "" },
  ],
  events: [
    { id: "ev-2", kind: "attested", from_stage: "validate", to_stage: null, actor_type: "agent", actor_id: "a-1", note: "Ready for review", created_at: "2026-10-30T00:00:00Z" },
  ],
  gates: [],
};

function renderDialog(proposals: Proposal[], focusId: string, canDecide = true) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(issueKeys.detail("ws-1", "issue-opp"), { description: "" });
  queryClient.setQueryData(issueKeys.detail("ws-1", "issue-gate"), { description: "" });
  queryClient.setQueryData(issueKeys.detail("ws-1", "issue-odd"), { description: "" });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <CompanyDecisionsDialog open proposals={proposals} squads={SQUADS} canDecide={canDecide} focusId={focusId} onClose={vi.fn()} />
    </QueryClientProvider>,
  );
}

describe("CompanyDecisionsDialog kinds", () => {
  beforeEach(() => {
    decide.mutate.mockReset();
    api.getInitiative.mockReset().mockResolvedValue(INITIATIVE_DETAIL);
    api.listProposalRevisions.mockReset().mockResolvedValue([]);
  });
  afterEach(cleanup);

  it("shows an opportunity's thesis, market and evidence links", async () => {
    renderDialog(
      [
        proposal("opp", {
          kind: "opportunity",
          title: "Invoicing for small firms",
          payload: {
            title: "Invoicing",
            thesis: "Small firms close their books late.",
            market: "SMB finance",
            evidence_doc_ids: ["doc-1", "doc-2"],
            target_stage: "validate",
          },
        }),
      ],
      "opp",
    );

    expect(await screen.findByText("Small firms close their books late.")).toBeTruthy();
    expect(screen.getByText("SMB finance")).toBeTruthy();
    expect(screen.getByText("Opportunity")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Document 1" }).getAttribute("href")).toBe("/acme/knowledge");
    expect(screen.getByRole("link", { name: "Document 2" })).toBeTruthy();
    expect(screen.getByText("Starts at Validate")).toBeTruthy();
  });

  it("shows a stage gate's artifact checklist and the leader's attestation", async () => {
    renderDialog(
      [proposal("gate", { kind: "stage_gate", title: "Ready to define", payload: { initiative_id: "in-1", from: "validate", to: "define", artifact_ids: [] } })],
      "gate",
    );

    expect(await screen.findByText("Validate → Define")).toBeTruthy();
    expect(screen.getByText("Validation report")).toBeTruthy();
    expect(screen.getByText("Published")).toBeTruthy();
    expect(screen.getByText("Competitor profile")).toBeTruthy();
    expect(screen.getByText("Missing")).toBeTruthy();
    expect(await screen.findByText(/Attested by agent a-1/)).toBeTruthy();
    expect(screen.getByText("Ready for review")).toBeTruthy();
    expect(api.getInitiative).toHaveBeenCalledWith("in-1");
  });

  it("says so when a stage gate's initiative cannot be loaded", async () => {
    api.getInitiative.mockRejectedValue(new Error("network"));
    renderDialog(
      [proposal("gate", { kind: "stage_gate", payload: { initiative_id: "in-1", from: "validate", to: "define" } })],
      "gate",
    );
    expect(await screen.findByText("The initiative could not be loaded.")).toBeTruthy();
  });

  it("keeps a kind this client does not know read-only", async () => {
    renderDialog([proposal("odd", { kind: "unknown", title: "A newer kind" })], "odd");

    expect(await screen.findByText("Other")).toBeTruthy();
    expect(screen.getByText("A newer kind", { selector: "h3" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Reject" })).toBeNull();
  });

  it("still lets the boss decide a plain decision", async () => {
    renderDialog([proposal("plain")], "plain");
    expect(await screen.findByRole("button", { name: "Approve" })).toBeTruthy();
  });
});
