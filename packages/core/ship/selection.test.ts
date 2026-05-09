// Phase 7a — selection store unit tests. Pure state, no React.
//
// We exercise toggle / add / remove / selectAll / clear / has and
// verify reference stability where the store contract claims it
// (clear on already-empty does nothing; toggle to existing returns
// the same object identity nowhere — but does flip Set membership).

import { describe, it, expect, beforeEach } from "vitest";
import { useShipSelection } from "./selection";

describe("ship selection store", () => {
  beforeEach(() => {
    useShipSelection.getState().clear();
  });

  it("starts empty", () => {
    const s = useShipSelection.getState();
    expect(s.selected.size).toBe(0);
  });

  it("toggle adds and removes", () => {
    const s = useShipSelection.getState();
    s.toggle("pr-1");
    expect(useShipSelection.getState().has("pr-1")).toBe(true);
    s.toggle("pr-1");
    expect(useShipSelection.getState().has("pr-1")).toBe(false);
  });

  it("selectAll replaces the set", () => {
    const s = useShipSelection.getState();
    s.add("pr-old");
    s.selectAll(["pr-1", "pr-2", "pr-3"]);
    const next = useShipSelection.getState();
    expect(next.has("pr-old")).toBe(false);
    expect(next.has("pr-2")).toBe(true);
    expect(next.selected.size).toBe(3);
  });

  it("clear is idempotent on empty selection", () => {
    const before = useShipSelection.getState().selected;
    useShipSelection.getState().clear();
    const after = useShipSelection.getState().selected;
    // Same reference — no fresh Set when nothing changed.
    expect(after).toBe(before);
  });

  it("add is idempotent", () => {
    const s = useShipSelection.getState();
    s.add("pr-1");
    const first = useShipSelection.getState().selected;
    s.add("pr-1");
    const second = useShipSelection.getState().selected;
    // Same reference because the second add was a no-op.
    expect(second).toBe(first);
  });

  it("remove of non-member is a no-op", () => {
    const s = useShipSelection.getState();
    s.add("pr-1");
    const before = useShipSelection.getState().selected;
    s.remove("pr-not-there");
    const after = useShipSelection.getState().selected;
    expect(after).toBe(before);
  });
});
