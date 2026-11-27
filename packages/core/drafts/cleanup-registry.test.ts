import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  registerDraftCleanup,
  clearRegisteredWorkspaceDrafts,
  resetAllRegisteredDrafts,
  __clearDraftCleanupRegistryForTest,
  __getRegisteredDraftKeysForTest,
} from "./cleanup-registry";

beforeEach(() => {
  __clearDraftCleanupRegistryForTest();
});

describe("draft cleanup registry", () => {
  it("clears workspace-scoped keys with the slug suffix and global keys bare", () => {
    const adapter = { getItem: vi.fn(), setItem: vi.fn(), removeItem: vi.fn() };
    registerDraftCleanup({ storageKey: "scoped_a", workspaceScoped: true, resetInMemory: vi.fn() });
    registerDraftCleanup({ storageKey: "global_b", workspaceScoped: false, resetInMemory: vi.fn() });

    clearRegisteredWorkspaceDrafts(adapter, "acme");

    expect(adapter.removeItem).toHaveBeenCalledWith("scoped_a:acme");
    expect(adapter.removeItem).toHaveBeenCalledWith("global_b");
    expect(adapter.removeItem).toHaveBeenCalledTimes(2);
  });

  it("resets every registered store's in-memory state", () => {
    const resetA = vi.fn();
    const resetB = vi.fn();
    registerDraftCleanup({ storageKey: "a", workspaceScoped: true, resetInMemory: resetA });
    registerDraftCleanup({ storageKey: "b", workspaceScoped: true, resetInMemory: resetB });

    resetAllRegisteredDrafts();

    expect(resetA).toHaveBeenCalledTimes(1);
    expect(resetB).toHaveBeenCalledTimes(1);
  });

  it("is idempotent per key — re-registering overwrites, never duplicates", () => {
    registerDraftCleanup({ storageKey: "dupe", workspaceScoped: true, resetInMemory: vi.fn() });
    registerDraftCleanup({ storageKey: "dupe", workspaceScoped: true, resetInMemory: vi.fn() });

    expect(__getRegisteredDraftKeysForTest()).toEqual(["dupe"]);
  });
});
