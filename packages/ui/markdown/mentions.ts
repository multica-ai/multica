/**
 * Convert legacy mention shortcodes [@ id="UUID" label="LABEL"] to the
 * standard markdown link format [@LABEL](mention://member/UUID).
 *
 * These shortcodes exist in older database records from a previous mention
 * serialization format. This function normalises them so downstream parsers
 * (Tiptap @tiptap/markdown, react-markdown) only need to handle one syntax.
 *
 * SYNCED COPY — KEEP IDENTICAL TO packages/core/markdown/mention-shortcodes.ts.
 * Mobile imports the core copy because packages/ui/ cannot be imported from
 * mobile (Sharing Principles in apps/mobile/CLAUDE.md), and packages/ui/
 * cannot import from packages/core/ (Package Boundary Rules in root
 * CLAUDE.md). If you change the regex / behavior here, change core's copy
 * too — otherwise web and mobile will render legacy mentions differently
 * and the "Counts must agree" parity rule breaks.
 */
export function preprocessMentionShortcodes(text: string): string {
  if (!text.includes("[@ ")) return text;
  return text.replace(
    /\[@\s+([^\]]*)\]/g,
    (match, attrString: string) => {
      const attrs: Record<string, string> = {};
      const re = /(\w+)="([^"]*)"/g;
      let m;
      while ((m = re.exec(attrString)) !== null) {
        if (m[1] && m[2] !== undefined) attrs[m[1]] = m[2];
      }
      const { id, label } = attrs;
      if (!id || !label) return match;
      return `[@${label}](mention://member/${id})`;
    },
  );
}
