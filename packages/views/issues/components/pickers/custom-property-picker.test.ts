import { describe, it, expect } from "vitest";
import type { IssueProperty } from "@multica/core/types";
import { isCustomPropertyReadOnly } from "./custom-property-picker";

const MEMBER = "member:11111111-1111-4111-8111-111111111111";
const AGENT = "agent:99999999-9999-4999-8999-999999999999";

function property(overrides: Partial<IssueProperty>): IssueProperty {
  return {
    id: "p-1",
    workspace_id: "ws-1",
    name: "Reviewer",
    type: "actor",
    config: {},
    position: 1,
    archived: false,
    created_at: "2026-08-17T00:00:00Z",
    updated_at: "2026-08-17T00:00:00Z",
    ...overrides,
  };
}

describe("isCustomPropertyReadOnly", () => {
  it("allows editing a normal actor value", () => {
    expect(isCustomPropertyReadOnly(property({}), MEMBER)).toBe(false);
    expect(isCustomPropertyReadOnly(property({}), undefined)).toBe(false);
  });

  it("locks an archived definition", () => {
    expect(isCustomPropertyReadOnly(property({ archived: true }), MEMBER)).toBe(true);
  });

  it("locks a type newer than this build", () => {
    expect(isCustomPropertyReadOnly(property({ type: "relation" }), "whatever")).toBe(true);
  });

  // The regression Elon flagged in round two: a single actor value referencing
  // a kind this build cannot parse renders as empty. If it stayed editable the
  // user would "fill in an empty field" and overwrite a value never shown to
  // them — a silent delete.
  it("locks a single actor value whose kind it cannot parse", () => {
    expect(isCustomPropertyReadOnly(property({}), AGENT)).toBe(true);
  });

  // multi_actor is exempt: its toggle round-trips unknown entries instead of
  // replacing the whole value, so editing stays safe and stays available.
  it("keeps multi_actor editable even with an unknown kind present", () => {
    const multi = property({ type: "multi_actor" });
    expect(isCustomPropertyReadOnly(multi, [MEMBER, AGENT])).toBe(false);
    expect(isCustomPropertyReadOnly(multi, [AGENT])).toBe(false);
  });
});
