import { sha256 } from "@noble/hashes/sha256";
import { bytesToHex } from "@noble/hashes/utils";

export function getGravatarUrl(email: string | null | undefined, size = 80): string | null {
  if (!email) return null;
  const normalized = email.trim().toLowerCase();
  if (!normalized) return null;
  const hash = bytesToHex(sha256(new TextEncoder().encode(normalized)));
  return `https://www.gravatar.com/avatar/${hash}?s=${size}&d=identicon&r=g`;
}
