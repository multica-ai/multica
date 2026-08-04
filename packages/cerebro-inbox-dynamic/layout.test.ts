// FIR-4350 — a persisted section whose feature flag is off must disappear, not
// fall through to the generic section renderer as an empty box.
import { describe, expect, it } from "vitest";
import { isSectionKindEnabled, type SectionFlagContext } from "./layout";

const ALL_ON: SectionFlagContext = { slackBlock: true, secretary: true, favorites: true };
const ALL_OFF: SectionFlagContext = { slackBlock: false, secretary: false, favorites: false };

describe("isSectionKindEnabled", () => {
  it("hides a stored Chat box when the slack-block flag is off", () => {
    expect(isSectionKindEnabled("team", ALL_OFF)).toBe(false);
    expect(isSectionKindEnabled("team", ALL_ON)).toBe(true);
  });

  it("hides Secretary and Favorites boxes when their flags are off", () => {
    expect(isSectionKindEnabled("secretary", ALL_OFF)).toBe(false);
    expect(isSectionKindEnabled("favorites", ALL_OFF)).toBe(false);
    expect(isSectionKindEnabled("secretary", ALL_ON)).toBe(true);
    expect(isSectionKindEnabled("favorites", ALL_ON)).toBe(true);
  });

  it("leaves ungated section kinds alone", () => {
    for (const kind of ["all", "unread", "act_now", "archived", "note"] as const) {
      expect(isSectionKindEnabled(kind, ALL_OFF)).toBe(true);
    }
  });
});
