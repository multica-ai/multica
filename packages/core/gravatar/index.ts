import { sha256 } from "@noble/hashes/sha256";
import { bytesToHex } from "@noble/hashes/utils";

export function getGravatarUrl(email: string | null | undefined, size = 80): string | null {
  if (!email) return null;
  const normalized = email.trim().toLowerCase();
  if (!normalized) return null;
  const hash = bytesToHex(sha256(new TextEncoder().encode(normalized)));
  return `https://www.gravatar.com/avatar/${hash}?s=${size}&d=identicon&r=g`;
}

export interface ResolveAvatarUrlParams {
  avatarUrl?: string | null;
  email?: string | null;
  gravatarEnabled?: boolean;
  size?: number;
  resolvePublicFileUrl?: (url: string | null | undefined) => string | null;
}

export function resolveAvatarUrl({
  avatarUrl,
  email,
  gravatarEnabled = false,
  size = 80,
  resolvePublicFileUrl,
}: ResolveAvatarUrlParams): string | null {
  const resolved = resolvePublicFileUrl ? resolvePublicFileUrl(avatarUrl) : avatarUrl;
  if (resolved) return resolved;
  if (gravatarEnabled && email) return getGravatarUrl(email, size);
  return null;
}
