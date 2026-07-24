import { describe, it, expect, beforeEach } from "vitest";
import { createDraftStore } from "./create-draft-store";
import {
  resetAllRegisteredDrafts,
  __clearDraftCleanupRegistryForTest,
  __getRegisteredDraftKeysForTest,
} from "./cleanup-registry";

interface Sample {
  title: string;
  tags: string[];
}

const EMPTY: Sample = { title: "", tags: [] };

beforeEach(() => {
  __clearDraftCleanupRegistryForTest();
});

describe("createDraftStore", () => {
  it("merges patches and reports meaningful drafts via hasMeaningful", () => {
    const useStore = createDraftStore<Sample>({
      storageKey: "t_merge",
      emptyData: EMPTY,
      hasMeaningful: (d) => !!d.title || d.tags.length > 0,
      workspaceScoped: false,
    });

    expect(useStore.getState().hasDraft()).toBe(false);
    useStore.getState().setDraft({ title: "hi" });
    expect(useStore.getState().draft).toEqual({ title: "hi", tags: [] });
    expect(useStore.getState().hasDraft()).toBe(true);
  });

  it("clearDraft resets to a fresh empty draft that does not share nested references", () => {
    const useStore = createDraftStore<Sample>({
      storageKey: "t_clear",
      emptyData: EMPTY,
      hasMeaningful: (d) => d.tags.length > 0,
      workspaceScoped: false,
    });

    useStore.getState().setDraft({ tags: ["a"] });
    useStore.getState().clearDraft();
    expect(useStore.getState().draft.tags).toEqual([]);

    // Mutating the cleared array must not corrupt EMPTY for the next clear.
    useStore.getState().draft.tags.push("leak");
    useStore.getState().setDraft({ tags: ["b"] });
    useStore.getState().clearDraft();
    expect(useStore.getState().draft.tags).toEqual([]);
  });

  it("self-registers for cleanup and reset wipes in-memory state", () => {
    const useStore = createDraftStore<Sample>({
      storageKey: "t_register",
      emptyData: EMPTY,
      hasMeaningful: (d) => !!d.title,
      workspaceScoped: false,
    });

    expect(__getRegisteredDraftKeysForTest()).toContain("t_register");

    useStore.getState().setDraft({ title: "leaky" });
    resetAllRegisteredDrafts();
    expect(useStore.getState().draft.title).toBe("");
  });
});
