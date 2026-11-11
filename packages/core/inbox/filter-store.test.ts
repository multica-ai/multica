// @vitest-environment node

import { beforeEach, describe, expect, it } from "vitest";
import type { InboxItem } from "../types";
import {
  EMPTY_INBOX_FILTERS,
  archivedInboxQuery,
  effectiveInboxFilters,
  filterInboxItems,
  inboxActorKey,
  inboxFacetCounts,
  inboxFiltersForPrioritySupport,
  inboxFilterCount,
  inboxIssueFieldsSupport,
  inboxPriorityFilterSupport,
  inboxReceivedAfter,
  inboxTypeGroup,
  isEmptyInboxFilters,
  sanitizeInboxFilters,
  useInboxFilterStore,
  type InboxFilters,
} from "./filter-store";

function item(
  id: string,
  issueStatus: InboxItem["issue_status"],
  issuePriority: InboxItem["issue_priority"],
  overrides: Partial<InboxItem> = {},
): InboxItem {
  return {
    id,
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: null,
    actor_id: null,
    type: "mentioned",
    severity: "info",
    issue_id: issueStatus == null ? null : `issue-${id}`,
    title: id,
    body: null,
    issue_status: issueStatus,
    issue_priority: issuePriority,
    issue_project_id: null,
    issue_assignee_type: null,
    issue_assignee_id: null,
    issue_due_date: null,
    issue_label_ids: [],
    read: false,
    archived: false,
    created_at: "2026-08-24T00:00:00Z",
    details: null,
    ...overrides,
  };
}

const ids = (items: InboxItem[]) => items.map((candidate) => candidate.id);

const ITEMS = [
  item("todo-high", "todo", "high"),
  item("todo-low", "todo", "low"),
  item("done-high", "done", "high"),
  item("system", null, null),
];

beforeEach(() => {
  useInboxFilterStore.setState({ filtersByWorkspace: {} });
});

describe("filterInboxItems", () => {
  it("shows each issue's newest notification when no filter is active", () => {
    const older = item("older", "todo", "high", { issue_id: "issue-1", created_at: "2026-08-23T00:00:00Z" });
    const newer = item("newer", "todo", "high", { issue_id: "issue-1" });
    expect(ids(filterInboxItems([older, newer, ...ITEMS], EMPTY_INBOX_FILTERS))).toEqual([
      "newer", "todo-high", "todo-low", "done-high", "system",
    ]);
  });

  it("uses OR within a dimension and AND between dimensions", () => {
    expect(
      ids(filterInboxItems(ITEMS, {
        ...EMPTY_INBOX_FILTERS,
        statuses: ["todo", "done"],
        priorities: ["high"],
      })),
    ).toEqual(["todo-high", "done-high"]);
  });

  it("excludes notifications without an issue from issue filters, negated ones too", () => {
    const filters = { ...EMPTY_INBOX_FILTERS, priorities: ["high"] } as const;
    expect(ids(filterInboxItems(ITEMS, filters))).toEqual(["todo-high", "done-high"]);
    expect(ids(filterInboxItems(ITEMS, { ...filters, negated: ["priority"] }))).toEqual(["todo-low"]);
  });

  it("does not apply a priority condition until the projection is supported", () => {
    const filters = {
      ...EMPTY_INBOX_FILTERS,
      statuses: ["todo"],
      priorities: ["high"],
      actors: ["member:alice"],
      unreadOnly: true,
    } as const;

    // Only the unsupported dimension is dropped — a backend that cannot
    // project priority still has nothing to say about who sent a
    // notification or whether it was read.
    expect(inboxFiltersForPrioritySupport(filters, "unknown")).toEqual({
      ...filters,
      priorities: [],
    });
    expect(inboxFiltersForPrioritySupport(filters, "unsupported")).toEqual({
      ...filters,
      priorities: [],
    });
    expect(inboxFiltersForPrioritySupport(filters, "supported")).toBe(filters);
  });
});

describe("notification dimensions", () => {
  const alice = { actor_type: "member" as const, actor_id: "alice" };
  const bob = { actor_type: "agent" as const, actor_id: "bob" };

  it("keys members and agents on their id and collapses system into one bucket", () => {
    expect(inboxActorKey(item("a", "todo", "high", alice))).toBe("member:alice");
    expect(inboxActorKey(item("b", "todo", "high", bob))).toBe("agent:bob");
    expect(
      inboxActorKey(
        item("c", "todo", "high", { actor_type: "system", actor_id: null }),
      ),
    ).toBe("system");
    expect(inboxActorKey(item("d", "todo", "high"))).toBeNull();
  });

  it("matches any notification of an issue and shows the newest one that matched", () => {
    // Alice commented, then Bob changed the status. Filtering by Alice finds
    // the issue and renders Alice's comment, not Bob's later change.
    const comment = item("comment", "todo", "high", {
      issue_id: "issue-1", type: "new_comment", created_at: "2026-08-23T00:00:00Z",
      details: { comment_id: "c-1" }, ...alice,
    });
    const change = item("change", "todo", "high", { issue_id: "issue-1", type: "status_changed", ...bob });

    expect(ids(filterInboxItems([comment, change], { ...EMPTY_INBOX_FILTERS, actors: ["member:alice"] }))).toEqual(["comment"]);
    expect(ids(filterInboxItems([comment, change], { ...EMPTY_INBOX_FILTERS, types: ["comment"] }))).toEqual(["comment"]);
    // "Is not" also asks of any notification: Bob's change is not a comment.
    const notComment = filterInboxItems([comment, change], { ...EMPTY_INBOX_FILTERS, types: ["comment"], negated: ["type"] });
    expect(ids(notComment)).toEqual(["change"]);
    // The rendered row keeps the issue's comment anchor.
    expect(notComment[0]?.details?.comment_id).toBe("c-1");
  });

  it("treats a row without attribution as not matching a sender, and as matching its exclusion", () => {
    const unattributed = [item("unattributed", "todo", "high")];
    expect(filterInboxItems(unattributed, { ...EMPTY_INBOX_FILTERS, actors: ["member:alice"] })).toEqual([]);
    expect(ids(filterInboxItems(unattributed, { ...EMPTY_INBOX_FILTERS, actors: ["member:alice"], negated: ["actor"] })))
      .toEqual(["unattributed"]);
  });

  it("groups every notification kind, unknown ones as system", () => {
    expect(inboxTypeGroup("mentioned")).toBe("mention");
    expect(inboxTypeGroup("reaction_added")).toBe("comment");
    expect(inboxTypeGroup("issue_subscribed")).toBe("assign");
    expect(inboxTypeGroup("children_done")).toBe("change");
    expect(inboxTypeGroup("agent_blocked")).toBe("run");
    expect(inboxTypeGroup("quick_create_failed")).toBe("system");
    expect(inboxTypeGroup("something_new")).toBe("system");
  });

  it("filters by local calendar days received", () => {
    const now = new Date(2026, 7, 24, 15, 0);
    expect(inboxReceivedAfter("today", now)).toEqual(new Date(2026, 7, 24));
    expect(inboxReceivedAfter("last_7_days", now)).toEqual(new Date(2026, 7, 18));
    const rows = [
      item("this-morning", "todo", "high", { created_at: new Date(2026, 7, 24, 8).toISOString() }),
      item("last-night", "todo", "high", { created_at: new Date(2026, 7, 23, 23).toISOString() }),
    ];
    expect(ids(filterInboxItems(rows, { ...EMPTY_INBOX_FILTERS, received: "today" }, { now }))).toEqual(["this-morning"]);
  });
});

describe("issue dimensions", () => {
  const rows = [
    item("tracked", "todo", "high", {
      issue_project_id: "project-1", issue_assignee_type: "agent", issue_assignee_id: "agent-1",
      issue_label_ids: ["bug", "ui"], issue_due_date: "2026-08-24",
    }),
    item("bare", "todo", "high"),
    item("no-issue", null, null),
  ];
  const run = (filters: Partial<InboxFilters>) =>
    ids(filterInboxItems(rows, { ...EMPTY_INBOX_FILTERS, ...filters }, { now: new Date(2026, 7, 24, 12) }));

  it("matches project, assignee, label and due date, with none for unset", () => {
    expect(run({ projects: ["project-1"] })).toEqual(["tracked"]);
    expect(run({ projects: ["none"] })).toEqual(["bare"]);
    expect(run({ assignees: ["agent:agent-1"] })).toEqual(["tracked"]);
    expect(run({ assignees: ["none"] })).toEqual(["bare"]);
    expect(run({ labels: ["ui"] })).toEqual(["tracked"]);
    expect(run({ labels: ["bug"], negated: ["label"] })).toEqual(["bare"]);
    expect(run({ dueDates: ["today"] })).toEqual(["tracked"]);
    expect(run({ dueDates: ["overdue", "none"] })).toEqual(["bare"]);
  });

  it("keeps or drops issues by whether an agent is on them", () => {
    const filters = (agent: InboxFilters["agent"]) => ({ ...EMPTY_INBOX_FILTERS, agent });
    const activeIssueIds = new Set(["issue-tracked"]);
    expect(ids(filterInboxItems(rows, filters("running"), { activeIssueIds }))).toEqual(["tracked"]);
    expect(ids(filterInboxItems(rows, filters("idle"), { activeIssueIds }))).toEqual(["bare"]);
  });
});

describe("unread filtering", () => {
  it("keeps unread rows and counts as one active filter", () => {
    const rows = [
      item("unread", "todo", "high"),
      item("read", "todo", "high", { read: true }),
    ];
    const filters = { ...EMPTY_INBOX_FILTERS, unreadOnly: true };

    expect(ids(filterInboxItems(rows, filters))).toEqual(["unread"]);
    expect(inboxFilterCount(filters)).toBe(1);
  });

  it("keeps notifications without an issue, unlike the issue dimensions", () => {
    // The issue-less rows are the quick-create failures — the ones that most
    // need to stay reachable while screening for what is unhandled.
    const rows = [item("no-issue", null, null)];

    expect(
      filterInboxItems(rows, { ...EMPTY_INBOX_FILTERS, unreadOnly: true }),
    ).toEqual(rows);
  });

  it("keeps the open notification visible after opening marks it read", () => {
    const filters = { ...EMPTY_INBOX_FILTERS, unreadOnly: true };
    const opened = item("opened", "todo", "high", { read: true });
    const read = item("read", "todo", "high", { read: true });

    expect(
      ids(filterInboxItems([opened, read], filters, { pinnedUnreadKey: "issue-opened" })),
    ).toEqual(["opened"]);

    // Pinning only crosses the unread dimension: an explicit issue filter
    // still removes the open row, and the page clears its selection.
    expect(
      filterInboxItems([opened], { ...filters, statuses: ["done"] }, { pinnedUnreadKey: "issue-opened" }),
    ).toEqual([]);
  });
});

describe("pinned row with agent filtering", () => {
  it("keeps the open notification after an agent starts on its issue", () => {
    const filters = { ...EMPTY_INBOX_FILTERS, agent: "idle" as const };
    const opened = item("opened", "todo", "high", { issue_id: "issue-opened" });
    const other = item("other", "todo", "high", { issue_id: "issue-other" });
    const activeIssueIds = new Set(["issue-opened", "issue-other"]);

    expect(
      ids(filterInboxItems([opened, other], filters, {
        activeIssueIds,
        pinnedAgentKey: "issue-opened",
      })),
    ).toEqual(["opened"]);

    // Each pin crosses only its own dimension.
    expect(
      filterInboxItems([opened], filters, {
        activeIssueIds,
        pinnedUnreadKey: "issue-opened",
      }),
    ).toEqual([]);
    expect(
      filterInboxItems(
        [{ ...opened, read: true }],
        { ...filters, agent: null, unreadOnly: true },
        { pinnedAgentKey: "issue-opened" },
      ),
    ).toEqual([]);

    // The pin still never crosses an explicit issue filter.
    expect(
      filterInboxItems([opened], { ...filters, statuses: ["done"] }, {
        activeIssueIds,
        pinnedAgentKey: "issue-opened",
      }),
    ).toEqual([]);
  });
});

describe("inboxFacetCounts", () => {
  it("counts each value as if it alone were selected", () => {
    const rows = [
      item("mention", "todo", "high", { issue_id: "issue-1", created_at: "2026-08-23T00:00:00Z", actor_type: "member", actor_id: "alice" }),
      item("change", "todo", "high", { issue_id: "issue-1", type: "status_changed", read: true, actor_type: "agent", actor_id: "bob" }),
      item("other", "done", "low", { type: "new_comment", issue_project_id: "project-1", issue_label_ids: ["bug"] }),
      item("no-issue", null, null, { type: "quick_create_failed" }),
    ];
    const counts = inboxFacetCounts(rows, { ...EMPTY_INBOX_FILTERS, types: ["mention"], statuses: ["todo"] });
    // Type ignores its own selection; status still applies.
    expect(Object.fromEntries(counts.types)).toEqual({ mention: 1, change: 1 });
    // Status ignores its own selection; the mention filter picks the rows.
    expect(Object.fromEntries(counts.statuses)).toEqual({ todo: 1 });
    expect(Object.fromEntries(counts.actors)).toEqual({ "member:alice": 1 });
    expect(counts.unread).toBe(1);

    const unfiltered = inboxFacetCounts(rows, EMPTY_INBOX_FILTERS);
    expect(Object.fromEntries(unfiltered.projects)).toEqual({ none: 1, "project-1": 1 });
    expect(Object.fromEntries(unfiltered.labels)).toEqual({ bug: 1 });
    expect(Object.fromEntries(unfiltered.types)).toEqual({ mention: 1, change: 1, comment: 1, system: 1 });
  });
});

describe("backend capability", () => {
  it("distinguishes an omitted legacy projection from a supported null", () => {
    expect(inboxPriorityFilterSupport([])).toBe("unknown");
    expect(inboxPriorityFilterSupport([item("system", null, null)])).toBe(
      "supported",
    );
    expect(inboxPriorityFilterSupport([item("legacy", "todo", undefined)])).toBe(
      "unsupported",
    );
    expect(inboxIssueFieldsSupport([item("system", null, null)])).toBe("supported");
    expect(inboxIssueFieldsSupport([item("legacy", "todo", "high", { issue_label_ids: undefined })])).toBe("unsupported");
  });

  it("keeps unproven dimensions dormant, and on the archive everything an old server would ignore", () => {
    const filters: InboxFilters = {
      ...EMPTY_INBOX_FILTERS,
      types: ["mention"], statuses: ["done"], projects: ["p"], agent: "idle",
      negated: ["status"], received: "today",
    };
    expect(effectiveInboxFilters(filters, { priority: "supported", issueFields: "unknown" })).toEqual({
      ...filters, projects: [],
    });
    expect(effectiveInboxFilters(filters, { priority: "supported", issueFields: "unsupported", archived: true })).toEqual({
      ...EMPTY_INBOX_FILTERS,
    });
    expect(effectiveInboxFilters(filters, { priority: "supported", issueFields: "supported", archived: true })).toEqual({
      ...filters, agent: null,
    });
  });
});

describe("archivedInboxQuery", () => {
  it("serializes every archive dimension in a stable order", () => {
    const now = new Date(2026, 7, 24, 15, 0);
    expect(archivedInboxQuery({
      ...EMPTY_INBOX_FILTERS,
      types: ["run", "mention"], actors: ["system"], statuses: ["todo"], projects: ["none", "p"],
      assignees: ["member:m"], labels: ["l"], dueDates: ["overdue"], received: "last_7_days",
      negated: ["type", "label", "priority"], unreadOnly: true,
    }, now)).toEqual({
      types: "mention,run", actors: "system", statuses: "todo", projects: "none,p",
      assignees: "member:m", labels: "l", due_dates: "overdue",
      // A dimension with nothing selected has nothing to negate.
      negated: "label,type",
      today: "2026-08-24", received_after: new Date(2026, 7, 18).toISOString(), unread_only: "true",
    });
  });
});

describe("useInboxFilterStore", () => {
  it("keeps filters isolated by workspace and clears one workspace only", () => {
    const store = useInboxFilterStore.getState();
    store.toggleStatusFilter("ws-1", "todo");
    store.togglePriorityFilter("ws-2", "urgent");

    expect(useInboxFilterStore.getState().filtersByWorkspace).toMatchObject({
      "ws-1": { statuses: ["todo"], priorities: [] },
      "ws-2": { statuses: [], priorities: ["urgent"] },
    });
    expect(
      inboxFilterCount(useInboxFilterStore.getState().filtersByWorkspace["ws-1"]!),
    ).toBe(1);

    useInboxFilterStore.getState().clearFilters("ws-1");
    expect(useInboxFilterStore.getState().filtersByWorkspace["ws-1"]).toBeUndefined();
    expect(useInboxFilterStore.getState().filtersByWorkspace["ws-2"]).toBeDefined();
  });

  it("clears only the unsupported dimensions", () => {
    const store = useInboxFilterStore.getState();
    store.toggleStatusFilter("ws-1", "todo");
    store.togglePriorityFilter("ws-1", "high");
    store.toggleFilterValue("ws-1", "label", "bug");
    store.setFilterNegated("ws-1", "label", true);

    useInboxFilterStore.getState().clearPriorityFilters("ws-1");
    useInboxFilterStore.getState().clearIssueFieldFilters("ws-1");

    expect(useInboxFilterStore.getState().filtersByWorkspace["ws-1"]).toEqual({
      ...EMPTY_INBOX_FILTERS,
      statuses: ["todo"],
    });
  });

  it("keeps is not when it is chosen before the first value", () => {
    const store = useInboxFilterStore.getState();
    store.setFilterNegated("ws-1", "type", true);
    useInboxFilterStore.getState().toggleFilterValue("ws-1", "type", "run");

    const filters = useInboxFilterStore.getState().filtersByWorkspace["ws-1"]!;
    expect(filters).toMatchObject({ types: ["run"], negated: ["type"] });
    expect(inboxFilterCount(filters)).toBe(1);
  });

  it("keeps a dimension's operator across its values and drops it when the field is cleared", () => {
    const store = useInboxFilterStore.getState();
    store.toggleFilterValue("ws-1", "type", "mention");
    store.setFilterNegated("ws-1", "type", true);
    useInboxFilterStore.getState().toggleFilterValue("ws-1", "type", "mention");
    useInboxFilterStore.getState().toggleFilterValue("ws-1", "type", "run");
    expect(useInboxFilterStore.getState().filtersByWorkspace["ws-1"]).toMatchObject({
      types: ["run"], negated: ["type"],
    });

    useInboxFilterStore.getState().clearFilter("ws-1", "type");
    expect(useInboxFilterStore.getState().filtersByWorkspace["ws-1"]).toBeUndefined();
  });

  it("does not count or send an operator with no value, and forgets it on reload", () => {
    useInboxFilterStore.getState().setFilterNegated("ws-1", "label", true);
    const filters = useInboxFilterStore.getState().filtersByWorkspace["ws-1"]!;

    expect(inboxFilterCount(filters)).toBe(0);
    expect(archivedInboxQuery(filters).negated).toBeUndefined();
    expect(isEmptyInboxFilters(sanitizeInboxFilters(filters))).toBe(true);
    expect(sanitizeInboxFilters(filters).negated).toEqual([]);
  });

  it("selects a single received window and agent state, toggling off on reselect", () => {
    const store = useInboxFilterStore.getState();
    store.setReceived("ws-1", "today");
    store.setReceived("ws-1", "last_7_days");
    store.setAgent("ws-1", "running");
    expect(useInboxFilterStore.getState().filtersByWorkspace["ws-1"]).toMatchObject({
      received: "last_7_days", agent: "running",
    });
    useInboxFilterStore.getState().setReceived("ws-1", "last_7_days");
    useInboxFilterStore.getState().clearFilter("ws-1", "agent");
    expect(useInboxFilterStore.getState().filtersByWorkspace["ws-1"]).toBeUndefined();
  });

  it("toggles the unread dimension off again", () => {
    useInboxFilterStore.getState().toggleUnreadOnly("ws-1");
    expect(
      useInboxFilterStore.getState().filtersByWorkspace["ws-1"]?.unreadOnly,
    ).toBe(true);

    useInboxFilterStore.getState().toggleUnreadOnly("ws-1");
    expect(useInboxFilterStore.getState().filtersByWorkspace["ws-1"]).toBeUndefined();
  });
});

describe("sanitizeInboxFilters", () => {
  it("rebuilds a stored entry, dropping unknown values and carrying the old idle toggle over", () => {
    expect(sanitizeInboxFilters({
      statuses: ["todo", 3], types: ["mention", "bogus"], idleOnly: true,
      negated: ["status", "label", "creator"], received: "yesterday",
    })).toEqual({
      ...EMPTY_INBOX_FILTERS,
      statuses: ["todo"], types: ["mention"], agent: "idle", negated: ["status"],
    });
    expect(sanitizeInboxFilters(null)).toEqual(EMPTY_INBOX_FILTERS);
  });
});
