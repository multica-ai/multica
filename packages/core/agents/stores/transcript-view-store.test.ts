import { beforeEach, describe, expect, it } from "vitest";
import { useTranscriptViewStore } from "./transcript-view-store";

beforeEach(() => {
  useTranscriptViewStore.setState({ sortDirection: "chronological" });
});

describe("useTranscriptViewStore", () => {
  it("defaults to chronological so existing readers see no behavior change", () => {
    expect(useTranscriptViewStore.getState().sortDirection).toBe("chronological");
  });

  it("setSortDirection switches between the two known directions", () => {
    const { setSortDirection } = useTranscriptViewStore.getState();

    setSortDirection("newest_first");
    expect(useTranscriptViewStore.getState().sortDirection).toBe("newest_first");

    setSortDirection("chronological");
    expect(useTranscriptViewStore.getState().sortDirection).toBe("chronological");
  });
});
