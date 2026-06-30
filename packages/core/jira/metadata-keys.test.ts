import { describe, expect, it } from "vitest";
import { JIRA_METADATA_KEYS } from "./metadata-keys";

describe("JIRA_METADATA_KEYS", () => {
  it("exposes the six sync keys with valid metadata key names", () => {
    const re = /^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$/;
    const values = Object.values(JIRA_METADATA_KEYS);
    expect(values).toEqual([
      "source",
      "jira_key",
      "jira_url",
      "jira_status",
      "jira_updated_at",
      "jira_comments_synced_at",
    ]);
    for (const v of values) expect(v).toMatch(re);
  });

  it("marks jira-sourced issues with the literal source value", () => {
    expect(JIRA_METADATA_KEYS.source).toBe("source");
  });
});
