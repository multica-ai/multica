import { copyToClipboard } from "@multica/ui/lib/clipboard";

export async function copyMarkdown(markdown: string): Promise<void> {
  await copyToClipboard(markdown);
}
