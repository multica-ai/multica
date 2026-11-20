// The assignee picker's Recent group: the current user's top 3 assignees from
// the server-side assignee-frequency counts.
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enIssues from "../../../locales/en/issues.json";
import { AssigneePicker } from "./assignee-picker";

const MEMBERS = [
  { user_id: "user-1", name: "Ada Lovelace", role: "member" },
  { user_id: "user-2", name: "Grace Hopper", role: "member" },
];
const AGENTS = [
  { id: "agent-1", name: "Builder", archived_at: null, visibility: "workspace" },
  { id: "agent-2", name: "Retired", archived_at: "2026-01-01T00:00:00Z", visibility: "workspace" },
];
const SQUADS = [{ id: "squad-1", name: "Platform", archived_at: null, leader_id: "agent-1" }];

type FrequencyEntry = { assignee_type: string; assignee_id: string; frequency: number };
let frequency: FrequencyEntry[] = [];

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    if (queryKey[0] === "members") return { data: MEMBERS };
    if (queryKey[0] === "agents") return { data: AGENTS };
    if (queryKey[0] === "squads") return { data: SQUADS };
    if (queryKey[0] === "frequency") return { data: frequency };
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

function picker(onUpdate: () => void) {
  return (
    <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
      <AssigneePicker
        assigneeType={null}
        assigneeId={null}
        onUpdate={onUpdate}
        open
        onOpenChange={() => {}}
      />
    </I18nProvider>
  );
}

function renderPicker(onUpdate: () => void = () => {}) {
  return render(picker(onUpdate));
}

/** Row names inside the Recent section, in display order. */
async function recentRows(): Promise<string[]> {
  const label = await screen.findByText("Recent");
  const section = label.parentElement as HTMLElement;
  return Array.from(section.querySelectorAll("[data-picker-item]")).map(
    (el) => el.textContent ?? "",
  );
}

describe("AssigneePicker Recent group", () => {
  beforeEach(() => {
    frequency = [];
  });

  it("is hidden without frequency data", async () => {
    renderPicker();
    await screen.findByText("Members");
    expect(screen.queryByText("Recent")).toBeNull();
  });

  it("shows the top 3 by count, highest first, between Unassigned and Members", async () => {
    frequency = [
      { assignee_type: "member", assignee_id: "user-1", frequency: 2 },
      { assignee_type: "squad", assignee_id: "squad-1", frequency: 9 },
      { assignee_type: "member", assignee_id: "user-2", frequency: 1 },
      { assignee_type: "agent", assignee_id: "agent-1", frequency: 5 },
    ];
    renderPicker();

    expect(await recentRows()).toEqual(["Platform", "Builder", "Ada Lovelace"]);
    // Recent sits between the Unassigned row and Members.
    const follows = (a: Node, b: Node) =>
      Boolean(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING);
    const unassignedRow = screen.getAllByText("Unassigned").at(-1)!;
    const recentLabel = screen.getByText("Recent");
    expect(follows(unassignedRow, recentLabel)).toBe(true);
    expect(follows(recentLabel, screen.getByText("Members"))).toBe(true);
    // The same actors still appear in their own groups.
    expect(screen.getAllByText("Platform")).toHaveLength(2);
  });

  it("skips archived and removed actors and zero counts, letting the next one move up", async () => {
    frequency = [
      { assignee_type: "agent", assignee_id: "agent-2", frequency: 10 }, // archived
      { assignee_type: "member", assignee_id: "user-gone", frequency: 8 }, // removed
      { assignee_type: "squad", assignee_id: "squad-gone", frequency: 7 }, // deleted
      { assignee_type: "member", assignee_id: "user-2", frequency: 3 },
      { assignee_type: "member", assignee_id: "user-1", frequency: 0 },
    ];
    renderPicker();

    expect(await recentRows()).toEqual(["Grace Hopper"]);
  });

  it("assigns from a Recent row", async () => {
    frequency = [{ assignee_type: "agent", assignee_id: "agent-1", frequency: 4 }];
    const user = userEvent.setup();
    const onUpdate = vi.fn();
    renderPicker(onUpdate);

    const section = (await screen.findByText("Recent")).parentElement as HTMLElement;
    await user.click(within(section).getByText("Builder"));

    expect(onUpdate).toHaveBeenCalledWith({ assignee_type: "agent", assignee_id: "agent-1" });
  });

  it("is hidden while searching so Enter picks the first match", async () => {
    frequency = [{ assignee_type: "member", assignee_id: "user-1", frequency: 6 }];
    const user = userEvent.setup();
    const onUpdate = vi.fn();
    renderPicker(onUpdate);

    await screen.findByText("Recent");
    await user.type(screen.getByPlaceholderText("Assign to..."), "grace");
    expect(screen.queryByText("Recent")).toBeNull();

    await user.keyboard("{Enter}");
    expect(onUpdate).toHaveBeenCalledWith({ assignee_type: "member", assignee_id: "user-2" });
  });

  it("keeps the keyboard highlight on the same row when Recent loads late", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();
    const { rerender } = renderPicker(onUpdate);

    await screen.findByText("Members");
    // Unassigned, then Ada Lovelace.
    await user.click(screen.getByPlaceholderText("Assign to..."));
    await user.keyboard("{ArrowDown}{ArrowDown}");

    // The frequency response arrives and Recent is inserted above Members.
    frequency = [{ assignee_type: "agent", assignee_id: "agent-1", frequency: 4 }];
    rerender(picker(onUpdate));
    await screen.findByText("Recent");

    await user.keyboard("{Enter}");
    expect(onUpdate).toHaveBeenCalledWith({ assignee_type: "member", assignee_id: "user-1" });
  });
});
