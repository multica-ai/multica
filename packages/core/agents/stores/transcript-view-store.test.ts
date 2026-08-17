import { beforeEach, describe, expect, it } from "vitest";
import { useTranscriptViewStore } from "./transcript-view-store";

beforeEach(() => {
  useTranscriptViewStore.setState({
    sortDirection: "chronological",
    selectedFilterKeys: [],
  });
});

describe("useTranscriptViewStore", () => {
  it("defaults to chronological and unfiltered", () => {
    expect(useTranscriptViewStore.getState().sortDirection).toBe("chronological");
    expect(useTranscriptViewStore.getState().selectedFilterKeys).toEqual([]);
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

  it("drops a persisted density from before the reading hierarchy replaced it", () => {
    const merge = useTranscriptViewStore.persist.getOptions().merge!;
    const current = useTranscriptViewStore.getState();

    // Expand mode is no longer a setting: agent prose reads open and tool
    // calls fold, so a stale persisted value must not resurface as state.
    expect(merge({ density: "collapsed", sortDirection: "newest_first" }, current)).toMatchObject({
      sortDirection: "newest_first",
    });
    expect(merge({ density: "collapsed" }, current)).not.toHaveProperty("density");
    expect(merge(undefined, current)).toMatchObject({ sortDirection: "chronological" });
  });
});
