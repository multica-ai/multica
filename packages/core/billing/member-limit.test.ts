// @vitest-environment node
import { describe, expect, it } from "vitest";

import { ApiError } from "../api";
import { MemberLimitSchema } from "../api/schemas";
import type { MemberLimit, StorageAdapter } from "../types";
import {
  bindUpgradeInviteCheckout,
  clearUpgradeInvite,
  heldInvitesToOffer,
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

  it("keeps each held invitation under its own token and drops only the one asked", () => {
    const storage = memoryStorage();
    const first = saveUpgradeInvite("acme", { email: "sarah@acme.io", role: "member" }, storage, now);
    const second = saveUpgradeInvite("acme", { email: "omar@acme.io", role: "admin" }, storage, now + 1);
    expect(first).not.toBe(second);
    expect(readUpgradeInvite("acme", first, storage, now + 60_000)).toMatchObject({ email: "sarah@acme.io", role: "member", checkout: null });
    expect(readUpgradeInvite("other", first, storage, now + 60_000)).toBeNull();

    clearUpgradeInvite("acme", first, storage, now + 60_000);
    expect(readUpgradeInvite("acme", first, storage, now + 60_000)).toBeNull();
    expect(readUpgradeInvite("acme", second, storage, now + 60_000)).not.toBeNull();
    clearUpgradeInvite("acme", second, storage, now + 60_000);
    expect(storage.data.size).toBe(0);
  });

  it("binds a held invitation to its checkout and lists bound ones newest first", () => {
    const storage = memoryStorage();
    const waiting = saveUpgradeInvite("acme", { email: "a@acme.io", role: "member" }, storage, now);
    const older = saveUpgradeInvite("acme", { email: "b@acme.io", role: "member" }, storage, now + 1);
    const newer = saveUpgradeInvite("acme", { email: "c@acme.io", role: "member" }, storage, now + 2);
    bindUpgradeInviteCheckout("acme", older, { requestId: "req-1", baseVersion: 0 }, storage, now + 3);
    bindUpgradeInviteCheckout("acme", newer, { requestId: "req-2", baseVersion: 0 }, storage, now + 4);

    expect(listCheckoutHeldInvites("acme", storage, now + 5).map((h) => h.token)).toEqual([newer, older]);
    expect(readUpgradeInvite("acme", newer, storage, now + 5)?.checkout).toEqual({ requestId: "req-2", baseVersion: 0, boundAt: now + 4 });
    expect(readUpgradeInvite("acme", waiting, storage, now + 5)?.checkout).toBeNull();
  });

  // Stripe keeps a Checkout session payable for 24 hours; a held invitation
  // whose checkout started must outlive it, with time left to send it.
  it("keeps a held invitation for as long as its checkout can still be paid", () => {
    const storage = memoryStorage();
    const token = saveUpgradeInvite("acme", { email: "sarah@acme.io", role: "member" }, storage, now);
    bindUpgradeInviteCheckout("acme", token, { requestId: "req-1", baseVersion: 0 }, storage, now + 60_000);
    const hour = 60 * 60 * 1000;
    expect(readUpgradeInvite("acme", token, storage, now + 30 * hour)).not.toBeNull();
    expect(readUpgradeInvite("acme", token, storage, now + 49 * hour)).toBeNull();
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
      JSON.stringify({ invites: { t1: { email: "x@acme.io", role: "owner", savedAt: now, checkout: null } } }),
    );
    expect(readUpgradeInvite("acme", "t1", storage, now)).toBeNull();
    expect(storage.data.size).toBe(0);
  });
});

describe("heldInvitesToOffer", () => {
  const held = (token: string, requestId: string | null, { baseVersion = 0, savedAt = 1 } = {}): HeldInvite => ({
    token,
    email: `${token}@acme.io`,
    role: "member",
    savedAt,
    checkout: requestId ? { requestId, baseVersion, boundAt: 10 } : null,
  });
  const paid = { version: 1, paid: true };

  it("waits until the plan is paid and changed after the invitation's checkout started", () => {
    // An incomplete first payment bumps the version but still reads as Free.
    expect(heldInvitesToOffer([held("a", "req-a")], { version: 1, paid: false })).toEqual([]);
    expect(heldInvitesToOffer([held("a", "req-a", { baseVersion: 1 })], paid)).toEqual([]);
    expect(heldInvitesToOffer([held("a", null)], paid)).toEqual([]);
  });

  it("offers every paid-for held invitation, newest first, for the person to pick", () => {
    // Paying for A while a newer B waits in another tab must still offer A.
    const older = held("a", "req-a", { savedAt: 1 });
    const newer = held("b", "req-b", { savedAt: 2 });
    expect(heldInvitesToOffer([older, newer], paid)).toEqual([newer, older]);
  });
});
