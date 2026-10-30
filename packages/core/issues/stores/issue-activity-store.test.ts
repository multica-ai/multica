// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

const storage = vi.hoisted(() => new Map<string, string>());
vi.mock("../../platform/storage", () => ({
  defaultStorage: {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
  },
}));

import { useIssueActivityStore } from "./issue-activity-store";

beforeEach(() => {
  storage.clear();
  useIssueActivityStore.setState({ order: "oldest_first" });
});

describe("issue activity order", () => {
  it.each(["newest_first", "oldest_first"] as const)("restores %s after reload", async (order) => {
    useIssueActivityStore.getState().setOrder(order);
    const saved = storage.get("multica_issue_activity")!;
    expect(JSON.parse(saved).state).toEqual({ order });
    useIssueActivityStore.setState({ order: "newest_first" });
    storage.set("multica_issue_activity", saved);
    await useIssueActivityStore.persist.rehydrate();
    expect(useIssueActivityStore.getState().order).toBe(order);
  });

  it.each([null, {}, { order: "unknown" }])("uses oldest first for missing or invalid saved settings: %j", async (state) => {
    storage.set("multica_issue_activity", JSON.stringify({ state, version: 0 }));
    await useIssueActivityStore.persist.rehydrate();
    expect(useIssueActivityStore.getState().order).toBe("oldest_first");
  });
});
