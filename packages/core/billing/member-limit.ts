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
 * binds it to the checkout it creates for it. Once the plan is paid, Billing
 * offers it for sending (see heldInvitesToOffer); Members consumes it only
 * once it is sent.
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
  /** The checkout request id Cloud returned. */
  requestId: string;
  /** The subscription version when the checkout started. */
  baseVersion: number;
  boundAt: number;
}

interface StoredHeldInvites {
  invites: Record<string, HeldInvite>;
}

// A held invitation whose checkout never started belongs to an abandoned
// upgrade after this long.
const UPGRADE_INVITE_TTL_MS = 2 * 60 * 60 * 1000;
// Once its checkout started it lives as long as Stripe can still take the
// payment (a Checkout session stays open 24 hours) plus a day to come back
// and send it.
const BOUND_UPGRADE_INVITE_TTL_MS = 48 * 60 * 60 * 1000;

function upgradeInviteKey(slug: string): string {
  return `multica:upgrade_invite:${slug}`;
}

function newToken(): string {
  return globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function readStored(slug: string, storage: StorageAdapter, now: number): StoredHeldInvites {
  const empty: StoredHeldInvites = { invites: {} };
  const raw = storage.getItem(upgradeInviteKey(slug));
  if (!raw) return empty;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    storage.removeItem(upgradeInviteKey(slug));
    return empty;
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    storage.removeItem(upgradeInviteKey(slug));
    return empty;
  }
  const value = parsed as { invites?: unknown };
  const out: StoredHeldInvites = { invites: {} };
  if (typeof value.invites === "object" && value.invites !== null && !Array.isArray(value.invites)) {
    for (const [token, entry] of Object.entries(value.invites as Record<string, unknown>)) {
      const held = readHeld(token, entry, now);
      if (held) out.invites[token] = held;
    }
  }
  writeStored(slug, out, storage);
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
    v.savedAt > now
  ) {
    return null;
  }
  const c = v.checkout;
  const checkout =
    c &&
    typeof c.requestId === "string" &&
    c.requestId !== "" &&
    typeof c.baseVersion === "number" &&
    typeof c.boundAt === "number"
      ? { requestId: c.requestId, baseVersion: c.baseVersion, boundAt: c.boundAt }
      : null;
  const expired = checkout
    ? now - checkout.boundAt > BOUND_UPGRADE_INVITE_TTL_MS
    : now - v.savedAt > UPGRADE_INVITE_TTL_MS;
  if (expired) return null;
  return { token, email: v.email, role: v.role, savedAt: v.savedAt, checkout };
}

function writeStored(slug: string, stored: StoredHeldInvites, storage: StorageAdapter): void {
  if (Object.keys(stored.invites).length === 0) {
    storage.removeItem(upgradeInviteKey(slug));
    return;
  }
  storage.setItem(upgradeInviteKey(slug), JSON.stringify(stored));
}

/** Holds an invitation and returns the token that names it. */
export function saveUpgradeInvite(
  slug: string,
  invite: UpgradeInvite,
  storage: StorageAdapter = defaultStorage,
  now: number = Date.now(),
): string {
  const stored = readStored(slug, storage, now);
  const token = newToken();
  stored.invites[token] = { token, email: invite.email, role: invite.role, savedAt: now, checkout: null };
  writeStored(slug, stored, storage);
  return token;
}

export function readUpgradeInvite(
  slug: string,
  token: string,
  storage: StorageAdapter = defaultStorage,
  now: number = Date.now(),
): HeldInvite | null {
  return readStored(slug, storage, now).invites[token] ?? null;
}

/** Records the checkout Billing created for a held invitation. */
export function bindUpgradeInviteCheckout(
  slug: string,
  token: string,
  checkout: Omit<HeldInviteCheckout, "boundAt">,
  storage: StorageAdapter = defaultStorage,
  now: number = Date.now(),
): void {
  const stored = readStored(slug, storage, now);
  const held = stored.invites[token];
  if (!held) return;
  stored.invites[token] = { ...held, checkout: { ...checkout, boundAt: now } };
  writeStored(slug, stored, storage);
}

/** Held invitations whose checkout has started, newest first. */
export function listCheckoutHeldInvites(
  slug: string,
  storage: StorageAdapter = defaultStorage,
  now: number = Date.now(),
): HeldInvite[] {
  return Object.values(readStored(slug, storage, now).invites)
    .filter((held) => held.checkout !== null)
    .sort((a, b) => b.savedAt - a.savedAt);
}

/** Drops one held invitation. */
export function clearUpgradeInvite(
  slug: string,
  token: string,
  storage: StorageAdapter = defaultStorage,
  now: number = Date.now(),
): void {
  const stored = readStored(slug, storage, now);
  delete stored.invites[token];
  writeStored(slug, stored, storage);
}

/** Drops every held invitation for email, once an invitation to it is out. */
export function clearUpgradeInvitesFor(
  slug: string,
  email: string,
  storage: StorageAdapter = defaultStorage,
  now: number = Date.now(),
): void {
  const stored = readStored(slug, storage, now);
  const normalized = email.trim().toLowerCase();
  for (const [token, held] of Object.entries(stored.invites)) {
    if (held.email.trim().toLowerCase() === normalized) delete stored.invites[token];
  }
  writeStored(slug, stored, storage);
}

/**
 * The held invitations to offer for sending now, newest first: those whose
 * checkout started on this client, once the plan is paid (an incomplete first
 * payment still reads as Free) and the subscription has changed since their
 * checkout started.
 *
 * They are only ever offered, never sent on their own, and all of them are
 * offered: nothing the client sees proves which checkout a plan change came
 * from (Desktop pays in the system browser and never sees the Stripe return,
 * another admin may have upgraded elsewhere, several checkouts can be open in
 * different tabs), so the person who held them picks.
 */
export function heldInvitesToOffer(
  held: HeldInvite[],
  subscription: { version: number; paid: boolean },
): HeldInvite[] {
  if (!subscription.paid) return [];
  return held
    .filter((h) => h.checkout !== null && subscription.version > h.checkout.baseVersion)
    .sort((a, b) => b.savedAt - a.savedAt);
}
