import { stripChannelMediaMarkers } from "@multica/core/types";

/**
 * Flatten description Markdown into a one-line plain-text preview. Shared by
 * every surface that shows a description snippet next to an issue.
 *
 * Channel-media provenance is server-owned merge metadata, not authored
 * content, so it is stripped first: the image Markdown it annotates is removed
 * a line below, and without this the bare HTML comment survives every
 * remaining pass and becomes visible preview text.
 */
export function descriptionPreview(markdown: string): string {
  return stripChannelMediaMarkers(markdown)
    .replace(/!file\[[^\]]*\]\([^)]*\)/g, "")
    .replace(/!\[[^\]]*\]\([^)]*\)/g, "")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/[*_`~]+/g, "")
    .replace(/^[\s>#]+/gm, "")
    .replace(/\s+/g, " ")
    .trim();
}
