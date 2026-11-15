import { describe, expect, it } from "vitest";
import { compareVersions, parseVersion } from "@multica/core/whats-new";
import { WHATS_NEW_DECK_SINCE, WHATS_NEW_RELEASES } from "./releases";

describe("WHATS_NEW_RELEASES", () => {
  it("gives every release since the deck at least one highlight with hero art", () => {
    const since = parseVersion(WHATS_NEW_DECK_SINCE)!;
    const missing = WHATS_NEW_RELEASES.filter(
      (r) => compareVersions(parseVersion(r.version)!, since) >= 0 && !r.highlights.some((h) => h.hero),
    ).map((r) => r.version);
    expect(missing).toEqual([]);
  });
});
