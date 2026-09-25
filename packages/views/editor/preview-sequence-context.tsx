"use client";

/**
 * PreviewSequenceProvider — prev / next for the attachments of ONE surface
 * (MUL-5752, MUL-7642).
 *
 * A surface that can hold several attachments (an issue: description + every
 * comment; a chat session: every message) mounts this once with the ordered
 * sequence built by `collectPreviewSequence`. Opening any previewable
 * attachment inside — an image, a PDF, a Markdown report, an HTML file —
 * opens the shared viewer at its real position so the reader can page through
 * the rest of the surface's files.
 *
 * Why one provider instead of per-attachment state: `<Attachment>` owns a
 * private `useAttachmentPreview()` modal, which is right for a lone file but
 * cannot know what comes next. The provider hosts a single modal above every
 * attachment, so navigation state has exactly one owner.
 *
 * Two behaviours worth stating up front, both from the product brief:
 *
 *   - The sequence is FROZEN when the modal opens. New comments and streaming
 *     agent output keep arriving while a preview is open; recomputing live
 *     would shift "3 / 7" under the reader mid-look.
 *   - Boundaries DISABLE, they don't wrap. First file: no previous. Last
 *     file: no next.
 *
 * Attachments outside the sequence (an in-flight upload in a composer, a file
 * in a surface with no provider) are not an error: `openAt` reports false and
 * the caller falls back to its own single-file preview.
 */

import {
  createContext,
  use,
  useCallback,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { toast } from "sonner";
import {
  collectAttachmentSequence,
  indexOfImageKey,
  type ImageSequenceBlock,
  type ImageSequenceItem,
} from "@multica/core/attachments/image-sequence";
import { useT } from "../i18n";
import {
  AttachmentPreviewModal,
  PreviewImagePrefetch,
  type PreviewSource,
} from "./attachment-preview-modal";
import { canOpenPreview, getPreviewKind } from "./utils/preview";

/**
 * The sequence a web / desktop surface pages through: every attachment the
 * viewer can open, in render order.
 */
export function collectPreviewSequence(
  blocks: ReadonlyArray<ImageSequenceBlock | null | undefined>,
): ImageSequenceItem[] {
  return collectAttachmentSequence(blocks, ({ contentType, filename, hasRecord }) =>
    canOpenPreview(getPreviewKind(contentType, filename), hasRecord),
  );
}

interface PreviewSequenceApi {
  /**
   * Open the shared viewer at `key` — the attachment id, or the URL as written
   * in the body for references that don't resolve to a record.
   *
   * Returns false when no provider is mounted or the key is not part of this
   * surface's sequence, so callers can fall back to a single-file preview.
   */
  openAt: (key: string) => boolean;
}

const NO_SEQUENCE: PreviewSequenceApi = { openAt: () => false };

const PreviewSequenceContext = createContext<PreviewSequenceApi>(NO_SEQUENCE);

/**
 * Returns the surrounding surface's viewer, or a no-op handle when there is
 * no provider. Always safe to call — `openAt` reporting false is the
 * documented "not part of a sequence" answer, not a failure.
 */
export function usePreviewSequence(): PreviewSequenceApi {
  return use(PreviewSequenceContext);
}

function toPreviewSource(item: ImageSequenceItem): PreviewSource {
  if (item.attachment) return { kind: "full", attachment: item.attachment };
  return {
    kind: "url",
    url: item.url,
    filename: item.filename,
    // A body image's `filename` is its markdown caption — prose with no
    // extension to read — so say what it is instead of letting the modal
    // re-derive it (MUL-7518). A file card's name is a real filename.
    forceKind: item.imageByConstruction ? "image" : undefined,
  };
}

function isImageItem(item: ImageSequenceItem): boolean {
  if (item.imageByConstruction) return true;
  const contentType = item.attachment?.content_type ?? "";
  return getPreviewKind(contentType, item.filename || item.url) === "image";
}

interface Session {
  /** Snapshot taken at open time — see the freeze note in the file header. */
  items: ImageSequenceItem[];
  index: number;
}

export function PreviewSequenceProvider({
  items,
  children,
}: {
  items: ReadonlyArray<ImageSequenceItem>;
  children: ReactNode;
}) {
  const { t } = useT("editor");
  // Read at click time only, so a streaming surface doesn't re-create the
  // context value (and re-render every image) on every incoming message.
  const itemsRef = useRef(items);
  itemsRef.current = items;

  const [session, setSession] = useState<Session | null>(null);
  const [open, setOpen] = useState(false);
  // Images that failed to load this session. Kept out of navigation so a
  // deleted image can't trap the reader on a broken frame, and so the
  // auto-skip below can't bounce between two dead images forever. The ref is
  // the synchronous truth (an <img> can report failure more than once before
  // React re-renders); the state copy exists to drive that re-render.
  const brokenRef = useRef<Set<string>>(new Set());
  const [broken, setBroken] = useState<ReadonlySet<string>>(brokenRef.current);
  // Direction of the last move, so a load failure keeps skipping the way the
  // reader was already going.
  const directionRef = useRef<1 | -1>(1);

  const api = useMemo<PreviewSequenceApi>(
    () => ({
      openAt: (key: string) => {
        const snapshot = [...itemsRef.current];
        const index = indexOfImageKey(snapshot, key);
        if (index < 0) return false;
        directionRef.current = 1;
        brokenRef.current = new Set();
        setBroken(brokenRef.current);
        setSession({ items: snapshot, index });
        setOpen(true);
        return true;
      },
    }),
    [],
  );

  const step = useCallback(
    (current: Session, from: number, delta: 1 | -1, skip: ReadonlySet<string>) => {
      for (let i = from + delta; i >= 0 && i < current.items.length; i += delta) {
        if (!skip.has(current.items[i]!.key)) return i;
      }
      return -1;
    },
    [],
  );

  const go = useCallback(
    (delta: 1 | -1) => {
      if (!session) return;
      const next = step(session, session.index, delta, brokenRef.current);
      if (next < 0) return;
      directionRef.current = delta;
      setSession({ ...session, index: next });
    },
    [session, step],
  );

  // A frame that fails to decode is skipped rather than left on screen: the
  // attachment was deleted, or its signed URL outlived the session. Advance
  // the way the reader was heading, then the other way, and only give up (and
  // leave the broken frame visible) when nothing loadable is left.
  const handleImageError = useCallback(() => {
    if (!session) return;
    const failed = session.items[session.index];
    if (!failed || brokenRef.current.has(failed.key)) return;

    brokenRef.current = new Set(brokenRef.current).add(failed.key);
    setBroken(brokenRef.current);
    toast.error(t(($) => $.image.unavailable));

    const forward = directionRef.current;
    const next = step(session, session.index, forward, brokenRef.current);
    const target =
      next >= 0
        ? next
        : step(session, session.index, forward === 1 ? -1 : 1, brokenRef.current);
    if (target >= 0) setSession({ ...session, index: target });
  }, [session, step, t]);

  const current = session ? session.items[session.index] : undefined;
  const prevIndex = session ? step(session, session.index, -1, broken) : -1;
  const nextIndex = session ? step(session, session.index, 1, broken) : -1;

  const modal =
    session && current ? (
      <AttachmentPreviewModal
        source={toPreviewSource(current)}
        open={open}
        onClose={() => setOpen(false)}
        onExitComplete={() => setSession(null)}
        onImageError={handleImageError}
        sequence={
          session.items.length > 1
            ? {
                index: session.index,
                total: session.items.length,
                onPrev: prevIndex >= 0 ? () => go(-1) : undefined,
                onNext: nextIndex >= 0 ? () => go(1) : undefined,
              }
            : undefined
        }
      />
    ) : null;

  return (
    <PreviewSequenceContext.Provider value={api}>
      {children}
      {modal}
      {/* Warm the immediate image neighbours while a preview is open, so
          paging swaps from cache instead of waiting a network round-trip.
          Other kinds load on arrival — a PDF or a video is not worth
          fetching speculatively. Keyed mounts: moving re-targets the
          prefetch to the new neighbours. */}
      {open && session && prevIndex >= 0 && isImageItem(session.items[prevIndex]!) && (
        <PreviewImagePrefetch
          key={session.items[prevIndex]!.key}
          source={toPreviewSource(session.items[prevIndex]!)}
        />
      )}
      {open && session && nextIndex >= 0 && isImageItem(session.items[nextIndex]!) && (
        <PreviewImagePrefetch
          key={session.items[nextIndex]!.key}
          source={toPreviewSource(session.items[nextIndex]!)}
        />
      )}
    </PreviewSequenceContext.Provider>
  );
}
