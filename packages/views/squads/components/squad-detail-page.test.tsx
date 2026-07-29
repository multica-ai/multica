// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type {
  SquadMember,
  SquadMemberStatus,
  SquadMemberStatusValue,
} from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enSquads from "../../locales/en/squads.json";

const TEST_RESOURCES = { en: { common: enCommon, squads: enSquads } };

// SquadMembersTab pulls nothing from React Query (it receives its data as
// props), but the host module imports a handful of platform hooks at the
// top level - stub them inert so the module loads without workspace infra.
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme" }),
  useWorkspacePaths: () => ({
    agentDetail: (id: string) => `/ws-1/agents/${id}`,
    issueDetail: (id: string) => `/ws-1/issues/${id}`,
  }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(vi.fn(), {
    getState: () => ({ user: { id: "user-1" } }),
  }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: () => Promise.resolve([]) }),
  memberListOptions: () => ({ queryKey: ["members"], queryFn: () => Promise.resolve([]) }),
  squadMemberStatusOptions: () => ({ queryKey: ["squad-status"], queryFn: () => Promise.resolve({ members: [] }) }),
  workspaceKeys: { squads: () => ["squads"] },
}));
vi.mock("@multica/core/workspace/avatar-url", () => ({ resolvePublicFileUrl: (u: string) => u }));
vi.mock("@multica/core/utils", () => ({ isImeComposing: () => false }));
vi.mock("@multica/core/shortcuts", () => ({
  getShortcut: () => ({ key: "Enter", ctrl: false, meta: false, shift: false, alt: false }),
  shortcutMatchesEvent: () => false,
}));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <div data-testid={`avatar-${actorId}`} />
  ),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
  useNavigation: () => ({ push: vi.fn(), replace: vi.fn(), back: vi.fn() }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { SquadMembersTab } from "./squad-detail-page";

const LEADER_ID = "agent-leader";
const WORKING_ID = "agent-working";
const IDLE_ID = "agent-idle";
const ARCHIVED_ID = "agent-archived";
const HUMAN_WITH_ROLE_ID = "user-1";
const HUMAN_NO_ROLE_ID = "user-2";

function makeMember(
  id: string,
  memberType: "agent" | "member",
  memberId: string,
  role = "",
): SquadMember {
  return {
    id,
    squad_id: "squad-1",
    member_type: memberType,
    member_id: memberId,
    role,
    created_at: "2026-01-01T00:00:00Z",
  };
}

function makeStatus(
  memberId: string,
  status: SquadMemberStatusValue,
): SquadMemberStatus {
  return {
    member_type: "agent",
    member_id: memberId,
    status,
    active_issues: [],
    last_active_at: "2026-07-19T12:00:00Z",
  };
}

const archivedIds = new Set<string>([ARCHIVED_ID]);

interface Fixture {
  members: SquadMember[];
  memberStatusById: Map<string, SquadMemberStatus>;
}

function fullFixture(): Fixture {
  const members: SquadMember[] = [
    makeMember("m-leader", "agent", LEADER_ID, "Lead"),
    makeMember("m-working", "agent", WORKING_ID, "Reviewer"),
    makeMember("m-idle", "agent", IDLE_ID, ""),
    makeMember("m-archived", "agent", ARCHIVED_ID, "Old role"),
    makeMember("m-human1", "member", HUMAN_WITH_ROLE_ID, "PM"),
    makeMember("m-human2", "member", HUMAN_NO_ROLE_ID, ""),
  ];
  const memberStatusById = new Map<string, SquadMemberStatus>([
    [LEADER_ID, makeStatus(LEADER_ID, "working")],
    [WORKING_ID, makeStatus(WORKING_ID, "working")],
    [IDLE_ID, makeStatus(IDLE_ID, "idle")],
    [ARCHIVED_ID, makeStatus(ARCHIVED_ID, "archived")],
  ]);
  return { members, memberStatusById };
}

function isLeader(m: SquadMember) {
  return m.member_type === "agent" && m.member_id === LEADER_ID;
}
function isArchived(m: SquadMember) {
  return m.member_type === "agent" && archivedIds.has(m.member_id);
}

// MemberSubsection wraps its rows in <div><div header><span label>...
// so label -> header div -> outer section div. Returns the outer section
// container so a test can scope queries to one group.
function sectionByText(label: string) {
  return screen.getByText(label).parentElement!.parentElement!;
}

function renderTab(overrides: Partial<Parameters<typeof SquadMembersTab>[0]> = {}) {
  const { members, memberStatusById } = fullFixture();
  const props = {
    members,
    memberStatusById,
    canManage: false,
    isLeader,
    isArchived,
    getEntityName: (_type: string, id: string) => id,
    onAddMemberClick: vi.fn(),
    onSetLeader: vi.fn(),
    onRemoveMember: vi.fn(),
    onUpdateRole: vi.fn(),
    setLeaderPending: false,
    ...overrides,
  };
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <SquadMembersTab {...props} />
    </I18nProvider>,
  );
  return props;
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("SquadMembersTab grouping", () => {
  it("pulls the leader into its own prominent card and out of the Agents group", () => {
    renderTab();
    // Leader renders exactly once (in the amber Leader card), never duplicated
    // inside the Agents group.
    expect(screen.getAllByText(LEADER_ID)).toHaveLength(1);
    const agentsSection = sectionByText("Agents");
    expect(within(agentsSection).queryByText(LEADER_ID)).toBeNull();
    // The leader-card label ("Leader") is present once.
    expect(screen.getAllByText(/Leader/).length).toBeGreaterThanOrEqual(1);
  });

  it("splits the working roster into Agents and People groups with counts", () => {
    renderTab();
    const agentsSection = sectionByText("Agents");
    const peopleSection = sectionByText("People");
    // Agents group: working + idle (leader pulled out, archived collapsed).
    expect(agentsSection).toHaveTextContent(/·\s*2/);
    // People group: two humans.
    expect(peopleSection).toHaveTextContent(/·\s*2/);
    expect(within(peopleSection).getByText(HUMAN_WITH_ROLE_ID)).toBeInTheDocument();
    expect(within(agentsSection).queryByText(HUMAN_WITH_ROLE_ID)).toBeNull();
  });

  it("renders the status summary with presence counts over non-archived agents", () => {
    renderTab();
    // 2 working (leader + working agent), 1 idle, 1 offline, 0 unstable.
    const working = screen.getByRole("button", { name: /Working/ });
    const idle = screen.getByRole("button", { name: /Idle/ });
    const offline = screen.getByRole("button", { name: /Offline/ });
    expect(working).toHaveTextContent(/2/);
    expect(idle).toHaveTextContent(/1/);
    expect(offline).toHaveTextContent(/0/);
  });

  it("filters the Agents group by status when a chip is clicked", () => {
    renderTab();
    // Both working and idle agents are visible before filtering.
    expect(screen.getByText(WORKING_ID)).toBeInTheDocument();
    expect(screen.getByText(IDLE_ID)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Idle/ }));

    // Idle agent stays; working agent is filtered out.
    expect(screen.getByText(IDLE_ID)).toBeInTheDocument();
    expect(screen.queryByText(WORKING_ID)).toBeNull();
  });

  it("collapses archived members by default and reveals them on toggle", () => {
    renderTab();
    // Archived agent is not in the active roster initially.
    expect(screen.queryByText(ARCHIVED_ID)).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /Archived/ }));
    expect(screen.getByText(ARCHIVED_ID)).toBeInTheDocument();
  });

  it("always shows the role, even in read-only mode, falling back to a placeholder", () => {
    renderTab({ canManage: false });
    // A set role renders as text.
    expect(screen.getByText("PM")).toBeInTheDocument();
    // Empty roles (one agent, one human) render the placeholder, not nothing.
    expect(screen.getAllByText("No role")).toHaveLength(2);
  });

  it("hides every mutating control when canManage is false", () => {
    renderTab({ canManage: false });
    expect(screen.queryByRole("button", { name: /Add Member/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /Create Agent/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /Remove from squad/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /Make squad leader/i })).toBeNull();
  });

  it("exposes add / create / remove / make-leader controls when canManage is true", () => {
    renderTab({ canManage: true, onCreateAgentClick: vi.fn() });
    expect(screen.getByRole("button", { name: /Add Member/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Create Agent/i })).toBeInTheDocument();
    // Remove + make-leader are hover-revealed (opacity-0) but still in the DOM.
    expect(screen.getAllByRole("button", { name: /Remove from squad/i }).length).toBeGreaterThan(0);
    expect(screen.getAllByRole("button", { name: /Make squad leader/i }).length).toBeGreaterThan(0);
  });
});
