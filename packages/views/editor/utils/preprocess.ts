import {
  preprocessLinks,
  preprocessMentionShortcodes,
  preprocessFileCards,
  preprocessIssueIdentifiers,
} from "@multica/ui/markdown";
import { configStore } from "@multica/core/config";

/**
 * Preprocess a markdown string before loading into Tiptap via contentType: 'markdown'.
 *
 * This is the ONLY transform applied before @tiptap/markdown parses the content.
 * It does NOT convert to HTML — that was the old markdownToHtml.ts pipeline which
 * was deleted in the April 2026 refactor.
 *
 * String→string transforms on raw Markdown:
 * 1. Legacy mention shortcodes [@ id="..." label="..."] → [@Label](mention://member/id)
 *    (old serialization format in database, migrated on read)
 * 2. (readonly only) Bare issue identifiers MUL-123 → [MUL-123](mention://issue/MUL-123)
 * 3. Raw URLs → markdown links via linkify-it (so they render as clickable Link nodes)
 * 4. File card syntax (new !file[name](url) + legacy [name](cdnUrl)) → HTML div for
 *    fileCard node parsing
 *
 * Shared by the Tiptap editor and the read-only react-markdown renderer so both
 * linkify identically. `autolinkIssueIdentifiers` is the one deliberate
 * asymmetry: it is OPT-IN and MUST stay off for the editable Tiptap path, since
 * rewriting a bare identifier there would create a mention node whose id is the
 * identifier string (not a real UUID) and corrupt the saved markdown. Only the
 * readonly renderer (which resolves the identifier to a UUID at render time)
 * passes it.
 */
export function preprocessMarkdown(
  markdown: string,
  opts?: { autolinkIssueIdentifiers?: boolean },
): string {
  if (!markdown) return "";
  const cdnDomain = configStore.getState().cdnDomain;
  const step1 = preprocessMentionShortcodes(markdown);
  const step2 = opts?.autolinkIssueIdentifiers
    ? preprocessIssueIdentifiers(step1)
    : step1;
  const step3 = preprocessLinks(step2);
  const step4 = preprocessFileCards(step3, cdnDomain);
  return step4;
}
