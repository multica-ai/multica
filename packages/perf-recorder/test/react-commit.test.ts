import { describe, expect, it } from "vitest";
import { extractCommitEvidence } from "../src/react-commit";
import type { CommitRoot } from "../src/install";

const PROFILER_TAG = 12;

// Build a synthetic fiber tree. Only the fields the extractor is allowed to read
// are set; any secret is placed in a forbidden field to prove it is never read.
function fiber(partial: Record<string, unknown>): Record<string, unknown> {
  return { tag: 0, actualDuration: 0, alternate: {}, child: null, sibling: null, ...partial };
}

describe("extractCommitEvidence", () => {
  it("reads commit actualDuration and phase off the root", () => {
    const root: CommitRoot = { current: fiber({ actualDuration: 42, alternate: null }) };
    const out = extractCommitEvidence(root, new Set());
    expect(out.commitActualDurationMs).toBe(42);
    expect(out.phase).toBe("mount");
  });

  it("attributes only host-registered Profiler boundaries", () => {
    const registered = fiber({
      tag: PROFILER_TAG,
      actualDuration: 30,
      memoizedProps: { id: "issue-detail" },
    });
    const unregistered = fiber({
      tag: PROFILER_TAG,
      actualDuration: 99,
      memoizedProps: { id: "secret-area" },
    });
    registered.sibling = unregistered;
    const root: CommitRoot = { current: fiber({ actualDuration: 129, child: registered }) };

    const out = extractCommitEvidence(root, new Set(["issue-detail"]));
    expect(out.boundaries).toHaveLength(1);
    expect(out.boundaries[0]).toMatchObject({ boundaryId: "issue-detail", actualDurationMs: 30 });
  });

  it("never reads non-id props / state / context — only a Profiler's own id", () => {
    let sensitiveRead = false;
    const props = {
      get value() {
        sensitiveRead = true;
        return "user typed secret";
      },
      get children() {
        sensitiveRead = true;
        return "dom text";
      },
      id: "issue-detail",
    };
    const profiler = fiber({ tag: PROFILER_TAG, actualDuration: 20, memoizedProps: props });
    // A non-Profiler fiber whose memoizedProps must NOT be touched at all.
    // Defined via defineProperty so the getter is not triggered by object spread
    // at construction time — only a real read would flip the flag.
    const host = fiber({ tag: 5, actualDuration: 5 });
    Object.defineProperty(host, "memoizedProps", {
      enumerable: true,
      get() {
        sensitiveRead = true;
        return { onClick: () => {} };
      },
    });
    profiler.sibling = host;
    const root: CommitRoot = { current: fiber({ child: profiler }) };

    const out = extractCommitEvidence(root, new Set(["issue-detail"]));
    expect(out.boundaries[0]?.boundaryId).toBe("issue-detail");
    // reading `.id` off the Profiler props is expected; value/children/host props are not.
    expect(sensitiveRead).toBe(false);
  });

  it("emits nothing for boundaries when the allowlist is empty", () => {
    const profiler = fiber({ tag: PROFILER_TAG, actualDuration: 50, memoizedProps: { id: "x" } });
    const root: CommitRoot = { current: fiber({ child: profiler }) };
    expect(extractCommitEvidence(root, new Set()).boundaries).toHaveLength(0);
  });
});
