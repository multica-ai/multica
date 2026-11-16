import { beforeEach, describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import type { InboxItem } from "../types";
import { keepOpenInboxRow, nextInboxRow, type HeldInboxRow } from "./open-row";
import { useInboxSelectionStore } from "./selection-store";
import { onInboxIssueDeleted, onInboxIssuesDeleted } from "./ws-updaters";

function item(id: string, overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: `n-${id}`,
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: id,
    title: id,
    body: null,
    issue_status: "in_progress",
    issue_priority: "none",
    read: true,
    archived: false,
    created_at: "2026-11-10T08:00:00Z",
    details: null,
    ...overrides,
  };
}

const NOW = new Date("2026-11-16T08:00:00Z").getTime();
const FILTERS = {};
const [A, B, C] = [item("a"), item("b"), item("c")];
const held = (row: InboxItem, index: number, filters: unknown = FILTERS): HeldInboxRow => ({
  key: row.issue_id!,
  item: row,
  index,
  filters,
});

describe("keepOpenInboxRow", () => {
  it("leaves a list that still shows the open row as it is", () => {
    const rows = [A, B, C];
    expect(keepOpenInboxRow(rows, rows, held(B, 1), "b", FILTERS, { now: NOW })).toEqual({ rows, kept: null });
  });

  it("keeps an archived open row where it was", () => {
    const { rows, kept } = keepOpenInboxRow([A, C], [A, C], held(B, 1), "b", FILTERS, { now: NOW });
    expect(rows).toEqual([A, B, C]);
    expect(kept).toEqual({ key: "b", item: B, reason: "archived" });
  });

  it("says a row snoozed elsewhere is snoozed, as it is now", () => {
    const snoozed = { ...B, snoozed_until: "2026-11-17T09:00:00Z" };
    const { kept } = keepOpenInboxRow([A, C], [A, snoozed, C], held(B, 1), "b", FILTERS, { now: NOW });
    expect(kept).toEqual({ key: "b", item: snoozed, reason: "snoozed" });
  });

  it("says a row snoozed in another window is snoozed, though the list leaves it out", () => {
    const until = "2026-11-17T09:00:00Z";
    const snoozed = [{ issue_id: "b", item_id: "n-b2", snoozed_until: until, snoozed_at: "2026-11-16T07:00:00Z", updates: 0 }];
    const { rows, kept } = keepOpenInboxRow([A, C], [A, C], held(B, 1), "b", FILTERS, { snoozed, now: NOW });
    expect(kept).toEqual({ key: "b", item: { ...B, id: "n-b2", snoozed_until: until }, reason: "snoozed" });
    expect(rows[1]).toBe(kept?.item);
  });

  it("says a row whose snooze has passed was archived", () => {
    const snoozed = [{ issue_id: "b", item_id: "n-b", snoozed_until: "2026-11-15T09:00:00Z", snoozed_at: "2026-11-14T07:00:00Z", updates: 0 }];
    expect(keepOpenInboxRow([A, C], [A, C], held(B, 1), "b", FILTERS, { snoozed, now: NOW }).kept?.reason).toBe("archived");
  });

  it("keeps a row an edit took out of the filters, as it is now", () => {
    const edited = { ...B, issue_status: "todo" };
    const { rows, kept } = keepOpenInboxRow([A, C], [A, edited, C], held(B, 1), "b", FILTERS, { now: NOW });
    expect(rows).toEqual([A, edited, C]);
    expect(kept?.reason).toBe("filtered");
  });

  it("lets a changed filter drop the open row", () => {
    const rows = [A, C];
    expect(keepOpenInboxRow(rows, [A, B, C], held(B, 1), "b", { ...FILTERS }, { now: NOW })).toEqual({ rows, kept: null });
  });

  it("keeps an archived row even under changed filters", () => {
    expect(keepOpenInboxRow([A], [A], held(B, 1), "b", { ...FILTERS }, { now: NOW }).kept?.reason).toBe("archived");
  });

  it("keeps nothing for another notification, or with none open", () => {
    expect(keepOpenInboxRow([A, C], [A, C], held(B, 1), "c", FILTERS, { now: NOW }).kept).toBeNull();
    expect(keepOpenInboxRow([A, C], [A, C], held(B, 1), null, FILTERS, { now: NOW }).kept).toBeNull();
    expect(keepOpenInboxRow([A, C], [A, C], undefined, "b", FILTERS, { now: NOW }).kept).toBeNull();
  });

  it("puts the row at the end of a list that got shorter", () => {
    expect(keepOpenInboxRow([A], [A], held(C, 2), "c", FILTERS, { now: NOW }).rows).toEqual([A, C]);
  });
});

describe("nextInboxRow", () => {
  it("opens the older row below, else the newer one above", () => {
    expect(nextInboxRow([A, B, C], "b")).toBe(C);
    expect(nextInboxRow([A, B, C], "c")).toBe(B);
    expect(nextInboxRow([A], "a")).toBeNull();
    expect(nextInboxRow([A, B], "x")).toBeNull();
  });

  it("skips rows leaving with it", () => {
    const unread = item("d", { read: false });
    const isRead = (row: InboxItem) => row.read === true;
    expect(nextInboxRow([unread, A, B, C], "b", isRead)).toBe(unread);
    expect(nextInboxRow([A, B, C], "b", isRead)).toBeNull();
  });
});

describe("held and left rows", () => {
  beforeEach(() => useInboxSelectionStore.setState({ heldRows: {}, leftRowKeyByWorkspace: {} }));
  const store = () => useInboxSelectionStore.getState();

  it("holds a row per list until the user leaves it", () => {
    store().holdRow("ws", "section", held(B, 1));
    store().holdRow("ws", "page", held(B, 3));
    expect(store().heldRows["ws:section"]?.index).toBe(1);
    expect(store().heldRows["ws:page"]?.index).toBe(3);

    store().leaveRow("ws", "b");
    expect(store().heldRows).toEqual({});
    store().holdRow("ws", "section", held(B, 1));
    expect(store().heldRows).toEqual({});
  });

  it("forgets rows once their notification is no longer open", () => {
    store().holdRow("ws", "section", held(B, 1));
    store().holdRow("other", "section", held(C, 0));
    store().leaveRow("ws", "a");
    store().releaseRows("ws", ["b"]);
    expect(Object.keys(store().heldRows)).toEqual(["ws:section", "other:section"]);
    expect(store().leftRowKeyByWorkspace).toEqual({});

    store().releaseRows("ws", []);
    expect(Object.keys(store().heldRows)).toEqual(["other:section"]);
  });

  it("lets a row be kept again once the action that left it is undone", () => {
    store().leaveRow("ws", "b");
    store().returnRow("ws", "c");
    expect(store().leftRowKeyByWorkspace).toEqual({ ws: "b" });
    store().returnRow("ws", "b");
    expect(store().leftRowKeyByWorkspace).toEqual({});
    store().holdRow("ws", "section", held(B, 1));
    expect(store().heldRows["ws:section"]?.key).toBe("b");
  });

  it("forgets every row of an issue that was deleted", async () => {
    store().holdRow("ws", "section", held(B, 1));
    store().holdRow("ws", "page", held(B, 1));
    store().holdRow("other", "section", held(B, 1));
    const qc = new QueryClient();
    await onInboxIssueDeleted(qc, "ws", "b");
    expect(Object.keys(store().heldRows)).toEqual(["other:section"]);

    store().leaveRow("ws", "c");
    await onInboxIssuesDeleted(qc, "ws", ["c"]);
    expect(store().leftRowKeyByWorkspace).toEqual({});
  });

  it("drops the row of a list that goes away", () => {
    store().holdRow("ws", "section", held(B, 1));
    store().holdRow("ws", "page", held(B, 1));
    store().dropRow("ws", "page");
    expect(Object.keys(store().heldRows)).toEqual(["ws:section"]);
  });
});
