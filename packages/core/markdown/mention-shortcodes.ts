/**
 * Convert legacy mention shortcodes [@ id="UUID" label="LABEL"] to the
 * standard markdown link format [@LABEL](mention://member/UUID).
 *
 * These shortcodes exist in older database records from a previous mention
 * serialization format. This function normalises them so downstream parsers
 * (Tiptap @tiptap/markdown on web/desktop, the mobile renderer in
 * apps/mobile/lib/markdown/) only need to handle one syntax.
 *
 * SYNCED COPY — KEEP IDENTICAL TO packages/ui/markdown/mentions.ts.
 * Mobile imports this copy because mobile is allowed to import pure functions
 * from @multica/core/. Web/desktop use the @multica/ui/markdown copy because
 * packages/ui/ cannot import from packages/core/.
 *
 * Pure regex transform — no IO, no global state. Idempotent: running it
 * twice on the same input produces the same output.
 */
export function preprocessMentionShortcodes(text: string): string {
  if (!text.includes("[@ ")) return text;
  return text.replace(/\[@\s+([^\]]*)\]/g, (match, attrString: string) => {
    const attrs: Record<string, string> = {};
    const re = /(\w+)="([^"]*)"/g;
    let m;
    while ((m = re.exec(attrString)) !== null) {
      if (m[1] && m[2] !== undefined) attrs[m[1]] = m[2];
    }
    const { id, label } = attrs;
    if (!id || !label) return match;
    return `[@${label}](mention://member/${id})`;
  });
}
