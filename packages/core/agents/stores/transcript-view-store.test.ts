import { beforeEach, describe, expect, it } from "vitest";
import { useTranscriptViewStore } from "./transcript-view-store";

beforeEach(() => {
  useTranscriptViewStore.setState({
    sortDirection: "chronological",
    selectedFilterKeys: [],
    defaultExpanded: false,
  });
});

describe("useTranscriptViewStore", () => {
  it("defaults to chronological, unfiltered, and collapsed", () => {
    expect(useTranscriptViewStore.getState().sortDirection).toBe("chronological");
    expect(useTranscriptViewStore.getState().selectedFilterKeys).toEqual([]);
    expect(useTranscriptViewStore.getState().defaultExpanded).toBe(false);
  });

  it("setSortDirection switches between the two known directions", () => {
    const { setSortDirection } = useTranscriptViewStore.getState();

    setSortDirection("newest_first");
    expect(useTranscriptViewStore.getState().sortDirection).toBe("newest_first");

    setSortDirection("chronological");
    expect(useTranscriptViewStore.getState().sortDirection).toBe("chronological");
  });

  it("stores filter preferences as unique serializable keys", () => {
    const { setSelectedFilterKeys, toggleFilterKey, clearFilterKeys } =
      useTranscriptViewStore.getState();

    setSelectedFilterKeys(["thinking", "tool:terminal", "thinking", ""]);
    expect(useTranscriptViewStore.getState().selectedFilterKeys).toEqual([
      "thinking",
      "tool:terminal",
    ]);

    toggleFilterKey("thinking");
    expect(useTranscriptViewStore.getState().selectedFilterKeys).toEqual(["tool:terminal"]);

    toggleFilterKey("text");
    expect(useTranscriptViewStore.getState().selectedFilterKeys).toEqual([
      "tool:terminal",
      "text",
    ]);

    clearFilterKeys();
    expect(useTranscriptViewStore.getState().selectedFilterKeys).toEqual([]);
  });

  it("stores the default-expanded preference", () => {
    const { setDefaultExpanded } = useTranscriptViewStore.getState();

    setDefaultExpanded(true);
    expect(useTranscriptViewStore.getState().defaultExpanded).toBe(true);

    setDefaultExpanded(false);
    expect(useTranscriptViewStore.getState().defaultExpanded).toBe(false);
  });
});
