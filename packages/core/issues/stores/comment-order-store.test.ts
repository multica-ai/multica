// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { setCurrentWorkspace } from "../../platform/workspace-storage";
import { useIssueCommentOrderStore } from "./comment-order-store";

const flush = () => new Promise((resolve) => queueMicrotask(() => resolve(null)));

// Node 25 ships a partial `localStorage` shim under jsdom that's missing
// `clear`/`removeItem`; replace it with a real in-memory Storage so persist
// can round-trip values.
beforeAll(() => {
  if (typeof globalThis.localStorage?.clear !== "function") {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() {
        return values.size;
      },
      clear: () => values.clear(),
      getItem: (k) => values.get(k) ?? null,
      key: (i) => Array.from(values.keys())[i] ?? null,
      removeItem: (k) => {
        values.delete(k);
      },
      setItem: (k, v) => {
        values.set(k, v);
      },
    };
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: storage,
    });
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: storage,
    });
  }
});

beforeEach(() => {
  localStorage.clear();
  useIssueCommentOrderStore.setState({ order: "oldest_first" });
  setCurrentWorkspace(null, null);
});

afterEach(() => {
  setCurrentWorkspace(null, null);
});

describe("useIssueCommentOrderStore", () => {
  it("defaults to oldest first so existing timelines do not change", () => {
    expect(useIssueCommentOrderStore.getState().order).toBe("oldest_first");
  });

  it("persists the selected order under the workspace namespace", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();

    useIssueCommentOrderStore.getState().setOrder("newest_first");

    const raw = localStorage.getItem("multica_issue_comment_order:acme");
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw as string);
    expect(parsed.state).toEqual({ order: "newest_first" });
  });

  it("rehydrates the saved order when switching workspaces", async () => {
    localStorage.setItem(
      "multica_issue_comment_order:acme",
      JSON.stringify({ state: { order: "newest_first" }, version: 0 }),
    );
    localStorage.setItem(
      "multica_issue_comment_order:beta",
      JSON.stringify({ state: { order: "oldest_first" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useIssueCommentOrderStore.getState().order).toBe("newest_first");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();
    expect(useIssueCommentOrderStore.getState().order).toBe("oldest_first");
  });
});
