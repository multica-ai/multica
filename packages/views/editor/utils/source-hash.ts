/**
 * DJB2 hash of a rich block's source — small, fast, and sufficient for the
 * sessionStorage keys that remember a block's rendered size. The source text
 * itself is too unwieldy as a key (length, special chars), and a
 * crypto-strength hash would have to be async.
 */
export function hashSource(source: string): string {
  let hash = 5381;
  for (let i = 0; i < source.length; i++) {
    hash = ((hash << 5) + hash) ^ source.charCodeAt(i);
  }
  return (hash >>> 0).toString(36);
}
