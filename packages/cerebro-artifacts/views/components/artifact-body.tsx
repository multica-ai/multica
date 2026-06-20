"use client";

import { ReadonlyContent } from "@multica/views/editor";

/**
 * Renders a read-only document/artifact body through the shared
 * `ReadonlyContent` renderer — the exact same pipeline used by notes, issue
 * descriptions and comments (and by the editable document view via Tiptap).
 *
 * Unifying on `ReadonlyContent` means a read-only document inherits, for free,
 * everything those surfaces already have:
 *   - click-to-fullscreen + pinch/wheel zoom on embedded images
 *     (ImageLightbox → ZoomableImage),
 *   - fullscreen Mermaid diagrams ("grafer") via the editor MermaidDiagram,
 *   - responsive tables (horizontal scroll inside the wrapper on desktop,
 *     wrap-to-fit on mobile),
 *   - mentions, code highlighting and KaTeX.
 *
 * Previously this used `@multica/views/common/markdown` with its own Mermaid
 * splitting, which gave documents none of the above — that is why embedded
 * images were not clickable and tables overflowed on documents only.
 */
export function ArtifactBody({
  body,
  className,
}: {
  body: string;
  className?: string;
}) {
  return <ReadonlyContent content={body} className={className} />;
}
