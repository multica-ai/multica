/**
 * DJB2 string hash — 32-bit unsigned, rendered as base36.
 *
 * Extracted from `rich-content/mounted-block-registry.ts` (same cheap,
 * synchronous hash used by the Mermaid layout cache). Used by the HTML
 * attachment preview to derive a `contentKey` fingerprint of the rendered
 * text so that scroll restoration can detect when the attachment's content
 * changed (re-upload to the same id) and refuse to restore a stale offset.
 */
export function hashString(source: string): string {
  let hash = 5381;
  for (let i = 0; i < source.length; i++) {
    hash = ((hash << 5) + hash) ^ source.charCodeAt(i);
  }
  return (hash >>> 0).toString(36);
}
