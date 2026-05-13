import { beforeEach, describe, expect, it } from "vitest";
import { useIssueDraftStore } from "./draft-store";

const RESET_STATE = {
  draft: {
    title: "",
    description: "",
    status: "todo" as const,
    priority: "none" as const,
    assigneeType: undefined,
    assigneeId: undefined,
    dueDate: null,
  },
};

describe("issue draft store", () => {
  beforeEach(() => {
    useIssueDraftStore.setState(RESET_STATE);
  });

  it("clearDraft resets the next draft back to empty and unassigned", () => {
    const { setDraft, clearDraft } = useIssueDraftStore.getState();

    setDraft({ title: "first", assigneeType: "member", assigneeId: "alice" });
    clearDraft();

    const { draft } = useIssueDraftStore.getState();
    expect(draft.title).toBe("");
    expect(draft.description).toBe("");
    expect(draft.assigneeType).toBeUndefined();
    expect(draft.assigneeId).toBeUndefined();
  });

  it("hasDraft only tracks typed title or description, not picker state", () => {
    const { setDraft, hasDraft } = useIssueDraftStore.getState();

    setDraft({ assigneeType: "member", assigneeId: "alice" });
    expect(hasDraft()).toBe(false);

    setDraft({ title: "first" });
    expect(hasDraft()).toBe(true);
  });

  it("clearDraft yields an empty assignee when none has ever been remembered", () => {
    const { setDraft, clearDraft } = useIssueDraftStore.getState();

    setDraft({ title: "first" });
    clearDraft();

    const { draft } = useIssueDraftStore.getState();
    expect(draft.assigneeType).toBeUndefined();
    expect(draft.assigneeId).toBeUndefined();
  });
});
