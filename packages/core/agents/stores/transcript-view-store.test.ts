import { beforeEach, describe, expect, it } from "vitest";
import { useTranscriptViewStore } from "./transcript-view-store";

beforeEach(() => {
  useTranscriptViewStore.setState(useTranscriptViewStore.getInitialState(), true);
});

describe("useTranscriptViewStore", () => {
  it("defaults to newest first so the latest execution events are visible", () => {
    expect(useTranscriptViewStore.getState().sortDirection).toBe("newest_first");
  });

  it("setSortDirection switches between the two known directions", () => {
    const { setSortDirection } = useTranscriptViewStore.getState();

    setSortDirection("newest_first");
    expect(useTranscriptViewStore.getState().sortDirection).toBe("newest_first");

    setSortDirection("chronological");
    expect(useTranscriptViewStore.getState().sortDirection).toBe("chronological");
  });
});
