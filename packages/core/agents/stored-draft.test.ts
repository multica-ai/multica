import { describe, expect, it } from "vitest";
import type { AgentDraft } from "./draft";
import {
  fromStoredAgentDraft,
  storedAgentDraftsEqual,
  toStoredAgentDraft,
} from "./stored-draft";

const draft = (): AgentDraft => ({
  name: "Release manager",
  description: "Ships carefully",
  instructions: "# Role\nShip.",
  avatarUrl: "🚀",
  runtimeId: "runtime-1",
  model: "gpt-5.6-sol",
  thinkingLevel: "high",
  serviceTier: "priority",
  skillIds: new Set(["skill-1", "skill-2"]),
  permissionScope: "members",
  memberIds: new Set(["member-1"]),
  teamIds: new Set(["team-1"]),
});

describe("stored agent draft", () => {
  it("round-trips every editable field", () => {
    const original = draft();
    const restored = fromStoredAgentDraft(
      toStoredAgentDraft(original, "msg-1"),
      original.runtimeId,
    );
    expect(restored).toEqual(original);
  });

  // The runtime a conversation executes on is owned by its carrier agent. A
  // copy inside the draft could only go stale and put the picker on a runtime
  // that runs nothing (MUL-5163), so the restore takes it from the caller.
  it("takes the runtime from the caller, not the stored copy", () => {
    const stored = toStoredAgentDraft(draft(), null);
    expect(stored).not.toHaveProperty("runtimeId");
    expect(fromStoredAgentDraft(stored, "runtime-2").runtimeId).toBe(
      "runtime-2",
    );
  });

  // The marker is what stops a restore from re-applying the last reply over
  // edits made after it, so it has to survive the round trip.
  it("carries the applied message marker", () => {
    expect(toStoredAgentDraft(draft(), "msg-9").applied_message_id).toBe(
      "msg-9",
    );
    expect(toStoredAgentDraft(draft(), null).applied_message_id).toBeNull();
  });

  // Autosave compares on the stored form: a Set rebuilt with the same members
  // is a new reference every render and would otherwise write on every keystroke
  // elsewhere in the form.
  it("treats rebuilt sets with the same members as unchanged", () => {
    expect(
      storedAgentDraftsEqual(
        toStoredAgentDraft(draft(), "msg-1"),
        toStoredAgentDraft(draft(), "msg-1"),
      ),
    ).toBe(true);
    expect(
      storedAgentDraftsEqual(
        toStoredAgentDraft(draft(), "msg-1"),
        toStoredAgentDraft({ ...draft(), name: "Other" }, "msg-1"),
      ),
    ).toBe(false);
  });
});
