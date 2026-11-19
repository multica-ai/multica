// @vitest-environment node
import { describe, expect, it } from "vitest";

import { ApiError } from "../api";
import { MemberLimitSchema } from "../api/schemas";
import type { MemberLimit, StorageAdapter } from "../types";
import {
  bindUpgradeInviteCheckout,
  clearUpgradeInvite,
  heldInviteForConfirmedUpgrade,
  isMemberLimitFull,
  isMemberLimitReachedError,
  listCheckoutHeldInvites,
  memberLimitKeys,
  readUpgradeInvite,
  saveUpgradeInvite,
  type HeldInvite,
} from "./member-limit";
import { workspaceKeys } from "../workspace/queries";

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

describe("memberLimitKeys", () => {
  it("lives under the member list so a member change refetches it", () => {
    expect(memberLimitKeys.all("ws-1").slice(0, 3)).toEqual([...workspaceKeys.members("ws-1")]);
  });
});

describe("held upgrade invitations", () => {
  const now = Date.UTC(2026, 10, 19, 9);

  it("keeps each held invitation under its own token", () => {
    const storage = memoryStorage();
    const first = saveUpgradeInvite("acme", { email: "sarah@acme.io", role: "member" }, storage, now);
    const second = saveUpgradeInvite("acme", { email: "omar@acme.io", role: "admin" }, storage, now + 1);
    expect(first).not.toBe(second);
    expect(readUpgradeInvite("acme", first, storage, now + 60_000)).toMatchObject({ email: "sarah@acme.io", role: "member", checkout: null });
    expect(readUpgradeInvite("acme", second, storage, now + 60_000)).toMatchObject({ email: "omar@acme.io", role: "admin" });
    expect(readUpgradeInvite("other", first, storage, now)).toBeNull();

    clearUpgradeInvite("acme", first, storage, now + 60_000);
    expect(readUpgradeInvite("acme", first, storage, now + 60_000)).toBeNull();
    expect(readUpgradeInvite("acme", second, storage, now + 60_000)).not.toBeNull();
    clearUpgradeInvite("acme", undefined, storage);
    expect(storage.data.size).toBe(0);
  });

  it("lists only invitations whose checkout started, newest first", () => {
    const storage = memoryStorage();
    const waiting = saveUpgradeInvite("acme", { email: "a@acme.io", role: "member" }, storage, now);
    const older = saveUpgradeInvite("acme", { email: "b@acme.io", role: "member" }, storage, now + 1);
    const newer = saveUpgradeInvite("acme", { email: "c@acme.io", role: "member" }, storage, now + 2);
    bindUpgradeInviteCheckout("acme", older, { sessionId: "cs_1", baseVersion: 0 }, storage, now + 3);
    bindUpgradeInviteCheckout("acme", newer, { sessionId: "cs_2", baseVersion: 0 }, storage, now + 3);

    expect(listCheckoutHeldInvites("acme", storage, now + 4).map((h) => h.token)).toEqual([newer, older]);
    expect(readUpgradeInvite("acme", waiting, storage, now + 4)?.checkout).toBeNull();
  });

  it("drops abandoned or unreadable entries", () => {
    const storage = memoryStorage();
    const token = saveUpgradeInvite("acme", { email: "sarah@acme.io", role: "admin" }, storage, now);
    expect(readUpgradeInvite("acme", token, storage, now + 3 * 60 * 60 * 1000)).toBeNull();
    expect(storage.data.size).toBe(0);

    storage.setItem("multica:upgrade_invite:acme", "{not json");
    expect(listCheckoutHeldInvites("acme", storage, now)).toEqual([]);
    storage.setItem(
      "multica:upgrade_invite:acme",
      JSON.stringify({ t1: { email: "x@acme.io", role: "owner", savedAt: now, checkout: null } }),
    );
    expect(readUpgradeInvite("acme", "t1", storage, now)).toBeNull();
    expect(storage.data.size).toBe(0);
  });
});

describe("heldInviteForConfirmedUpgrade", () => {
  const held = (token: string, sessionId: string | null, baseVersion = 0): HeldInvite => ({
    token,
    email: `${token}@acme.io`,
    role: "member",
    savedAt: 1,
    checkout: sessionId ? { sessionId, baseVersion } : null,
  });
  const upgraded = { version: 1, checkoutAvailable: false };

  it("waits until the subscription changed after the invitation's own checkout", () => {
    expect(heldInviteForConfirmedUpgrade([held("a", "cs_a")], { version: 0, checkoutAvailable: false }, null)).toBeNull();
    expect(heldInviteForConfirmedUpgrade([held("a", "cs_a")], { version: 1, checkoutAvailable: true }, null)).toBeNull();
    expect(heldInviteForConfirmedUpgrade([held("a", "cs_a", 1)], { version: 1, checkoutAvailable: false }, null)).toBeNull();
    expect(heldInviteForConfirmedUpgrade([held("a", null)], upgraded, null)).toBeNull();
    expect(heldInviteForConfirmedUpgrade([held("a", "cs_a")], upgraded, null)?.token).toBe("a");
  });

  it("picks the invitation of the returned checkout session", () => {
    const both = [held("a", "cs_a"), held("b", "cs_b")];
    expect(heldInviteForConfirmedUpgrade(both, upgraded, "cs_b")?.token).toBe("b");
    expect(heldInviteForConfirmedUpgrade(both, upgraded, "cs_other")).toBeNull();
  });

  it("never guesses between several invitations without a returned session", () => {
    expect(heldInviteForConfirmedUpgrade([held("a", "cs_a"), held("b", "cs_b")], upgraded, null)).toBeNull();
  });
});
