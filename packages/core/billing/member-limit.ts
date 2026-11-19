import { queryOptions } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import { defaultStorage } from "../platform/storage";
import type { MemberLimit, MemberRole, StorageAdapter } from "../types";
import { workspaceKeys } from "../workspace/queries";

/**
 * Member limit (MUL-9488).
 *
 * A Free workspace can have a limited number of members. Members and pending
 * invitations both take a slot; guests take none. The server checks every
 * invitation, invitation acceptance and share-link join; this module only
 * renders the limit ahead of that check and carries an invitation through an
 * upgrade.
 */
export const MEMBER_LIMIT_REACHED_CODE = "member_limit_reached";

export const memberLimitKeys = {
  /** Under `members`, so every member change also refetches the limit. */
  all: (wsId: string) => [...workspaceKeys.members(wsId), "limit"] as const,
};

/** Refetched on focus: an upgrade happens outside this tab, in Stripe. */
export function memberLimitOptions(wsId: string) {
  return queryOptions({
    queryKey: memberLimitKeys.all(wsId),
    queryFn: () => api.getMemberLimit(wsId),
    enabled: wsId.length > 0,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });
}

/**
 * Whether the next member would be refused. Only an enforced limit refuses;
 * an observed one admits everyone and stays invisible.
 */
export function isMemberLimitFull(limit: MemberLimit | null | undefined): boolean {
  if (!limit || limit.action !== "enforce" || limit.limit === null) return false;
  return (limit.members ?? 0) + (limit.pendingInvitations ?? 0) >= limit.limit;
}

/** The server refused a new member because the workspace is full. */
export function isMemberLimitReachedError(error: unknown): boolean {
  if (!(error instanceof ApiError) || error.status !== 403) return false;
  const body = error.body;
  return (
    typeof body === "object" &&
    body !== null &&
    (body as { code?: unknown }).code === MEMBER_LIMIT_REACHED_CODE
  );
}

/**
 * An invitation the member limit held back while its workspace upgrades.
 *
 * Members saves it under a token and opens Billing with that token. Billing
 * binds it to the checkout it creates for it, and sends it back to Members
 * only once that upgrade went through. A checkout started any other way never
 * picks it up, and several held invitations (one per tab) never get mixed.
 */
export interface UpgradeInvite {
  email: string;
  role: MemberRole;
}

export interface HeldInvite extends UpgradeInvite {
  token: string;
  savedAt: number;
  /** The checkout Billing created for this invitation, once it has. */
  checkout: HeldInviteCheckout | null;
}

export interface HeldInviteCheckout {
  sessionId: string;
  /** The subscription version when the checkout started. */
  baseVersion: number;
}

// Checkout plus the webhook that activates the plan take minutes; a held
// invitation older than this belongs to an upgrade that was abandoned.
const UPGRADE_INVITE_TTL_MS = 2 * 60 * 60 * 1000;

function upgradeInviteKey(slug: string): string {
  return `multica:upgrade_invite:${slug}`;
}

function newToken(): string {
  return globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function readAll(slug: string, storage: StorageAdapter, now: number): Record<string, HeldInvite> {
  const raw = storage.getItem(upgradeInviteKey(slug));
  if (!raw) return {};
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    storage.removeItem(upgradeInviteKey(slug));
    return {};
  }
  const out: Record<string, HeldInvite> = {};
  if (typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)) {
    for (const [token, value] of Object.entries(parsed as Record<string, unknown>)) {
      const held = readHeld(token, value, now);
      if (held) out[token] = held;
    }
  }
  writeAll(slug, out, storage);
  return out;
}

function readHeld(token: string, value: unknown, now: number): HeldInvite | null {
  if (typeof value !== "object" || value === null) return null;
  const v = value as Partial<HeldInvite>;
  if (
    typeof v.email !== "string" ||
    v.email.trim() === "" ||
    (v.role !== "admin" && v.role !== "member") ||
    typeof v.savedAt !== "number" ||
    v.savedAt > now ||
    now - v.savedAt > UPGRADE_INVITE_TTL_MS
  ) {
    return null;
  }
  const checkout =
    v.checkout &&
    typeof v.checkout.sessionId === "string" &&
    v.checkout.sessionId !== "" &&
    typeof v.checkout.baseVersion === "number"
      ? { sessionId: v.checkout.sessionId, baseVersion: v.checkout.baseVersion }
      : null;
  return { token, email: v.email, role: v.role, savedAt: v.savedAt, checkout };
}

function writeAll(slug: string, all: Record<string, HeldInvite>, storage: StorageAdapter): void {
  const entries = Object.values(all);
  if (entries.length === 0) {
    storage.removeItem(upgradeInviteKey(slug));
    return;
  }
  const value: Record<string, Omit<HeldInvite, "token">> = {};
  for (const { token, ...rest } of entries) value[token] = rest;
  storage.setItem(upgradeInviteKey(slug), JSON.stringify(value));
}

/** Holds an invitation and returns the token that names it. */
export function saveUpgradeInvite(
  slug: string,
  invite: UpgradeInvite,
  storage: StorageAdapter = defaultStorage,
  now: number = Date.now(),
): string {
  const all = readAll(slug, storage, now);
  const token = newToken();
  all[token] = { token, email: invite.email, role: invite.role, savedAt: now, checkout: null };
  writeAll(slug, all, storage);
  return token;
}

export function readUpgradeInvite(
  slug: string,
  token: string,
  storage: StorageAdapter = defaultStorage,
  now: number = Date.now(),
): HeldInvite | null {
  return readAll(slug, storage, now)[token] ?? null;
}

/** Records the checkout Billing created for a held invitation. */
export function bindUpgradeInviteCheckout(
  slug: string,
  token: string,
  checkout: HeldInviteCheckout,
  storage: StorageAdapter = defaultStorage,
  now: number = Date.now(),
): void {
  const all = readAll(slug, storage, now);
  const held = all[token];
  if (!held) return;
  all[token] = { ...held, checkout };
  writeAll(slug, all, storage);
}

/** Held invitations whose checkout has started, newest first. */
export function listCheckoutHeldInvites(
  slug: string,
  storage: StorageAdapter = defaultStorage,
  now: number = Date.now(),
): HeldInvite[] {
  return Object.values(readAll(slug, storage, now))
    .filter((held) => held.checkout !== null)
    .sort((a, b) => b.savedAt - a.savedAt);
}

/** Drops one held invitation, or every one in the workspace without a token. */
export function clearUpgradeInvite(
  slug: string,
  token?: string,
  storage: StorageAdapter = defaultStorage,
  now: number = Date.now(),
): void {
  if (token === undefined) {
    storage.removeItem(upgradeInviteKey(slug));
    return;
  }
  const all = readAll(slug, storage, now);
  delete all[token];
  writeAll(slug, all, storage);
}

/**
 * The held invitation a confirmed upgrade should send, if any. It qualifies
 * only when its own checkout started on this client and the subscription has
 * changed since (a version above the one seen when that checkout started)
 * with checkout no longer offered. A Stripe return to this tab names its
 * session and picks exactly that invitation. Without one (Desktop pays in the
 * system browser and the app only sees the plan change) a held invitation is
 * picked only when it is the only one waiting, never one of several.
 */
export function heldInviteForConfirmedUpgrade(
  held: HeldInvite[],
  subscription: { version: number; checkoutAvailable: boolean },
  returnSessionId: string | null,
): HeldInvite | null {
  if (subscription.checkoutAvailable) return null;
  const eligible = held.filter(
    (h) => h.checkout !== null && subscription.version > h.checkout.baseVersion,
  );
  if (returnSessionId) {
    return eligible.find((h) => h.checkout?.sessionId === returnSessionId) ?? null;
  }
  return eligible.length === 1 ? eligible[0]! : null;
}
