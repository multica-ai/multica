import { describe, it, expect, vi, beforeEach } from "vitest";
import { clearWorkspaceStorage } from "./storage-cleanup";
import {
  registerDraftCleanup,
  __clearDraftCleanupRegistryForTest,
} from "../drafts/cleanup-registry";

beforeEach(() => {
  __clearDraftCleanupRegistryForTest();
});

describe("clearWorkspaceStorage", () => {
  it("removes all non-draft workspace-scoped keys for the given slug", () => {
    const adapter = {
      getItem: vi.fn(),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    };

    clearWorkspaceStorage(adapter, "ws_123");

    expect(adapter.removeItem).toHaveBeenCalledWith("multica_issue_surface_views:ws_123");
    expect(adapter.removeItem).toHaveBeenCalledWith("multica_issues_view:ws_123");
    expect(adapter.removeItem).toHaveBeenCalledWith("multica_issues_scope:ws_123");
    expect(adapter.removeItem).toHaveBeenCalledWith("multica_my_issues_view:ws_123");
    expect(adapter.removeItem).toHaveBeenCalledWith("multica:chat:selectedAgentId:ws_123");
    expect(adapter.removeItem).toHaveBeenCalledWith("multica:chat:activeSessionId:ws_123");
    expect(adapter.removeItem).toHaveBeenCalledWith("multica:chat:expanded:ws_123");
    expect(adapter.removeItem).toHaveBeenCalledWith("multica_navigation:ws_123");
    // 8 non-draft keys, and no registered drafts in this test.
    expect(adapter.removeItem).toHaveBeenCalledTimes(8);
  });

  it("also clears registered draft keys via the registry", () => {
    const adapter = {
      getItem: vi.fn(),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    };
    registerDraftCleanup({
      storageKey: "multica_test_draft",
      workspaceScoped: true,
      resetInMemory: vi.fn(),
    });
    registerDraftCleanup({
      storageKey: "multica_test_global_draft",
      workspaceScoped: false,
      resetInMemory: vi.fn(),
    });

    clearWorkspaceStorage(adapter, "ws_123");

    expect(adapter.removeItem).toHaveBeenCalledWith("multica_test_draft:ws_123");
    // Globally-namespaced draft keys are removed without the slug suffix.
    expect(adapter.removeItem).toHaveBeenCalledWith("multica_test_global_draft");
    // 8 non-draft keys + 2 registered draft keys.
    expect(adapter.removeItem).toHaveBeenCalledTimes(10);
  });
});
