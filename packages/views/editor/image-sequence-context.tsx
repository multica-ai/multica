"use client";

/**
 * ImageSequenceProvider — prev / next for the images of ONE surface (MUL-5752).
 *
 * A surface that can hold several images (an issue: description + every
 * comment; a chat session: every message) mounts this once with the ordered
 * sequence built by `collectImageSequence`. Clicking any image inside opens
 * the shared preview modal at that image's real position and lets the reader
 * page through the rest.
 *
 * Why one provider instead of per-image state: `<Attachment>` owns a private
 * `useAttachmentPreview()` modal, which is right for a lone image but cannot
 * know what comes next. The provider hosts a single modal above every image,
 * so navigation state has exactly one owner.
 *
 * Two behaviours worth stating up front, both from the product brief:
 *
 *   - The sequence is FROZEN when the modal opens. New comments and streaming
 *     agent output keep arriving while a preview is open; recomputing live
 *     would shift "3 / 7" under the reader mid-look.
 *   - Boundaries DISABLE, they don't wrap. First image: no previous. Last
 *     image: no next.
 *
 * Images outside the sequence (an in-flight upload in a composer, an image in
 * a surface with no provider) are not an error: `openAt` reports false and the
 * caller falls back to its own single-image preview.
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
  indexOfImageKey,
  type ImageSequenceItem,
} from "@multica/core/attachments/image-sequence";
import { useT } from "../i18n";
import {
  AttachmentPreviewModal,
  PreviewImagePrefetch,
  type PreviewSource,
} from "./attachment-preview-modal";

interface ImageSequenceApi {
  /**
   * Open the shared viewer at `key` — the attachment id, or the URL as written
   * in the body for references that don't resolve to a record.
   *
   * Returns false when no provider is mounted or the key is not part of this
   * surface's sequence, so callers can fall back to a single-image preview.
   */
  openAt: (key: string) => boolean;
}

const NO_SEQUENCE: ImageSequenceApi = { openAt: () => false };

const ImageSequenceContext = createContext<ImageSequenceApi>(NO_SEQUENCE);

/**
 * Returns the surrounding surface's image viewer, or a no-op handle when
 * there is no provider. Always safe to call — `openAt` reporting false is the
 * documented "not part of a sequence" answer, not a failure.
 */
export function useImageSequencePreview(): ImageSequenceApi {
  return use(ImageSequenceContext);
}

function toPreviewSource(item: ImageSequenceItem): PreviewSource {
  return item.attachment
    ? { kind: "full", attachment: item.attachment }
    : { kind: "url", url: item.url, filename: item.filename };
}

interface Session {
  /** Snapshot taken at open time — see the freeze note in the file header. */
  items: ImageSequenceItem[];
  index: number;
}

export function ImageSequenceProvider({
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
  // deleted attachment can't trap the reader on a broken frame, and so the
  // auto-skip below can't bounce between two dead images forever. The ref is
  // the synchronous truth (an <img> can report failure more than once before
  // React re-renders); the state copy exists to drive that re-render.
  const brokenRef = useRef<Set<string>>(new Set());
  const [broken, setBroken] = useState<ReadonlySet<string>>(brokenRef.current);
  // Direction of the last move, so a load failure keeps skipping the way the
  // reader was already going.
  const directionRef = useRef<1 | -1>(1);

  const api = useMemo<ImageSequenceApi>(
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
    <ImageSequenceContext.Provider value={api}>
      {children}
      {modal}
      {/* Warm the immediate neighbours while a preview is open, so paging
          swaps from cache instead of waiting a network round-trip. Keyed
          mounts: moving re-targets the prefetch to the new neighbours. */}
      {open && session && prevIndex >= 0 && (
        <PreviewImagePrefetch
          key={session.items[prevIndex]!.key}
          source={toPreviewSource(session.items[prevIndex]!)}
        />
      )}
      {open && session && nextIndex >= 0 && (
        <PreviewImagePrefetch
          key={session.items[nextIndex]!.key}
          source={toPreviewSource(session.items[nextIndex]!)}
        />
      )}
    </ImageSequenceContext.Provider>
  );
}
