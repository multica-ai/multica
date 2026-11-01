/**
 * Whether a value from the knowledge service is an http(s) link. Sources and
 * document URLs are user-supplied, so only these become anchors; anything
 * else, including a `javascript:` URL, stays plain text.
 */
export function isWebLink(value: string): boolean {
  return /^https?:\/\//i.test(value);
}
