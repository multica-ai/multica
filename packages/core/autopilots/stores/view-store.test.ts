// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { useAutopilotsViewStore } from "./view-store";

describe("useAutopilotsViewStore", () => {
  it("defaults to newest-created order", () => {
    const initialState = useAutopilotsViewStore.getInitialState();

    expect(initialState.sortField).toBe("created");
    expect(initialState.sortDirection).toBe("desc");
  });
});
