/**
 * FNV-1a 32-bit hash used for deterministic percent-rollout bucketing.
 *
 * The same (key, identifier) pair MUST always produce the same bucket;
 * otherwise users would flip in and out of experiments across requests. The
 * algorithm matches the Go-side server/pkg/featureflag/hash.go byte-for-byte
 * so a flag evaluated on the frontend and on the backend lands in the same
 * bucket for the same user. Cross-language equality is exercised by golden
 * tests on both sides; see hash.test.ts and hash_test.go.
 *
 * The hash operates on the UTF-8 encoding of each input. Go's `[]byte(s)`
 * conversion is also UTF-8, so the two implementations agree even when
 * flag keys or identifiers contain non-ASCII characters (Chinese flag
 * names, user IDs that include accented characters, emoji, ...). Using
 * `charCodeAt` directly would have hashed UTF-16 code units instead and
 * silently diverged from Go for any non-ASCII input.
 *
 * FNV-1a is used because it is cheap, dependency-free, and well-distributed
 * enough for sub-100 bucketing. It is NOT cryptographic; do not use it for
 * anything beyond bucketing.
 */

// One shared TextEncoder per module. TextEncoder is part of the WHATWG
// Encoding spec and ships in every evergreen browser, in Node 11+, and in
// React Native (Hermes) >= 0.74. We deliberately do not lazy-init it so
// failures show up at import time, not the first time a flag is read.
const utf8 = new TextEncoder();

function fnv1a(parts: ReadonlyArray<string>): number {
  // 32-bit FNV-1a: offset basis 0x811c9dc5, prime 0x01000193.
  let hash = 0x811c9dc5;
  for (let p = 0; p < parts.length; p++) {
    if (p > 0) {
      // Zero-byte separator BETWEEN parts (not after the last one). This
      // matches what the Go side writes via h.Write([]byte{0}) between
      // key and identifier and is what prevents ("ab", "c") and
      // ("a", "bc") from colliding. A trailing separator would diverge
      // from Go and silently break cross-tier bucket parity.
      hash ^= 0;
      hash = Math.imul(hash, 0x01000193);
    }
    // Encode the part as UTF-8 to match Go's `[]byte(string)`. Using
    // charCodeAt would hash UTF-16 code units instead and diverge from
    // Go for any non-ASCII input (Chinese keys, accented user IDs,
    // emoji, ...). See the package doc above.
    const bytes = utf8.encode(parts[p]!);
    for (let i = 0; i < bytes.length; i++) {
      hash ^= bytes[i]!;
      // Multiply by FNV prime mod 2^32. Math.imul keeps the result in a
      // 32-bit integer without slipping into float territory.
      hash = Math.imul(hash, 0x01000193);
    }
  }
  // Force unsigned 32-bit before the modulo to match Go's uint32 arithmetic.
  return hash >>> 0;
}

/**
 * bucketFor returns a deterministic bucket in [0, 100) for the supplied
 * (key, identifier) pair. Identical to the Go bucketFor in
 * server/pkg/featureflag/hash.go.
 */
export function bucketFor(key: string, identifier: string): number {
  return fnv1a([key, identifier]) % 100;
}

/**
 * inPercent reports whether (key, identifier) falls within the first
 * `percent` buckets. Values outside [0, 100] are clamped: <=0 disables for
 * everyone, >=100 enables for everyone.
 */
export function inPercent(key: string, identifier: string, percent: number): boolean {
  if (percent <= 0) return false;
  if (percent >= 100) return true;
  return bucketFor(key, identifier) < percent;
}
