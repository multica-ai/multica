// @vitest-environment node
import { beforeEach, describe, expect, it } from "vitest";
import {
  resetCommentFocusStoreForTests,
  useCommentFocusStore,
} from "./comment-focus-store";

function reset() {
  resetCommentFocusStoreForTests();
}

describe("useCommentFocusStore (per-issue, session-only focus intent)", () => {
  beforeEach(reset);

  it("starts with no focus intent and no collapsed roots", () => {
    const s = useCommentFocusStore.getState();
    expect(s.focus).toBeNull();
    expect(s.expandedRoots["issue-1"]).toBeUndefined();
  });

  it("requestFocus stores a per-tap nonce so re-taps re-trigger", () => {
    const { requestFocus } = useCommentFocusStore.getState();
    requestFocus("issue-1", "root-a");
    expect(useCommentFocusStore.getState().focus).toEqual({
      issueId: "issue-1",
      rootId: "root-a",
      nonce: 1,
    });
    requestFocus("issue-1", "root-a");
    expect(useCommentFocusStore.getState().focus?.nonce).toBe(2);
  });

  it("requestFocus resets the published status to pending", () => {
    const { requestFocus, setStatus } = useCommentFocusStore.getState();
    requestFocus("issue-1", "root-a");
    setStatus({ phase: "failed", nonce: 1, reason: "scroll" });
    requestFocus("issue-1", "root-a");
    expect(useCommentFocusStore.getState().status).toEqual({
      phase: "pending",
      nonce: 2,
    });
  });

  it("setStatus ignores statuses for a superseded nonce", () => {
    const { requestFocus, setStatus } = useCommentFocusStore.getState();
    requestFocus("issue-1", "root-a");
    requestFocus("issue-1", "root-a"); // nonce 2
    setStatus({ phase: "located", nonce: 1 }); // stale run finishing late
    expect(useCommentFocusStore.getState().status).toEqual({
      phase: "pending",
      nonce: 2,
    });
    setStatus({ phase: "located", nonce: 2 });
    expect(useCommentFocusStore.getState().status).toEqual({
      phase: "located",
      nonce: 2,
    });
  });

  it("focus is isolated per issue — a request for issue-2 replaces issue-1", () => {
    const { requestFocus } = useCommentFocusStore.getState();
    requestFocus("issue-1", "root-a");
    requestFocus("issue-2", "root-b");
    const f = useCommentFocusStore.getState().focus;
    expect(f?.issueId).toBe("issue-2");
    expect(f?.rootId).toBe("root-b");
  });

  it("expandRoot / collapseRoot toggle per-issue sets", () => {
    const { expandRoot, collapseRoot } = useCommentFocusStore.getState();
    expandRoot("issue-1", "root-a");
    expandRoot("issue-1", "root-b");
    expect(useCommentFocusStore.getState().expandedRoots["issue-1"]).toEqual(
      new Set(["root-a", "root-b"]),
    );
    collapseRoot("issue-1", "root-a");
    expect(useCommentFocusStore.getState().expandedRoots["issue-1"]).toEqual(
      new Set(["root-b"]),
    );
    // Other issue untouched.
    expect(useCommentFocusStore.getState().expandedRoots["issue-2"]).toBeUndefined();
  });

  it("expandRoot is idempotent — repeating the same write keeps the same Set instance", () => {
    const { expandRoot } = useCommentFocusStore.getState();
    expandRoot("issue-1", "root-a");
    const first = useCommentFocusStore.getState().expandedRoots["issue-1"];
    // Deep-link rows remount repeatedly inside the highlight hold window;
    // each mount re-writes the same expansion. The store must not allocate
    // a fresh Set (which would also notify every subscriber) per rewrite.
    expandRoot("issue-1", "root-a");
    const second = useCommentFocusStore.getState().expandedRoots["issue-1"];
    expect(second).toBe(first);
  });

  it("collapseRoot is a no-op (same record) when the root is not expanded", () => {
    const { expandRoot, collapseRoot } = useCommentFocusStore.getState();
    expandRoot("issue-1", "root-a");
    const before = useCommentFocusStore.getState().expandedRoots;
    collapseRoot("issue-1", "root-never-expanded");
    expect(useCommentFocusStore.getState().expandedRoots).toBe(before);
    // Absent issue bucket: also a no-op, no record minted.
    collapseRoot("issue-404", "root-a");
    expect(
      useCommentFocusStore.getState().expandedRoots["issue-404"],
    ).toBeUndefined();
  });

  it("clearFocus clears the intent but keeps expansion state", () => {
    const { requestFocus, expandRoot, clearFocus } =
      useCommentFocusStore.getState();
    requestFocus("issue-1", "root-a");
    expandRoot("issue-1", "root-a");
    clearFocus();
    expect(useCommentFocusStore.getState().focus).toBeNull();
    expect(useCommentFocusStore.getState().expandedRoots["issue-1"]).toEqual(
      new Set(["root-a"]),
    );
  });

  it("resetIssue wipes both intent-by-issue bookkeeping and expansion for that issue only", () => {
    const { requestFocus, expandRoot, resetIssue } =
      useCommentFocusStore.getState();
    expandRoot("issue-1", "root-a");
    expandRoot("issue-2", "root-z");
    requestFocus("issue-2", "root-z");
    resetIssue("issue-2");
    const s = useCommentFocusStore.getState();
    expect(s.expandedRoots["issue-1"]).toEqual(new Set(["root-a"]));
    expect(s.expandedRoots["issue-2"]).toBeUndefined();
    expect(s.focus).toBeNull();
    expect(s.status).toBeNull();
  });
});
