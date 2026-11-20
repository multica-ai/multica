// The assignee picker lists only actors the current user can actually pick:
// agents they may invoke that have a runtime, and squads whose leader has one.
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enIssues from "../../../locales/en/issues.json";
import { AssigneePicker } from "./assignee-picker";

const MEMBERS = [{ user_id: "user-1", name: "Ada Lovelace", role: "member" }];
const WORKSPACE_GRANT = [{ target_type: "workspace", target_id: "workspace-1" }];
const AGENTS = [
  // Workspace-shared, has a runtime: pickable.
  { id: "agent-shared", name: "Shared", owner_id: "user-2", permission_mode: "public_to", invocation_targets: WORKSPACE_GRANT, runtime_id: "rt-1", archived_at: null, visibility: "workspace" },
  // Own private agent: pickable, keeps the lock.
  { id: "agent-mine", name: "Mine", owner_id: "user-1", permission_mode: "private", invocation_targets: [], runtime_id: "rt-1", archived_at: null, visibility: "private" },
  // Someone else's private agent: hidden.
  { id: "agent-theirs", name: "Theirs", owner_id: "user-2", permission_mode: "private", invocation_targets: [], runtime_id: "rt-1", archived_at: null, visibility: "private" },
  // Shared but no runtime: hidden.
  { id: "agent-unbound", name: "Unbound", owner_id: "user-2", permission_mode: "public_to", invocation_targets: WORKSPACE_GRANT, runtime_id: null, archived_at: null, visibility: "workspace" },
];
const SQUADS = [
  { id: "squad-ok", name: "Runnable Squad", archived_at: null, leader_id: "agent-shared" },
  { id: "squad-stuck", name: "Stuck Squad", archived_at: null, leader_id: "agent-unbound" },
];
const FREQUENCY = [
  { assignee_type: "agent", assignee_id: "agent-theirs", frequency: 9 },
  { assignee_type: "squad", assignee_id: "squad-stuck", frequency: 8 },
  { assignee_type: "agent", assignee_id: "agent-shared", frequency: 1 },
];

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    if (queryKey[0] === "members") return { data: MEMBERS };
    if (queryKey[0] === "agents") return { data: AGENTS };
    if (queryKey[0] === "squads") return { data: SQUADS };
    if (queryKey[0] === "frequency") return { data: FREQUENCY };
    return { data: [] };
  },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: () => ({ id: "user-1" }) }));
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
  assigneeFrequencyOptions: () => ({ queryKey: ["frequency"] }),
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

describe("AssigneePicker unpickable actors", () => {
  it("hides agents and squads the user cannot assign, including from Recent", async () => {
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

    await screen.findByText("Agents");
    expect(rowsIn("Agents")).toEqual(["Shared", "Mine"]);
    expect(rowsIn("Squads")).toEqual(["Runnable Squad"]);
    expect(rowsIn("Recent")).toEqual(["Shared"]);
    expect(screen.queryByText("Theirs")).toBeNull();
    expect(screen.queryByText("Unbound")).toBeNull();
    expect(screen.queryByText("Stuck Squad")).toBeNull();
  });
});
