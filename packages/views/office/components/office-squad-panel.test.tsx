/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { officeKeys, proposalKeys } from "@multica/core/office";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { autopilotKeys } from "@multica/core/autopilots/queries";
import type { Agent, Squad, SquadMember } from "@multica/core/types";
import type { Proposal } from "@multica/core/office";
import { renderWithI18n } from "../../test/i18n";
import { OfficeSquadPanel } from "./office-squad-panel";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/hooks")>()),
  useActorName: () => ({
    getActorName: (type: string, id: string) => (type === "agent" ? `Agent ${id}` : `Member ${id}`),
  }),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span data-testid="avatar" /> }));

const SQUAD = { id: "sq", name: "Research", description: "Finds the market gaps", leader_id: "ada" } as Squad;

const MEMBERS: SquadMember[] = [
  { id: "m1", squad_id: "sq", member_type: "agent", member_id: "ada", role: "lead", created_at: "" },
  { id: "m2", squad_id: "sq", member_type: "agent", member_id: "bob", role: "", created_at: "" },
  { id: "m3", squad_id: "sq", member_type: "member", member_id: "human", role: "", created_at: "" },
];

const PROPOSAL = {
  id: "p1",
  squad_id: "sq",
  issue_identifier: "MUL-9",
  title: "Enter the EU market",
  state: "pending",
} as Proposal;

describe("OfficeSquadPanel", () => {
  afterEach(cleanup);

  function setup(props: Partial<Parameters<typeof OfficeSquadPanel>[0]> = {}) {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(workspaceKeys.squads("ws-1"), [SQUAD]);
    queryClient.setQueryData(workspaceKeys.agents("ws-1"), [
      { id: "ada", name: "Ada", home_squad_id: "sq" } as Agent,
      { id: "bob", name: "Bob", home_squad_id: "sq" } as Agent,
    ]);
    queryClient.setQueryData(officeKeys.squadMembers("ws-1", "sq"), MEMBERS);
    queryClient.setQueryData(workspaceKeys.squadMemberStatus("ws-1", "sq"), {
      members: [
        { member_type: "agent", member_id: "ada", status: "working", active_issues: [], last_active_at: null },
        { member_type: "agent", member_id: "bob", status: "offline", active_issues: [], last_active_at: null },
      ],
    });
    queryClient.setQueryData(autopilotKeys.list("ws-1"), {
      autopilots: [
        {
          id: "ap1",
          title: "Weekly scan",
          status: "active",
          assignee_type: "squad",
          assignee_id: "sq",
        },
      ],
    });
    queryClient.setQueryData(proposalKeys.listFiltered("ws-1", { state: "pending", squad_id: "sq" }), [PROPOSAL]);
    const onPickAgent = vi.fn();
    const onReview = vi.fn();
    const onClose = vi.fn();
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <OfficeSquadPanel
          squadId="sq"
          room={{ slot: 0, squad: SQUAD, agentIds: ["ada"], overflow: 2 }}
          onPickAgent={onPickAgent}
          onReview={onReview}
          onClose={onClose}
          {...props}
        />
      </QueryClientProvider>,
    );
    return { onPickAgent, onReview, onClose };
  }

  it("shows the squad's mission and its members with their state", () => {
    setup();

    expect(screen.getByRole("heading", { name: "Research" })).toBeTruthy();
    expect(screen.getByText("Finds the market gaps")).toBeTruthy();
    const members = screen.getByRole("region", { name: "Members" });
    expect(within(members).getByText("Agent ada")).toBeTruthy();
    expect(within(members).getByText("Working")).toBeTruthy();
    expect(within(members).getByText("Member human")).toBeTruthy();
  });

  it("marks an agent with a home here but no desk, and counts the overflow", () => {
    setup();

    const members = screen.getByRole("region", { name: "Members" });
    expect(within(members).getByText("No desk")).toBeTruthy();
    expect(within(members).getByText("+2")).toBeTruthy();
  });

  it("opens an agent's panel from its row", () => {
    const { onPickAgent } = setup();

    fireEvent.click(screen.getByRole("button", { name: /Agent bob/ }));

    expect(onPickAgent).toHaveBeenCalledWith("bob");
  });

  it("lists the autopilots and the pending proposals, and opens the decisions from a proposal", () => {
    const { onReview } = setup();

    expect(screen.getByRole("region", { name: "Autopilots" }).textContent).toContain("Weekly scan");
    fireEvent.click(screen.getByRole("button", { name: /Enter the EU market/ }));

    expect(onReview).toHaveBeenCalled();
  });
});
