import { describe, expect, it } from "vitest";
import { DEFAULT_SECRETARY_CRITERIA, readSecretaryCriteria } from "./secretary-criteria";

describe("readSecretaryCriteria", () => {
  it("returns the default when the blob is missing or not an object", () => {
    expect(readSecretaryCriteria(undefined)).toEqual(DEFAULT_SECRETARY_CRITERIA);
    expect(readSecretaryCriteria(null)).toEqual(DEFAULT_SECRETARY_CRITERIA);
    expect(readSecretaryCriteria("nope")).toEqual(DEFAULT_SECRETARY_CRITERIA);
  });

  it("keeps valid boolean fields and falls back per-field on garbage", () => {
    expect(
      readSecretaryCriteria({
        unreadOnly: true,
        includeChannels: false,
        includeIssues: "yes", // garbage → fall back to default (true)
        // includeChats missing → default (true)
      }),
    ).toEqual({
      unreadOnly: true,
      includeIssues: true,
      includeChannels: false,
      includeChats: true,
      startFolded: true, // missing → default (true)
    });
  });
});
