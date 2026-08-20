import { describe, expect, it } from "vitest";
import { findKeyForSlug, resolveTeamName } from "./litellm-match";
import type { LiteLlmKey, LiteLlmTeam } from "./litellm-schema";

describe("findKeyForSlug", () => {
  it("matches Gandalf's agentfarm-<slug> key_alias convention", () => {
    const keys: LiteLlmKey[] = [{ key_alias: "agentfarm-roi-ppc", team_id: null }];
    expect(findKeyForSlug(keys, "roi-ppc")).toBe(keys[0]);
  });

  it("does not match on the bare slug (key_alias always carries the agentfarm- prefix)", () => {
    const keys: LiteLlmKey[] = [{ key_alias: "roi-ppc", team_id: null }];
    expect(findKeyForSlug(keys, "roi-ppc")).toBeNull();
  });

  it("does not substring-match a longer alias for a shorter slug", () => {
    const keys: LiteLlmKey[] = [{ key_alias: "agentfarm-ankesh-thakur-automations", team_id: null }];
    expect(findKeyForSlug(keys, "ankesh-thakur")).toBeNull();
  });

  it("returns null when no key matches", () => {
    expect(findKeyForSlug([], "roi-ppc")).toBeNull();
  });
});

describe("resolveTeamName", () => {
  it("resolves a key's team_id to the team's human-readable alias", () => {
    const teams: LiteLlmTeam[] = [{ team_id: "abc-123", team_alias: "Digital Acquisition" }];
    expect(resolveTeamName(teams, "abc-123")).toBe("Digital Acquisition");
  });

  it("returns null for a team_id with no matching team", () => {
    const teams: LiteLlmTeam[] = [{ team_id: "abc-123", team_alias: "Digital Acquisition" }];
    expect(resolveTeamName(teams, "unknown-id")).toBeNull();
  });

  it("returns null when team_id is null or undefined", () => {
    const teams: LiteLlmTeam[] = [{ team_id: "abc-123", team_alias: "Digital Acquisition" }];
    expect(resolveTeamName(teams, null)).toBeNull();
    expect(resolveTeamName(teams, undefined)).toBeNull();
  });

  it("returns null when the matching team has no alias", () => {
    const teams: LiteLlmTeam[] = [{ team_id: "abc-123", team_alias: null }];
    expect(resolveTeamName(teams, "abc-123")).toBeNull();
  });
});
