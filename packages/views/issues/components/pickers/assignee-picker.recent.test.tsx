// The assignee picker's Recent group and per-group order: ranked from this
// user's local pick queue (core's assignee-picks-store), fed by manual picks.
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enIssues from "../../../locales/en/issues.json";
import {
  useAssigneePicksStore,
  type AssigneePick,
} from "@multica/core/issues/stores/assignee-picks-store";
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

const SCOPE = "user-1:workspace-1";

/** Seed the queue: each `[pick, n]` is appended n times, in order. */
function seed(...entries: [AssigneePick, number][]) {
  const queue = entries.flatMap(([pick, n]) => Array.from({ length: n }, () => pick));
  useAssigneePicksStore.setState({ byScope: { [SCOPE]: queue } });
}

const queue = () => useAssigneePicksStore.getState().byScope[SCOPE] ?? [];

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
}));
vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

type PickerProps = Partial<React.ComponentProps<typeof AssigneePicker>>;

function picker(onUpdate: () => void, props: PickerProps = {}) {
  return (
    <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
      <AssigneePicker
        assigneeType={null}
        assigneeId={null}
        onUpdate={onUpdate}
        open
        onOpenChange={() => {}}
        {...props}
      />
    </I18nProvider>
  );
}

function renderPicker(onUpdate: () => void = () => {}, props: PickerProps = {}) {
  return render(picker(onUpdate, props));
}

/** Row names inside a labelled section, in display order. */
function sectionRows(label: string): string[] {
  const section = screen.getByText(label).parentElement as HTMLElement;
  return Array.from(section.querySelectorAll("[data-picker-item]")).map(
    (el) => el.textContent ?? "",
  );
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
    useAssigneePicksStore.setState({ byScope: {} });
  });

  it("is hidden without local picks", async () => {
    renderPicker();
    await screen.findByText("Members");
    expect(screen.queryByText("Recent")).toBeNull();
  });

  it("shows the top 3 by count, highest first, between Unassigned and Members", async () => {
    seed(
      [{ type: "member", id: "user-1" }, 2],
      [{ type: "squad", id: "squad-1" }, 9],
      [{ type: "member", id: "user-2" }, 1],
      [{ type: "agent", id: "agent-1" }, 5],
    );
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

  it("skips archived and removed actors, letting the next one move up", async () => {
    seed(
      [{ type: "agent", id: "agent-2" }, 10], // archived
      [{ type: "member", id: "user-gone" }, 8], // removed
      [{ type: "squad", id: "squad-gone" }, 7], // deleted
      [{ type: "member", id: "user-2" }, 3],
    );
    renderPicker();

    expect(await recentRows()).toEqual(["Grace Hopper"]);
  });

  it("orders each group by its own counts, default order on ties", async () => {
    seed([{ type: "member", id: "user-2" }, 2]);
    renderPicker();

    await screen.findByText("Members");
    expect(sectionRows("Members")).toEqual(["Grace Hopper", "Ada Lovelace"]);
  });

  it("ignores another account's or workspace's picks", async () => {
    useAssigneePicksStore.setState({
      byScope: {
        "user-2:workspace-1": [{ type: "member", id: "user-2" }],
        "user-1:workspace-2": [{ type: "member", id: "user-2" }],
      },
    });
    renderPicker();

    await screen.findByText("Members");
    expect(screen.queryByText("Recent")).toBeNull();
    expect(sectionRows("Members")).toEqual(["Ada Lovelace", "Grace Hopper"]);
  });

  it("assigns from a Recent row", async () => {
    seed([{ type: "agent", id: "agent-1" }, 4]);
    const user = userEvent.setup();
    const onUpdate = vi.fn();
    renderPicker(onUpdate);

    const section = (await screen.findByText("Recent")).parentElement as HTMLElement;
    await user.click(within(section).getByText("Builder"));

    expect(onUpdate).toHaveBeenCalledWith({ assignee_type: "agent", assignee_id: "agent-1" });
  });

  it("is hidden while searching so Enter picks the first match", async () => {
    seed([{ type: "member", id: "user-1" }, 6]);
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

    // A pick from another picker lands and Recent is inserted above Members.
    act(() => seed([{ type: "agent", id: "agent-1" }, 4]));
    rerender(picker(onUpdate));
    await screen.findByText("Recent");

    await user.keyboard("{Enter}");
    expect(onUpdate).toHaveBeenCalledWith({ assignee_type: "member", assignee_id: "user-1" });
  });
});

describe("AssigneePicker pick recording", () => {
  beforeEach(() => {
    useAssigneePicksStore.setState({ byScope: {} });
  });

  it("records one pick per click, and repeats add up", async () => {
    const user = userEvent.setup();
    renderPicker();

    await user.click(await screen.findByText("Builder"));
    // Now listed twice: in Recent and under Agents.
    await user.click(screen.getAllByText("Builder").at(-1)!);
    expect(queue()).toEqual([
      { type: "agent", id: "agent-1" },
      { type: "agent", id: "agent-1" },
    ]);
  });

  it("records a batch pick across mixed assignees once", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();
    renderPicker(onUpdate, { mixed: true });

    await user.click(await screen.findByText("Platform"));
    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect(queue()).toEqual([{ type: "squad", id: "squad-1" }]);
  });

  it("does not record re-picking the current assignee or clearing it", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();
    renderPicker(onUpdate, { assigneeType: "member", assigneeId: "user-2" });

    await user.click(await screen.findByText("Grace Hopper"));
    await user.click(screen.getAllByText("Unassigned").at(-1)!);
    expect(onUpdate).toHaveBeenCalledTimes(2);
    expect(queue()).toEqual([]);
  });

  it("leaves recording to the host when recordPicks is off", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();
    renderPicker(onUpdate, { recordPicks: false });

    await user.click(await screen.findByText("Builder"));
    expect(onUpdate).toHaveBeenCalledWith({ assignee_type: "agent", assignee_id: "agent-1" });
    expect(queue()).toEqual([]);
  });
});
