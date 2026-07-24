import { beforeEach, describe, expect, it } from "vitest";
import { useTranscriptViewStore } from "./transcript-view-store";

beforeEach(() => {
  useTranscriptViewStore.setState({
    sortDirection: "chronological",
    selectedFilterKeys: [],
    density: "smart",
  });
});

describe("useTranscriptViewStore", () => {
  it("defaults to chronological, unfiltered, and smart density", () => {
    expect(useTranscriptViewStore.getState().sortDirection).toBe("chronological");
    expect(useTranscriptViewStore.getState().selectedFilterKeys).toEqual([]);
    expect(useTranscriptViewStore.getState().density).toBe("smart");
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

  it("stores the density preference", () => {
    const { setDensity } = useTranscriptViewStore.getState();

    setDensity("expanded");
    expect(useTranscriptViewStore.getState().density).toBe("expanded");

    setDensity("collapsed");
    expect(useTranscriptViewStore.getState().density).toBe("collapsed");
  });

  it("migrates the legacy defaultExpanded boolean and rejects unknown density values", () => {
    const merge = useTranscriptViewStore.persist.getOptions().merge!;
    const current = useTranscriptViewStore.getState();

    expect(merge({ defaultExpanded: true }, current)).toMatchObject({ density: "expanded" });
    expect(merge({ defaultExpanded: false }, current)).toMatchObject({ density: "smart" });
    expect(merge({ density: "collapsed" }, current)).toMatchObject({ density: "collapsed" });
    expect(merge({ density: "bogus" }, current)).toMatchObject({ density: "smart" });
    expect(merge(undefined, current)).toMatchObject({ density: "smart" });
  });
});
