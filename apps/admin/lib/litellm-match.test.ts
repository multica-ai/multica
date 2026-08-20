import { describe, expect, it } from "vitest";
import { findKeyForSlug } from "./litellm-match";
import type { LiteLlmKey } from "./litellm-schema";

describe("findKeyForSlug", () => {
  it("matches Gandalf's agentfarm-<slug> key_alias convention", () => {
    const keys: LiteLlmKey[] = [{ key_alias: "agentfarm-roi-ppc", team_alias: null, team_id: null }];
    expect(findKeyForSlug(keys, "roi-ppc")).toBe(keys[0]);
  });

  it("does not match on the bare slug (key_alias always carries the agentfarm- prefix)", () => {
    const keys: LiteLlmKey[] = [{ key_alias: "roi-ppc", team_alias: null, team_id: null }];
    expect(findKeyForSlug(keys, "roi-ppc")).toBeNull();
  });

  it("does not substring-match a longer alias for a shorter slug", () => {
    const keys: LiteLlmKey[] = [
      { key_alias: "agentfarm-ankesh-thakur-automations", team_alias: null, team_id: null },
    ];
    expect(findKeyForSlug(keys, "ankesh-thakur")).toBeNull();
  });

  it("returns null when no key matches", () => {
    expect(findKeyForSlug([], "roi-ppc")).toBeNull();
  });
});
