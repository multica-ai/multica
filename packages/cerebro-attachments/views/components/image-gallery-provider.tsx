"use client";

// CEREBRO-PATCH-ZONE: cerebro-only package, no upstream marker needed.
//
// FIR-2710 — one gallery per surface.
//
// Before this, a surface (a comment, the issue description, a chat message)
// showed images through three different viewers depending on HOW the image got
// there: inline-pasted images opened the legacy per-image ImageLightbox /
// AttachmentPreviewModal, while standalone attachment chips opened the new
// paginated ImageGallery. Same surface, different interface.
//
// This provider unifies them. Every image on a surface — inline body images and
// standalone attachment chips alike — registers with the nearest provider via
// `useGalleryImage`. Clicking any one opens a SINGLE ImageGallery paged through
// all of them, in visual (document) order, so "click through the images in this
// message" works regardless of how each image was added.
//
// Gated on `cerebro_image_gallery`. When the flag is off — or when a renderer
// is used on a surface that no provider wraps — `useGalleryImage` reports
// `enabled: false` and the caller keeps its existing per-image lightbox. That
// fallback is why the wiring in the upstream renderers is a no-op until both a
// provider is present AND the flag is on.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { ImageGallery, type GalleryImage } from "@multica/cerebro-ui";
import { useFlagValue } from "@multica/cerebro-feature-flags";

interface Registration {
  id: string;
  getImage: () => GalleryImage;
  getNode: () => HTMLElement | null;
}

interface GalleryContextValue {
  enabled: boolean;
  register: (reg: Registration) => () => void;
  open: (id: string) => void;
}

const GalleryContext = createContext<GalleryContextValue | null>(null);

// Document order so the gallery pages images the way the reader sees them, not
// the order effects happened to register them (body images and a lazily-loaded
// attachment list mount at different times).
function documentOrder(a: HTMLElement, b: HTMLElement): number {
  const pos = a.compareDocumentPosition(b);
  if (pos & Node.DOCUMENT_POSITION_FOLLOWING) return -1;
  if (pos & Node.DOCUMENT_POSITION_PRECEDING) return 1;
  return 0;
}

/**
 * Register one image with the nearest {@link ImageGalleryProvider}. Returns the
 * wiring an inline renderer needs:
 * - `enabled` — a provider is present and `cerebro_image_gallery` is on. While
 *   false the caller must keep its own lightbox.
 * - `ref` — attach to the image's DOM node so the gallery can order images by
 *   document position.
 * - `open` — open the surface gallery at this image.
 *
 * Pass `null` to keep the hook call unconditional (React rules of hooks) while
 * opting an element out of registration — e.g. a non-image chip, or an image
 * inside an editable editor that should not be paged.
 */
export function useGalleryImage(image: GalleryImage | null): {
  enabled: boolean;
  ref: (node: HTMLElement | null) => void;
  open: () => void;
} {
  const ctx = useContext(GalleryContext);
  const id = useId();
  const nodeRef = useRef<HTMLElement | null>(null);
  const imageRef = useRef(image);
  imageRef.current = image;

  const active = !!ctx?.enabled && image !== null;

  useEffect(() => {
    if (!active || !ctx) return;
    return ctx.register({
      id,
      getImage: () => imageRef.current as GalleryImage,
      getNode: () => nodeRef.current,
    });
  }, [active, ctx, id]);

  const ref = useCallback((node: HTMLElement | null) => {
    nodeRef.current = node;
  }, []);
  const open = useCallback(() => ctx?.open(id), [ctx, id]);

  return { enabled: active, ref, open };
}

export function ImageGalleryProvider({ children }: { children: ReactNode }) {
  const enabled = useFlagValue("cerebro_image_gallery");
  const registry = useRef<Map<string, Registration>>(new Map());
  const [session, setSession] = useState<{
    images: GalleryImage[];
    index: number;
  } | null>(null);

  const register = useCallback((reg: Registration) => {
    registry.current.set(reg.id, reg);
    return () => {
      registry.current.delete(reg.id);
    };
  }, []);

  const open = useCallback((id: string) => {
    const entries = [...registry.current.values()]
      .map((r) => ({ r, node: r.getNode() }))
      .filter((e): e is { r: Registration; node: HTMLElement } => !!e.node);
    entries.sort((a, b) => documentOrder(a.node, b.node));
    const index = entries.findIndex((e) => e.r.id === id);
    if (index < 0) return;
    setSession({ images: entries.map((e) => e.r.getImage()), index });
  }, []);

  const ctx = useMemo<GalleryContextValue>(
    () => ({ enabled, register, open }),
    [enabled, register, open],
  );

  return (
    <GalleryContext.Provider value={ctx}>
      {children}
      {session && (
        <ImageGallery
          images={session.images}
          startIndex={session.index}
          open
          onClose={() => setSession(null)}
        />
      )}
    </GalleryContext.Provider>
  );
}

/**
 * Provide an {@link ImageGalleryProvider} only when no ancestor already does.
 * Lets a self-contained surface (a standalone AttachmentList) always have a
 * gallery, while still folding into a parent surface's single gallery when one
 * wraps it (comment body + its attachment list share one gallery).
 */
export function EnsureGalleryProvider({ children }: { children: ReactNode }) {
  const existing = useContext(GalleryContext);
  if (existing) return <>{children}</>;
  return <ImageGalleryProvider>{children}</ImageGalleryProvider>;
}
