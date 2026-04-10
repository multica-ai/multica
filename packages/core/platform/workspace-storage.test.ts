import { describe, it, expect, vi, afterEach } from "vitest";
import { createWorkspaceAwareStorage, setCurrentWorkspaceId } from "./workspace-storage";
import type { StorageAdapter } from "../types/storage";

function mockAdapter(): StorageAdapter {
  const store = new Map<string, string>();
  return {
    getItem: vi.fn((k) => store.get(k) ?? null),
    setItem: vi.fn((k, v) => store.set(k, v)),
    removeItem: vi.fn((k) => store.delete(k)),
  };
}

afterEach(() => {
  setCurrentWorkspaceId(null);
});

describe("workspace-aware storage", () => {
  it("uses plain key when no workspace is set", () => {
    const adapter = mockAdapter();
    setCurrentWorkspaceId(null);
    const storage = createWorkspaceAwareStorage(adapter);

    storage.setItem("draft", "data");
    expect(adapter.setItem).toHaveBeenCalledWith("draft", "data");
  });

  it("namespaces key when workspace is set", () => {
    const adapter = mockAdapter();
    setCurrentWorkspaceId("ws_abc");
    const storage = createWorkspaceAwareStorage(adapter);

    storage.setItem("draft", "data");
    expect(adapter.setItem).toHaveBeenCalledWith("draft:ws_abc", "data");

    storage.getItem("draft");
    expect(adapter.getItem).toHaveBeenCalledWith("draft:ws_abc");
  });

  it("follows workspace changes dynamically", () => {
    const adapter = mockAdapter();
    const storage = createWorkspaceAwareStorage(adapter);

    setCurrentWorkspaceId("ws_1");
    storage.setItem("draft", "v1");
    expect(adapter.setItem).toHaveBeenCalledWith("draft:ws_1", "v1");

    setCurrentWorkspaceId("ws_2");
    storage.setItem("draft", "v2");
    expect(adapter.setItem).toHaveBeenCalledWith("draft:ws_2", "v2");
  });

  it("removeItem uses current workspace", () => {
    const adapter = mockAdapter();
    setCurrentWorkspaceId("ws_x");
    const storage = createWorkspaceAwareStorage(adapter);

    storage.removeItem("draft");
    expect(adapter.removeItem).toHaveBeenCalledWith("draft:ws_x");
  });
});
