/**
 * Deterministic workspace color derivation.
 *
 * Mirrors the backend `server/internal/util/wscolor.go` contract from the
 * cross-workspace meta view tech spec (`docs/adrs/0001-cross-workspace-meta-view-techspec.md`,
 * section 1.6). Same algorithm + same palette guarantees that colors stay
 * stable when the backend starts shipping `workspace.color` in API responses
 * (MUL-4) — the response value will simply replace the locally-computed one
 * with no visual change.
 *
 * Algorithm: 32-bit FNV-1a hash over the UUID's 16 raw bytes, modulo the
 * palette length. Hashing the bytes (not the string) matches what Go does
 * with `uuid.UUID` ([16]byte), so frontend-derived and backend-derived
 * colors agree byte-for-byte.
 */

export const WORKSPACE_COLOR_PALETTE = [
  "#ef4444", // red
  "#f97316", // orange
  "#f59e0b", // amber
  "#eab308", // yellow
  "#84cc16", // lime
  "#22c55e", // green
  "#10b981", // emerald
  "#14b8a6", // teal
  "#06b6d4", // cyan
  "#3b82f6", // blue
  "#8b5cf6", // violet
  "#ec4899", // pink
] as const;

const FNV_OFFSET_32 = 0x811c9dc5;
const FNV_PRIME_32 = 0x01000193;

const UUID_HEX_REGEX = /^[0-9a-f]{32}$/;

function uuidToBytes(id: string): Uint8Array | null {
  const hex = id.replace(/-/g, "").toLowerCase();
  if (!UUID_HEX_REGEX.test(hex)) return null;
  const bytes = new Uint8Array(16);
  for (let i = 0; i < 16; i++) {
    bytes[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  }
  return bytes;
}

function fnv1a32(bytes: Uint8Array): number {
  let hash = FNV_OFFSET_32;
  for (let i = 0; i < bytes.length; i++) {
    hash ^= bytes[i]!;
    hash = Math.imul(hash, FNV_PRIME_32) >>> 0;
  }
  return hash >>> 0;
}

/**
 * Returns the palette color for a workspace UUID. Falls back to the first
 * palette entry for inputs that aren't valid UUIDs — this is graceful
 * degradation, not a contract: callers are expected to pass real UUIDs.
 */
export function workspaceColor(id: string): string {
  const bytes = uuidToBytes(id);
  if (!bytes) return WORKSPACE_COLOR_PALETTE[0];
  const hash = fnv1a32(bytes);
  return WORKSPACE_COLOR_PALETTE[hash % WORKSPACE_COLOR_PALETTE.length]!;
}
