import { afterEach, describe, expect, it, vi } from "vitest";
import { computeInviteEligibility } from "./invite-eligibility";
import type { WorkspaceMember } from "./types";

function makeMember(overrides: Partial<WorkspaceMember> = {}): WorkspaceMember {
  return {
    id: "user-1",
    name: "Jane Doe",
    email: "jane@example.com",
    role: "member",
    ...overrides,
  };
}

describe("computeInviteEligibility", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  it("is eligible when BOT_PAT is set and the bot account is an owner", () => {
    vi.stubEnv("BOT_PAT", "test-pat");
    const members = [makeMember({ email: "agentfarm-bot@g2.com", role: "owner" })];
    expect(computeInviteEligibility(members)).toEqual({
      eligible: true,
      botEmail: "agentfarm-bot@g2.com",
      reason: null,
    });
  });

  it("is eligible when the bot account is an admin (not just owner)", () => {
    vi.stubEnv("BOT_PAT", "test-pat");
    const members = [makeMember({ email: "agentfarm-bot@g2.com", role: "admin" })];
    expect(computeInviteEligibility(members).eligible).toBe(true);
  });

  it("is ineligible with reason 'pat-missing' when BOT_PAT is missing, even if the bot is an owner", () => {
    vi.stubEnv("BOT_PAT", "");
    const members = [makeMember({ email: "agentfarm-bot@g2.com", role: "owner" })];
    expect(computeInviteEligibility(members)).toMatchObject({ eligible: false, reason: "pat-missing" });
  });

  it("is ineligible with reason 'not-workspace-admin' when the bot account is only a plain member", () => {
    vi.stubEnv("BOT_PAT", "test-pat");
    const members = [makeMember({ email: "agentfarm-bot@g2.com", role: "member" })];
    expect(computeInviteEligibility(members)).toMatchObject({ eligible: false, reason: "not-workspace-admin" });
  });

  it("is ineligible with reason 'not-workspace-admin' when the bot account isn't a member of the workspace at all", () => {
    vi.stubEnv("BOT_PAT", "test-pat");
    expect(computeInviteEligibility([makeMember({ email: "someone-else@g2.com", role: "owner" })])).toMatchObject({
      eligible: false,
      reason: "not-workspace-admin",
    });
  });

  it("matches the bot email case-insensitively", () => {
    vi.stubEnv("BOT_PAT", "test-pat");
    const members = [makeMember({ email: "AgentFarm-Bot@G2.com", role: "owner" })];
    expect(computeInviteEligibility(members).eligible).toBe(true);
  });

  it("honors an AGENTFARM_BOT_EMAIL override", () => {
    vi.stubEnv("BOT_PAT", "test-pat");
    vi.stubEnv("AGENTFARM_BOT_EMAIL", "custom-bot@g2.com");
    const members = [makeMember({ email: "custom-bot@g2.com", role: "admin" })];
    const result = computeInviteEligibility(members);
    expect(result).toEqual({ eligible: true, botEmail: "custom-bot@g2.com", reason: null });
  });
});
