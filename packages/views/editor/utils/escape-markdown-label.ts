/**
 * Escape characters that break Markdown link/image label syntax: [ ] \ ( )
 * Used by image and file-card renderMarkdown to prevent raw filenames from
 * corrupting the `![alt](url)` / `!file[name](url)` output.
 */
export function escapeMarkdownLabel(text: string): string {
  return text.replace(/[[\]\\()]/g, (ch) => `\\${ch}`);
}
