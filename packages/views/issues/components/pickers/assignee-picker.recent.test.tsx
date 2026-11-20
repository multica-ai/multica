// The assignee picker's Recent section: the user's three most-assigned actors,
// counted on this device per signed-in user. Uses the real usage store,
// localStorage and PropertyPicker so recording and rendering are tested together.
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import {
  getAssigneeUsage,
  recordAssigneeUsage,
  resetAssigneeUsageForTests,
} from "@multica/core/issues/stores/assignee-usage-store";
import enIssues from "../../../locales/en/issues.json";
import { AssigneePicker } from "./assignee-picker";

const WS_ID = "workspace-1";
const auth = { userId: "user-1" };

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
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: auth.userId } }),
}));
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
  vi.restoreAllMocks();
  auth.userId = "user-1";
  localStorage.clear();
  resetAssigneeUsageForTests();
});

const record = (type: "member" | "agent" | "squad", id: string, times = 1) => {
  for (let i = 0; i < times; i++) recordAssigneeUsage(auth.userId, WS_ID, { type, id });
};

describe("AssigneePicker Recent section", () => {
  it("is absent when this browser has no assignment history", async () => {
    renderPicker();
    await screen.findByText("Members");
    expect(screen.queryByText("Recent")).toBeNull();
  });

  it("lists the top three by count, skipping archived actors", async () => {
    record("agent", "agent-2", 5);
    record("member", "user-2", 4);
    record("squad", "squad-1", 3);
    record("agent", "agent-1", 2);
    record("member", "user-1");

    renderPicker();
    await screen.findByText("Recent");

    expect(recentNames()).toEqual(["Grace Hopper", "Core Squad", "Builder"]);
  });

  it("hides while searching", async () => {
    record("member", "user-2");
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

    expect(getAssigneeUsage(auth.userId, WS_ID)).toEqual([]);
  });

  it("still assigns when the browser refuses to save the history", async () => {
    vi.spyOn(localStorage, "setItem").mockImplementation(() => {
      throw new DOMException("The quota has been exceeded.", "QuotaExceededError");
    });
    const user = userEvent.setup();
    const onUpdate = vi.fn();

    renderPicker(onUpdate);
    await user.click(await screen.findByText("Grace Hopper"));

    expect(onUpdate).toHaveBeenCalledWith({ assignee_type: "member", assignee_id: "user-2" });
  });

  it("does not show one account's history to the next account on this browser", async () => {
    const user = userEvent.setup();

    const { unmount } = renderPicker();
    await user.click(await screen.findByText("Grace Hopper"));
    unmount();

    auth.userId = "user-2";
    renderPicker();
    await screen.findByText("Members");
    expect(screen.queryByText("Recent")).toBeNull();
  });
});
