// The assignee picker's Recent section: the user's three most-assigned actors,
// counted on this device by `useAssigneeUsageStore`. Uses the real store and
// PropertyPicker so recording and rendering are tested together.
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { useAssigneeUsageStore } from "@multica/core/issues/stores";
import enIssues from "../../../locales/en/issues.json";
import { AssigneePicker } from "./assignee-picker";

const WS_ID = "workspace-1";

const MEMBERS = [
  { user_id: "user-1", name: "Ada Lovelace", role: "member" },
  { user_id: "user-2", name: "Grace Hopper", role: "member" },
];
const AGENTS = [
  { id: "agent-1", name: "Builder", archived_at: null, visibility: "workspace" },
  { id: "agent-2", name: "Retired", archived_at: "2026-01-01T00:00:00Z", visibility: "workspace" },
];
const SQUADS = [{ id: "squad-1", name: "Core Squad", archived_at: null, leader_id: "agent-1" }];

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    if (queryKey[0] === "members") return { data: MEMBERS };
    if (queryKey[0] === "agents") return { data: AGENTS };
    if (queryKey[0] === "squads") return { data: SQUADS };
    return { data: [] };
  },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: () => ({ id: "user-1" }) }));
vi.mock("@multica/core/agents", () => ({ isAgentRuntimeBound: () => true }));
vi.mock("@multica/core/permissions", () => ({
  canAssignAgentToIssue: () => ({ allowed: true }),
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

function renderPicker(onUpdate: () => void = () => {}) {
  return render(
    <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
      <AssigneePicker
        assigneeType={null}
        assigneeId={null}
        onUpdate={onUpdate}
        open
        onOpenChange={() => {}}
      />
    </I18nProvider>,
  );
}

function recentSection() {
  const label = screen.queryByText("Recent");
  return label?.parentElement ?? null;
}

function recentNames() {
  const section = recentSection();
  if (!section) return [];
  return within(section)
    .getAllByRole("button")
    .map((row) => row.textContent);
}

beforeEach(() => {
  useAssigneeUsageStore.setState({ byWorkspace: {} });
});

describe("AssigneePicker Recent section", () => {
  it("is absent when this browser has no assignment history", async () => {
    renderPicker();
    await screen.findByText("Members");
    expect(screen.queryByText("Recent")).toBeNull();
  });

  it("lists the top three by count, skipping archived actors", async () => {
    const record = useAssigneeUsageStore.getState().recordAssignment;
    for (let i = 0; i < 5; i++) record(WS_ID, { type: "agent", id: "agent-2" });
    for (let i = 0; i < 4; i++) record(WS_ID, { type: "member", id: "user-2" });
    for (let i = 0; i < 3; i++) record(WS_ID, { type: "squad", id: "squad-1" });
    for (let i = 0; i < 2; i++) record(WS_ID, { type: "agent", id: "agent-1" });
    record(WS_ID, { type: "member", id: "user-1" });

    renderPicker();
    await screen.findByText("Recent");

    expect(recentNames()).toEqual(["Grace Hopper", "Core Squad", "Builder"]);
  });

  it("hides while searching", async () => {
    useAssigneeUsageStore.getState().recordAssignment(WS_ID, { type: "member", id: "user-2" });
    const user = userEvent.setup();

    renderPicker();
    await screen.findByText("Recent");
    await user.type(screen.getByPlaceholderText("Assign to..."), "grace");

    expect(screen.queryByText("Recent")).toBeNull();
  });

  it("records a pick so the assignee shows up in Recent", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();

    const { unmount } = renderPicker(onUpdate);
    await user.click(await screen.findByText("Grace Hopper"));
    expect(onUpdate).toHaveBeenCalledWith({ assignee_type: "member", assignee_id: "user-2" });
    unmount();

    renderPicker();
    await screen.findByText("Recent");
    expect(recentNames()).toEqual(["Grace Hopper"]);
  });

  it("does not count unassigning", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();

    renderPicker(onUpdate);
    // The trigger also reads "Unassigned"; the picker row renders after it.
    const rows = await screen.findAllByText("Unassigned");
    await user.click(rows[rows.length - 1]!);

    expect(onUpdate).toHaveBeenCalledWith({ assignee_type: null, assignee_id: null });

    expect(useAssigneeUsageStore.getState().byWorkspace[WS_ID]).toBeUndefined();
  });
});
