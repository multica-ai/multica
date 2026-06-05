import { sha256 } from "@noble/hashes/sha256";
import { bytesToHex } from "@noble/hashes/utils";

export function getGravatarUrl(email: string, size = 80): string {
  const normalized = email.trim().toLowerCase();
  const hash = bytesToHex(sha256(new TextEncoder().encode(normalized)));
  return `https://www.gravatar.com/avatar/${hash}?s=${size}&d=identicon&r=g`;
}
