/**
 * Screen-scoped image sequence for the lightbox (MUL-5752).
 *
 * A screen that can show several images (an issue: description + every
 * comment; a chat session: every message) mounts this once with its blocks in
 * render order. Every `MarkdownImage` below it then opens the lightbox
 * positioned inside that sequence instead of on its own, so a swipe walks to
 * the next image and the viewer can say "3 / 7".
 *
 * Mirrors the web/desktop `ImageSequenceProvider`
 * (packages/views/editor/image-sequence-context.tsx) — same product contract,
 * different presentation. The ordering itself is NOT re-derived here: both
 * clients call `collectImageSequence` from @multica/core so an issue's images
 * are counted the same way on every client (mobile parity rule: mirror the
 * shared logic, own the UI).
 *
 * The URIs are resolved the same way `MarkdownImage` resolves the one it
 * renders — same attachment match, same `resolveAttachmentUrl` pass — so the
 * URI a tap reports is the one the sequence holds.
 */
import { createContext, use, useMemo, type ReactNode } from "react";
import {
  collectImageSequence,
  type ImageSequenceBlock,
} from "@multica/core/attachments/image-sequence";
import { resolveAttachmentUrl } from "@/lib/attachment-url";

const ImageSequenceContext = createContext<readonly string[]>([]);

/**
 * Resolved image URIs for the surrounding screen, in render order. Empty when
 * no provider is mounted — the lightbox then shows the tapped image alone.
 */
export function useImageSequence(): readonly string[] {
  return use(ImageSequenceContext);
}

export function ImageSequenceProvider({
  blocks,
  children,
}: {
  blocks: ReadonlyArray<ImageSequenceBlock | null | undefined>;
  children: ReactNode;
}) {
  const uris = useMemo(
    () =>
      collectImageSequence(blocks).map(
        (item) => resolveAttachmentUrl(item.url) ?? item.url,
      ),
    [blocks],
  );
  return (
    <ImageSequenceContext.Provider value={uris}>
      {children}
    </ImageSequenceContext.Provider>
  );
}
