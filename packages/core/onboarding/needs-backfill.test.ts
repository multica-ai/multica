import { describe, expect, it } from "vitest";
import type { User } from "../types";
import {
  needsSourceBackfill,
  SOURCE_BACKFILL_MAX_DISMISSALS,
} from "./needs-backfill";

const BASE_USER: User = {
  id: "u1",
  name: "User",
  email: "u@example.com",
  avatar_url: null,
  onboarded_at: "2025-01-01T00:00:00Z",
  onboarding_questionnaire: {},
  starter_content_state: "imported",
  language: null,
  profile_description: "",
  timezone: null,
  created_at: "2025-01-01T00:00:00Z",
  updated_at: "2025-01-01T00:00:00Z",
};

function makeUser(partial: Partial<User> = {}): User {
  return { ...BASE_USER, ...partial };
}

describe("needsSourceBackfill", () => {
  it("returns false when no user", () => {
    expect(needsSourceBackfill(null, 0)).toBe(false);
    expect(needsSourceBackfill(undefined, 0)).toBe(false);
  });

  it("returns false when user has not onboarded yet", () => {
    const user = makeUser({ onboarded_at: null });
    expect(needsSourceBackfill(user, 0)).toBe(false);
  });

  it("returns true when onboarded with empty questionnaire", () => {
    const user = makeUser({ onboarding_questionnaire: {} });
    expect(needsSourceBackfill(user, 0)).toBe(true);
  });

  it("returns true when onboarded with source missing", () => {
    const user = makeUser({
      onboarding_questionnaire: { role: "engineer" },
    });
    expect(needsSourceBackfill(user, 0)).toBe(true);
  });

  it("returns true when source is an empty array", () => {
    const user = makeUser({
      onboarding_questionnaire: { source: [] },
    });
    expect(needsSourceBackfill(user, 0)).toBe(true);
  });

  it("returns false when source has at least one entry", () => {
    const user = makeUser({
      onboarding_questionnaire: { source: ["search"] },
    });
    expect(needsSourceBackfill(user, 0)).toBe(false);
  });

  it("returns false when user previously skipped the source step", () => {
    const user = makeUser({
      onboarding_questionnaire: { source: [], source_skipped: true },
    });
    expect(needsSourceBackfill(user, 0)).toBe(false);
  });

  it("returns false once dismissCount hits the cap", () => {
    const user = makeUser({ onboarding_questionnaire: {} });
    expect(
      needsSourceBackfill(user, SOURCE_BACKFILL_MAX_DISMISSALS),
    ).toBe(false);
    expect(
      needsSourceBackfill(user, SOURCE_BACKFILL_MAX_DISMISSALS + 5),
    ).toBe(false);
  });

  it("still returns true just below the dismiss cap", () => {
    const user = makeUser({ onboarding_questionnaire: {} });
    expect(
      needsSourceBackfill(user, SOURCE_BACKFILL_MAX_DISMISSALS - 1),
    ).toBe(true);
  });

  it("tolerates malformed source field", () => {
    const user = makeUser({
      onboarding_questionnaire: { source: "search" },
    });
    expect(needsSourceBackfill(user, 0)).toBe(true);
  });
});
