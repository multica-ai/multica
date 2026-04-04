/**
 * Copy markdown content to the clipboard.
 */
export async function copyMarkdown(markdown: string): Promise<void> {
  await navigator.clipboard.writeText(markdown);
}
