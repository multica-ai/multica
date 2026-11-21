import { describe, expect, it } from "vitest";
import { SUPPORTED_LOCALES } from "@multica/core/i18n";
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

  it("provides translated title and body for every supported locale on every highlight", () => {
    for (const release of WHATS_NEW_RELEASES) {
      for (const highlight of release.highlights) {
        for (const locale of SUPPORTED_LOCALES) {
          const entry = highlight.copy[locale];
          expect(entry, `${release.version}/${highlight.id} missing ${locale}`).toBeDefined();
          expect(entry.title.trim().length, `${release.version}/${highlight.id}/${locale} empty title`).toBeGreaterThan(0);
          expect(entry.body.trim().length, `${release.version}/${highlight.id}/${locale} empty body`).toBeGreaterThan(0);
        }
      }
    }
  });
});
