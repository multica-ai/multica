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
  lastAssigneeType: undefined,
  lastAssigneeId: undefined,
};

describe("issue draft store — last assignee", () => {
  beforeEach(() => {
    useIssueDraftStore.setState(RESET_STATE);
  });

  it("clearDraft prefills the next draft with the remembered assignee", () => {
    const { setDraft, setLastAssignee, clearDraft } =
      useIssueDraftStore.getState();

    setDraft({ title: "first", assigneeType: "member", assigneeId: "alice" });
    setLastAssignee("member", "alice");
    clearDraft();

    const { draft } = useIssueDraftStore.getState();
    expect(draft.title).toBe("");
    expect(draft.assigneeType).toBe("member");
    expect(draft.assigneeId).toBe("alice");
  });

  it("clearDraft yields an empty assignee when none has ever been remembered", () => {
    const { setDraft, clearDraft } = useIssueDraftStore.getState();

    setDraft({ title: "first" });
    clearDraft();

    const { draft } = useIssueDraftStore.getState();
    expect(draft.assigneeType).toBeUndefined();
    expect(draft.assigneeId).toBeUndefined();
  });

  it("setLastAssignee(undefined) lets the user opt back out of a default", () => {
    const { setLastAssignee, clearDraft } = useIssueDraftStore.getState();

    setLastAssignee("member", "alice");
    clearDraft();
    expect(useIssueDraftStore.getState().draft.assigneeId).toBe("alice");

    setLastAssignee(undefined, undefined);
    clearDraft();
    expect(useIssueDraftStore.getState().draft.assigneeId).toBeUndefined();
    expect(useIssueDraftStore.getState().draft.assigneeType).toBeUndefined();
  });
});
