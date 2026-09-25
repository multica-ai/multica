import { describe, expect, it } from "vitest";
import { EMPTY_AGENT_DRAFT } from "./draft";
import {
  MANUAL_DRAFT_BLANK_OWNER,
  manualDraftEntryHasContent,
  manualDraftOwner,
  setManualDraftEntry,
  type ManualAgentDrafts,
  type ManualDraftEntry,
} from "./manual-draft-store";
import { toStoredAgentDraft } from "./stored-draft";

const entry = (
  overrides: Partial<typeof EMPTY_AGENT_DRAFT> = {},
): ManualDraftEntry => ({
  runtimeId: "runtime-1",
  draft: toStoredAgentDraft({ ...EMPTY_AGENT_DRAFT, ...overrides }, null),
});

describe("manual agent drafts", () => {
  // A blank agent and a copy of agent X are different pieces of work, and a
  // copy of X is not a copy of Y.
  it("scopes a draft to what is being created", () => {
    expect(manualDraftOwner(null)).toBe(MANUAL_DRAFT_BLANK_OWNER);
    expect(manualDraftOwner("agent-1")).toBe("duplicate:agent-1");
    expect(manualDraftOwner("agent-1")).not.toBe(manualDraftOwner("agent-2"));
  });

  // The regression this keying exists for: with one shared slot, opening a
  // second flow overwrote the first before the user typed a character.
  it("leaves other owners' drafts alone", () => {
    const drafts: ManualAgentDrafts = setManualDraftEntry(
      { byOwner: {} },
      "duplicate:agent-A",
      entry({ name: "Half-finished copy of A" }),
    );

    const afterOpeningBlank = setManualDraftEntry(
      drafts,
      MANUAL_DRAFT_BLANK_OWNER,
      entry({ name: "Something else" }),
    );
    const afterOpeningB = setManualDraftEntry(
      afterOpeningBlank,
      "duplicate:agent-B",
      entry({ name: "Copy of B" }),
    );

    expect(afterOpeningB.byOwner["duplicate:agent-A"]?.draft.name).toBe(
      "Half-finished copy of A",
    );
    expect(Object.keys(afterOpeningB.byOwner).sort()).toEqual([
      MANUAL_DRAFT_BLANK_OWNER,
      "duplicate:agent-A",
      "duplicate:agent-B",
    ]);
  });

  // The runtime is seeded automatically on every visit. Counting it as content
  // would store a slot for a form nobody touched — and would grow a dead key
  // for every agent ever opened for duplication.
  it("stores nothing for a form the user has not touched", () => {
    const drafts = setManualDraftEntry({ byOwner: {} }, "blank", entry());
    expect(drafts.byOwner).toEqual({});
    expect(manualDraftEntryHasContent(entry())).toBe(false);
  });

  // Emptying the form removes its slot rather than parking a blank one.
  it("drops a slot once its content is gone", () => {
    const withContent = setManualDraftEntry(
      { byOwner: {} },
      "blank",
      entry({ name: "Typed" }),
    );
    expect(withContent.byOwner.blank).toBeDefined();

    const emptied = setManualDraftEntry({ ...withContent }, "blank", entry());
    expect(emptied.byOwner.blank).toBeUndefined();

    const discarded = setManualDraftEntry({ ...withContent }, "blank", null);
    expect(discarded.byOwner.blank).toBeUndefined();
  });

  // Every field the form can change, one at a time. An earlier version listed
  // six of them by hand and silently dropped the rest: picking a model before
  // typing a name, or setting access first, deleted the slot on the next save.
  it.each([
    ["name", { name: "Release" }],
    ["description", { description: "Ships carefully" }],
    ["instructions", { instructions: "Be careful" }],
    ["avatar", { avatarUrl: "🚀" }],
    ["model", { model: "gpt-5.6-sol" }],
    ["thinking level", { thinkingLevel: "high" }],
    ["service tier", { serviceTier: "priority" }],
    ["access scope", { permissionScope: "workspace" as const }],
    ["skills", { skillIds: new Set(["skill-1"]) }],
    ["members", { memberIds: new Set(["member-1"]) }],
    ["teams", { teamIds: new Set(["team-1"]) }],
  ])("keeps a draft whose only edit is the %s", (_label, overrides) => {
    expect(manualDraftEntryHasContent(entry(overrides))).toBe(true);
    expect(
      setManualDraftEntry({ byOwner: {} }, "blank", entry(overrides)).byOwner
        .blank,
    ).toBeDefined();
  });

  // The runtime is the one field that must not count: the form seeds it on
  // every visit, so counting it would store a draft for a form nobody touched.
  it("does not count the auto-seeded runtime", () => {
    expect(
      manualDraftEntryHasContent({ ...entry(), runtimeId: "runtime-9" }),
    ).toBe(false);
  });
});
