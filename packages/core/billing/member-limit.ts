import { queryOptions } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import { defaultStorage } from "../platform/storage";
import type { MemberLimit, MemberRole, StorageAdapter } from "../types";

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
  all: (wsId: string) => ["member-limit", wsId] as const,
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
 * Members settings saves it before leaving for checkout and sends it once
 * Billing has confirmed the new plan.
 */
export interface UpgradeInvite {
  email: string;
  role: MemberRole;
}

// Checkout plus the webhook that activates the plan take minutes; a draft
// older than this belongs to an upgrade that was abandoned.
const UPGRADE_INVITE_TTL_MS = 2 * 60 * 60 * 1000;

function upgradeInviteKey(slug: string): string {
  return `multica:upgrade_invite:${slug}`;
}

export function saveUpgradeInvite(
  slug: string,
  invite: UpgradeInvite,
  storage: StorageAdapter = defaultStorage,
  now: number = Date.now(),
): void {
  storage.setItem(
    upgradeInviteKey(slug),
    JSON.stringify({ email: invite.email, role: invite.role, savedAt: now }),
  );
}

export function readUpgradeInvite(
  slug: string,
  storage: StorageAdapter = defaultStorage,
  now: number = Date.now(),
): UpgradeInvite | null {
  const raw = storage.getItem(upgradeInviteKey(slug));
  if (!raw) return null;
  try {
    const value = JSON.parse(raw) as Partial<UpgradeInvite & { savedAt: number }>;
    const role = value.role;
    if (
      typeof value.email !== "string" ||
      value.email.trim() === "" ||
      (role !== "admin" && role !== "member") ||
      typeof value.savedAt !== "number" ||
      now - value.savedAt > UPGRADE_INVITE_TTL_MS ||
      value.savedAt > now
    ) {
      storage.removeItem(upgradeInviteKey(slug));
      return null;
    }
    return { email: value.email, role };
  } catch {
    storage.removeItem(upgradeInviteKey(slug));
    return null;
  }
}

export function clearUpgradeInvite(
  slug: string,
  storage: StorageAdapter = defaultStorage,
): void {
  storage.removeItem(upgradeInviteKey(slug));
}
