// The assignee picker lists only actors the current user can actually pick:
// agents they may invoke that have a runtime, and squads whose leader is one.
import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enIssues from "../../../locales/en/issues.json";
import { useAssigneePicksStore } from "@multica/core/issues/stores/assignee-picks-store";
import { AssigneePicker, canAssignAgent } from "./assignee-picker";

const MEMBERS = [{ user_id: "user-1", name: "Ada Lovelace", role: "member" }];
const MEMBER_GRANT = [{ target_type: "member", target_id: "user-1" }];
const WORKSPACE_GRANT = [{ target_type: "workspace", target_id: "workspace-1" }];
const AGENTS = [
  // Workspace-shared, has a runtime: pickable.
  { id: "agent-shared", name: "Shared", owner_id: "user-2", permission_mode: "public_to", invocation_targets: WORKSPACE_GRANT, runtime_id: "rt-1", archived_at: null, visibility: "workspace" },
  // Own private agent: pickable, keeps the lock.
  { id: "agent-mine", name: "Mine", owner_id: "user-1", permission_mode: "private", invocation_targets: [], runtime_id: "rt-1", archived_at: null, visibility: "private" },
  // Someone else's private agent: hidden.
  { id: "agent-theirs", name: "Theirs", owner_id: "user-2", permission_mode: "private", invocation_targets: [], runtime_id: "rt-1", archived_at: null, visibility: "private" },
  // Granted to this user alone: pickable.
  { id: "agent-granted", name: "Granted", owner_id: "user-2", permission_mode: "public_to", invocation_targets: MEMBER_GRANT, runtime_id: "rt-1", archived_at: null, visibility: "private" },
  // Shared but no runtime: hidden.
  { id: "agent-unbound", name: "Unbound", owner_id: "user-2", permission_mode: "public_to", invocation_targets: WORKSPACE_GRANT, runtime_id: null, archived_at: null, visibility: "workspace" },
];
const SQUADS = [
  { id: "squad-ok", name: "Runnable Squad", archived_at: null, leader_id: "agent-shared" },
  { id: "squad-stuck", name: "Stuck Squad", archived_at: null, leader_id: "agent-unbound" },
  // Leader has a runtime but is someone else's private agent: the server
  // rejects the pick, so it's hidden too.
  { id: "squad-private", name: "Private Squad", archived_at: null, leader_id: "agent-theirs" },
];
const PICKS = [
  ...Array.from({ length: 9 }, () => ({ type: "agent" as const, id: "agent-theirs" })),
  ...Array.from({ length: 8 }, () => ({ type: "squad" as const, id: "squad-stuck" })),
  { type: "agent" as const, id: "agent-shared" },
];

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    if (queryKey[0] === "members") return { data: MEMBERS };
    if (queryKey[0] === "agents") return { data: AGENTS };
    if (queryKey[0] === "squads") return { data: SQUADS };
    return { data: [] };
  },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "user-1" } };
  return {
    useAuthStore: Object.assign(
      (selector?: (s: typeof state) => unknown) => (selector ? selector(state) : state),
      { getState: () => state },
    ),
  };
});
vi.mock("@multica/core/agents", () => ({
  isAgentRuntimeBound: (a: { runtime_id: string | null }) => !!a.runtime_id,
}));
vi.mock("@multica/core/permissions", async () => ({
  canAssignAgentToIssue: (await import("@multica/core/permissions/rules")).canAssignAgentToIssue,
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Ada Lovelace" }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
  squadListOptions: () => ({ queryKey: ["squads"] }),
}));
vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

function rowsIn(label: string): string[] {
  const section = screen.getByText(label).parentElement as HTMLElement;
  return Array.from(section.querySelectorAll("[data-picker-item]")).map(
    (el) => el.textContent ?? "",
  );
}

function renderPicker() {
  render(
    <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
      <AssigneePicker
        assigneeType={null}
        assigneeId={null}
        onUpdate={() => {}}
        open
        onOpenChange={() => {}}
      />
    </I18nProvider>,
  );
}

describe("AssigneePicker unpickable actors", () => {
  afterEach(() => {
    MEMBERS[0]!.role = "member";
  });

  it("hides agents and squads the user cannot assign, including from Recent", async () => {
    useAssigneePicksStore.setState({ byScope: { "user-1:workspace-1": PICKS } });
    renderPicker();

    await screen.findByText("Agents");
    expect(rowsIn("Agents")).toEqual(["Shared", "Mine", "Granted"]);
    expect(rowsIn("Squads")).toEqual(["Runnable Squad"]);
    expect(rowsIn("Recent")).toEqual(["Shared"]);
    expect(screen.queryByText("Theirs")).toBeNull();
    expect(screen.queryByText("Unbound")).toBeNull();
    expect(screen.queryByText("Stuck Squad")).toBeNull();
    expect(screen.queryByText("Private Squad")).toBeNull();
  });

  it("offers a guest no agent or squad, not even one it owns or was granted (MUL-9877)", async () => {
    MEMBERS[0]!.role = "guest";
    useAssigneePicksStore.setState({ byScope: { "user-1:workspace-1": PICKS } });
    renderPicker();

    await screen.findByText("Ada Lovelace");
    for (const name of ["Shared", "Mine", "Granted", "Runnable Squad"]) {
      expect(screen.queryByText(name)).toBeNull();
    }
    for (const agent of AGENTS) {
      expect(canAssignAgent(agent as never, "user-1", "guest")).toBe(false);
    }
    expect(canAssignAgent(AGENTS[1] as never, "user-1", "member")).toBe(true);
  });
});
