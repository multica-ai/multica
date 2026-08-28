// @vitest-environment node
import { describe, expect, it } from "vitest";
import type {
  CommentFocusIntent,
  CommentFocusStatus,
} from "@/data/stores/comment-focus-store";
import {
  preMountNonceOf,
  shouldAutoCloseOnLocated,
} from "./comment-directory-close";

// Plain literals + `import type` — the close handshake is pure decision
// math over the shapes the focus store publishes; no zustand is loaded.

const intent = (issueId: string, nonce: number): CommentFocusIntent => ({
  issueId,
  rootId: "root-a",
  nonce,
});

const located = (nonce: number): CommentFocusStatus => ({
  phase: "located",
  nonce,
});

describe("preMountNonceOf (mount-time snapshot of the store's existing intent)", () => {
  it("fresh session — no intent in the store at mount → -1", () => {
    expect(preMountNonceOf(null, "issue-1")).toBe(-1);
  });

  it("intent belonging to a different issue → -1 (not ours to guard)", () => {
    expect(preMountNonceOf(intent("issue-2", 3), "issue-1")).toBe(-1);
  });

  it("re-opened modal with a same-issue intent still in the store → that nonce", () => {
    expect(preMountNonceOf(intent("issue-1", 3), "issue-1")).toBe(3);
  });
});

describe("shouldAutoCloseOnLocated (directory modal close decision)", () => {
  const ours = intent("issue-1", 1);

  it("REGRESSION (RUYI-28 review round 1): fresh session, first-ever selection — a located for the just-minted nonce closes even though the snapshot is -1", () => {
    // Under the previous render-time `mountedNonceRef` snapshot the first
    // selection locked nonce 1 into the snapshot, making `1 > 1` false —
    // the session's first directory pick never auto-closed. The snapshot
    // must be the PRE-MOUNT value (-1), so located(1) closes.
    expect(shouldAutoCloseOnLocated(ours, located(1), "issue-1", -1)).toBe(
      true,
    );
  });

  it("re-opened modal — the previous visit's already-located intent must NOT replay into an unrequested close", () => {
    expect(
      shouldAutoCloseOnLocated(intent("issue-1", 4), located(4), "issue-1", 4),
    ).toBe(false);
  });

  it("re-opened modal, then a NEW selection — a located for the newer nonce closes", () => {
    expect(
      shouldAutoCloseOnLocated(intent("issue-1", 5), located(5), "issue-1", 4),
    ).toBe(true);
  });

  it("intent for another issue never closes this modal", () => {
    expect(
      shouldAutoCloseOnLocated(
        intent("issue-2", 7),
        located(7),
        "issue-1",
        -1,
      ),
    ).toBe(false);
  });

  it("pending / failed phases keep the modal open", () => {
    expect(
      shouldAutoCloseOnLocated(
        ours,
        { phase: "pending", nonce: 1 },
        "issue-1",
        -1,
      ),
    ).toBe(false);
    expect(
      shouldAutoCloseOnLocated(
        ours,
        { phase: "failed", nonce: 1, reason: "timeout" },
        "issue-1",
        -1,
      ),
    ).toBe(false);
  });

  it("status for a superseded nonce (late run of a re-tapped request) does not close", () => {
    expect(
      shouldAutoCloseOnLocated(intent("issue-1", 2), located(1), "issue-1", -1),
    ).toBe(false);
  });

  it("missing focus or status → no close", () => {
    expect(shouldAutoCloseOnLocated(null, located(1), "issue-1", -1)).toBe(
      false,
    );
    expect(shouldAutoCloseOnLocated(ours, null, "issue-1", -1)).toBe(false);
  });
});
