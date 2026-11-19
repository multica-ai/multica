// @vitest-environment node
import { describe, expect, it } from "vitest";

import { ApiError } from "../api";
import { MemberLimitSchema } from "../api/schemas";
import type { MemberLimit, StorageAdapter } from "../types";
import {
  clearUpgradeInvite,
  isMemberLimitFull,
  isMemberLimitReachedError,
  readUpgradeInvite,
  saveUpgradeInvite,
} from "./member-limit";

const OFF: MemberLimit = { action: "off", limit: null, members: null, pendingInvitations: null };

function memoryStorage(): StorageAdapter & { data: Map<string, string> } {
  const data = new Map<string, string>();
  return {
    data,
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => void data.set(key, value),
    removeItem: (key) => void data.delete(key),
    keys: () => [...data.keys()],
  };
}

describe("MemberLimitSchema", () => {
  it("reads an active limit with its counts", () => {
    expect(
      MemberLimitSchema.parse({ action: "enforce", limit: 5, members: 4, pending_invitations: 1 }),
    ).toEqual({ action: "enforce", limit: 5, members: 4, pendingInvitations: 1 });
  });

  it("reads off, an unknown action, or a limit without a value as no limit", () => {
    expect(MemberLimitSchema.parse({ action: "off" })).toEqual(OFF);
    expect(MemberLimitSchema.parse({ action: "block", limit: 5 })).toEqual(OFF);
    expect(MemberLimitSchema.parse({ action: "enforce", limit: "5" })).toEqual(OFF);
  });

  it("treats missing counts of an active limit as zero", () => {
    expect(MemberLimitSchema.parse({ action: "observe", limit: 5 })).toEqual({
      action: "observe",
      limit: 5,
      members: 0,
      pendingInvitations: 0,
    });
  });
});

describe("isMemberLimitFull", () => {
  const enforced = (members: number, pendingInvitations: number): MemberLimit => ({
    action: "enforce",
    limit: 5,
    members,
    pendingInvitations,
  });

  it("is full when members and pending invitations fill an enforced limit", () => {
    expect(isMemberLimitFull(enforced(4, 0))).toBe(false);
    expect(isMemberLimitFull(enforced(4, 1))).toBe(true);
    expect(isMemberLimitFull(enforced(7, 0))).toBe(true);
  });

  it("is never full while the limit is only observed, off, or unknown", () => {
    expect(isMemberLimitFull({ ...enforced(7, 0), action: "observe" })).toBe(false);
    expect(isMemberLimitFull(OFF)).toBe(false);
    expect(isMemberLimitFull(undefined)).toBe(false);
  });
});

describe("isMemberLimitReachedError", () => {
  it("matches only the member limit refusal", () => {
    expect(isMemberLimitReachedError(new ApiError("full", 403, "Forbidden", { code: "member_limit_reached" }))).toBe(true);
    expect(isMemberLimitReachedError(new ApiError("locked", 403, "Forbidden", { code: "feature_locked" }))).toBe(false);
    expect(isMemberLimitReachedError(new ApiError("full", 409, "Conflict", { code: "seat_capacity_full" }))).toBe(false);
    expect(isMemberLimitReachedError(new Error("member_limit_reached"))).toBe(false);
  });
});

describe("upgrade invite", () => {
  const now = Date.UTC(2026, 10, 19, 9);

  it("round-trips per workspace and clears", () => {
    const storage = memoryStorage();
    saveUpgradeInvite("acme", { email: "sarah@acme.io", role: "member" }, storage, now);
    expect(readUpgradeInvite("acme", storage, now + 60_000)).toEqual({ email: "sarah@acme.io", role: "member" });
    expect(readUpgradeInvite("other", storage, now)).toBeNull();
    clearUpgradeInvite("acme", storage);
    expect(readUpgradeInvite("acme", storage, now)).toBeNull();
  });

  it("drops an abandoned or unreadable draft", () => {
    const storage = memoryStorage();
    saveUpgradeInvite("acme", { email: "sarah@acme.io", role: "admin" }, storage, now);
    expect(readUpgradeInvite("acme", storage, now + 3 * 60 * 60 * 1000)).toBeNull();
    expect(storage.data.size).toBe(0);

    storage.setItem("multica:upgrade_invite:acme", "{not json");
    expect(readUpgradeInvite("acme", storage, now)).toBeNull();
    storage.setItem("multica:upgrade_invite:acme", JSON.stringify({ email: "x@acme.io", role: "owner", savedAt: now }));
    expect(readUpgradeInvite("acme", storage, now)).toBeNull();
    expect(storage.data.size).toBe(0);
  });
});
