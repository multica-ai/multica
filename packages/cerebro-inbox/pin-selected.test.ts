import { describe, expect, it } from "vitest";
import { sortInboxEntriesPinned, type PinnedSelection } from "./pin-selected";

type Row = { key: string; time: number };
const keyOf = (r: Row) => r.key;

function ref(initial: PinnedSelection | null = null): {
  current: PinnedSelection | null;
} {
  return { current: initial };
}

describe("sortInboxEntriesPinned", () => {
  it("sorts newest-first when nothing is selected", () => {
    const rows: Row[] = [
      { key: "a", time: 1 },
      { key: "b", time: 3 },
      { key: "c", time: 2 },
    ];
    const out = sortInboxEntriesPinned(rows, keyOf, "", ref());
    expect(out.map((r) => r.key)).toEqual(["b", "c", "a"]);
  });

  it("keeps the open row in place when its activity time grows", () => {
    const pin = ref();
    const initial: Row[] = [
      { key: "a", time: 10 },
      { key: "b", time: 8 },
      { key: "c", time: 6 },
    ];
    // Open "b" — it stays at index 1.
    const first = sortInboxEntriesPinned(initial, keyOf, "b", pin);
    expect(first.map((r) => r.key)).toEqual(["a", "b", "c"]);
    expect(pin.current).toEqual({ key: "b", time: 8 });

    // A new comment bumps "b" to the newest time; without pinning it would jump
    // to the top. With the pin it stays put.
    const bumped: Row[] = [
      { key: "a", time: 10 },
      { key: "b", time: 99 },
      { key: "c", time: 6 },
    ];
    const after = sortInboxEntriesPinned(bumped, keyOf, "b", pin);
    expect(after.map((r) => r.key)).toEqual(["a", "b", "c"]);
  });

  it("re-sorts the row to its natural position once it is closed", () => {
    const pin = ref({ key: "b", time: 8 });
    const bumped: Row[] = [
      { key: "a", time: 10 },
      { key: "b", time: 99 },
      { key: "c", time: 6 },
    ];
    const out = sortInboxEntriesPinned(bumped, keyOf, "", pin);
    expect(out.map((r) => r.key)).toEqual(["b", "a", "c"]);
    expect(pin.current).toBeNull();
  });

  it("re-pins when the selection moves to a different row", () => {
    const pin = ref({ key: "b", time: 8 });
    const rows: Row[] = [
      { key: "a", time: 10 },
      { key: "b", time: 8 },
      { key: "c", time: 6 },
    ];
    sortInboxEntriesPinned(rows, keyOf, "c", pin);
    expect(pin.current).toEqual({ key: "c", time: 6 });
  });

  it("does not mutate the input array", () => {
    const rows: Row[] = [
      { key: "a", time: 1 },
      { key: "b", time: 2 },
    ];
    const snapshot = [...rows];
    sortInboxEntriesPinned(rows, keyOf, "", ref());
    expect(rows).toEqual(snapshot);
  });
});
