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

import { useIssueOpeningStore } from "./issue-opening-store";

beforeEach(() => {
  storage.clear();
  useIssueOpeningStore.setState({ openMode: "page" });
});

describe("issue opening preference", () => {
  it("restores the saved choice on rehydration", async () => {
    useIssueOpeningStore.getState().setOpenMode("peek");
    const saved = storage.get("multica_issue_opening")!;
    expect(JSON.parse(saved).state).toEqual({ openMode: "peek" });
    useIssueOpeningStore.getState().setOpenMode("page");
    storage.set("multica_issue_opening", saved);
    await useIssueOpeningStore.persist.rehydrate();
    expect(useIssueOpeningStore.getState().openMode).toBe("peek");
  });

  it.each([null, {}, { openMode: "unknown" }])("defaults to full pages for missing or invalid preferences: %j", async (state) => {
    useIssueOpeningStore.getState().setOpenMode("peek");
    storage.set("multica_issue_opening", JSON.stringify({ state, version: 0 }));
    await useIssueOpeningStore.persist.rehydrate();
    expect(useIssueOpeningStore.getState().openMode).toBe("page");
  });
});
