/**
 * Strip mention markdown syntax to plain text.
 *
 * Handles:
 * - Simple mentions: `[@Name](mention://agent/id)` → `@Name`
 * - Escaped brackets in names: `[@David\[TF\]](mention://agent/id)` → `@David[TF]`
 * - Issue mentions (no @): `[MUL-123](mention://issue/id)` → `MUL-123`
 * - Does NOT touch regular markdown links: `[docs](https://...)` stays unchanged
 * - Does NOT touch backslash-escaped mentions: `\[@Name](mention://...)` stays unchanged
 *
 * The regex mirrors the tokenizer in mention-extension.ts.
 */
export function stripMentionMarkdown(text: string): string {
  return text.replace(
    /(?<![\\])\[(@?)((?:\\.|[^\]])+)\]\(mention:\/\/\w+\/[^)]+\)/g,
    (_, prefix: string, rawLabel: string) => {
      const label = rawLabel.replace(/\\\[/g, "[").replace(/\\\]/g, "]");
      return `${prefix}${label}`;
    },
  );
}
