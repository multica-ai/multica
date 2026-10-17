// @vitest-environment node
import { describe, expect, it } from "vitest";
import { createStore } from "zustand/vanilla";
import {
  mergeViewStatePersisted,
  viewStorePersistOptions,
  viewStoreSlice,
  type IssueViewState,
} from "./view-store";

function createViewStore() {
  return createStore<IssueViewState>()((set) => viewStoreSlice(set));
}

describe("board column preferences", () => {
  it("retains the manual width when fitting to the screen and persists both settings", () => {
    const store = createViewStore();
    store.getState().setBoardColumnWidth(240);
    store.getState().setBoardFitToScreen(true);
    const snapshot = viewStorePersistOptions("board-width-test").partialize(store.getState());
    const restored = mergeViewStatePersisted(snapshot, createViewStore().getState());
    expect(restored.boardColumnWidth).toBe(240);
    expect(restored.boardFitToScreen).toBe(true);
    store.getState().setBoardFitToScreen(false);
    expect(store.getState().boardColumnWidth).toBe(240);
  });

  it("keeps older snapshots at the existing 280 pixel width", () => {
    const restored = mergeViewStatePersisted({ viewMode: "board" }, createViewStore().getState());
    expect(restored.boardColumnWidth).toBe(280);
    expect(restored.boardFitToScreen).toBe(false);
  });

  it.each([
    [100, 200], [800, 480], [250.5, 251], [NaN, 280], [Infinity, 280], ["240", 280],
  ])("normalizes a stored width of %s to %s", (value, expected) => {
    const restored = mergeViewStatePersisted(
      { boardColumnWidth: value, boardFitToScreen: "true" },
      createViewStore().getState(),
    );
    expect(restored.boardColumnWidth).toBe(expected);
    expect(restored.boardFitToScreen).toBe(false);
  });
});
