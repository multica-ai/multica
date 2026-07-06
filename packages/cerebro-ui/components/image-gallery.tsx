"use client";

// ---------------------------------------------------------------------------
// ImageGallery — full-screen, paginated image lightbox (FIR-2710).
// ---------------------------------------------------------------------------
//
// Before this, clicking an image attachment under a comment opened the single
// image on its own full-page route in a new tab — to see the next picture you
// had to close it and click the next thumbnail. This gallery keeps you in
// place: a dark overlay above the thread, prev/next navigation through all the
// sibling images, a "3 / 5" counter, a thumbnail strip, and per-image zoom.
//
// Navigation is click/tap + keyboard (no swipe): the image surface is a
// ZoomableImage that already owns touch gestures for pinch-zoom and drag-pan,
// so a swipe would collide with panning. Large arrow buttons + the thumbnail
// strip cover mobile; ArrowLeft/ArrowRight/Escape cover desktop.
//
// Zoom (wheel on desktop, two-finger pinch on mobile, double-tap reset) comes
// for free from ZoomableImage, which resets to fit whenever `src` changes — so
// paging to a new image always starts fit-to-screen.

import { useCallback, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { ChevronLeft, ChevronRight, Download, ExternalLink, X } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { ZoomableImage } from "./zoomable-image";

export interface GalleryImage {
  /** Full-size image source. */
  src: string;
  alt: string;
  /** Optional direct download URL, surfaced as a per-image download button. */
  downloadHref?: string;
  /**
   * Optional link to the image's own standalone page (the in-app attachment
   * viewer). Surfaced as an "open on its own page" button so the user can
   * leave the gallery for the full-page view / a shareable URL.
   */
  pageHref?: string;
}

export interface ImageGalleryProps {
  images: GalleryImage[];
  /** Index of the image to show first (the thumbnail the user clicked). */
  startIndex?: number;
  open: boolean;
  onClose: () => void;
}

function clamp(i: number, count: number): number {
  if (count <= 0) return 0;
  return Math.min(Math.max(i, 0), count - 1);
}

export function ImageGallery({
  images,
  startIndex = 0,
  open,
  onClose,
}: ImageGalleryProps) {
  const count = images.length;
  const [index, setIndex] = useState(() => clamp(startIndex, count));

  // Re-sync to the clicked thumbnail every time the gallery (re)opens or the
  // caller points it at a new start image.
  useEffect(() => {
    if (open) setIndex(clamp(startIndex, count));
  }, [open, startIndex, count]);

  const go = useCallback(
    (delta: number) => setIndex((i) => clamp(i + delta, count)),
    [count],
  );

  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
      else if (e.key === "ArrowRight") go(1);
      else if (e.key === "ArrowLeft") go(-1);
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [open, onClose, go]);

  if (!open || count === 0 || typeof document === "undefined") return null;

  const safeIndex = clamp(index, count);
  const current = images[safeIndex];
  if (!current) return null;
  const multi = count > 1;
  const hasPrev = safeIndex > 0;
  const hasNext = safeIndex < count - 1;

  return createPortal(
    // Clicking the empty backdrop closes. Clicking the image (at fit) also
    // bubbles here and closes — the expected "tap to dismiss". When the image
    // is zoomed, ZoomableImage stops the click so panning never dismisses.
    <div
      className="fixed inset-0 z-50 flex flex-col bg-black/90"
      role="dialog"
      aria-modal="true"
      aria-label={current.alt || "Image preview"}
      onClick={onClose}
    >
      <div
        className="flex items-center gap-2 px-4 py-2 text-white"
        onClick={(e) => e.stopPropagation()}
      >
        {multi && (
          <span className="text-sm tabular-nums text-white/70">
            {safeIndex + 1} / {count}
          </span>
        )}
        <div className="ml-auto flex items-center gap-1">
          {current.pageHref && (
            <a
              href={current.pageHref}
              target="_blank"
              rel="noreferrer"
              className="rounded-md p-1.5 text-white/80 transition-colors hover:bg-white/10 hover:text-white"
              title="Open on its own page"
              aria-label="Open on its own page"
            >
              <ExternalLink className="size-5" />
            </a>
          )}
          {current.downloadHref && (
            <a
              href={current.downloadHref}
              target="_blank"
              rel="noreferrer"
              className="rounded-md p-1.5 text-white/80 transition-colors hover:bg-white/10 hover:text-white"
              title="Download"
              aria-label="Download"
            >
              <Download className="size-5" />
            </a>
          )}
          <button
            type="button"
            className="rounded-md p-1.5 text-white/80 transition-colors hover:bg-white/10 hover:text-white"
            title="Close"
            aria-label="Close"
            onClick={onClose}
          >
            <X className="size-5" />
          </button>
        </div>
      </div>

      <div className="relative min-h-0 flex-1">
        <ZoomableImage
          src={current.src}
          alt={current.alt}
          viewportClassName="h-full w-full"
        />

        {multi && (
          <>
            <NavButton side="left" disabled={!hasPrev} onClick={() => go(-1)} />
            <NavButton side="right" disabled={!hasNext} onClick={() => go(1)} />
          </>
        )}
      </div>

      {multi && (
        <div
          className="flex shrink-0 items-center justify-center gap-2 overflow-x-auto px-4 py-3"
          onClick={(e) => e.stopPropagation()}
        >
          {images.map((img, i) => (
            <button
              key={i}
              type="button"
              aria-label={`View image ${i + 1}`}
              aria-current={i === safeIndex}
              onClick={() => setIndex(i)}
              className={cn(
                "size-14 shrink-0 overflow-hidden rounded border-2 transition-colors",
                i === safeIndex
                  ? "border-white"
                  : "border-transparent opacity-60 hover:opacity-100",
              )}
            >
              <img
                src={img.src}
                alt=""
                draggable={false}
                className="size-full object-cover"
              />
            </button>
          ))}
        </div>
      )}
    </div>,
    document.body,
  );
}

function NavButton({
  side,
  disabled,
  onClick,
}: {
  side: "left" | "right";
  disabled: boolean;
  onClick: () => void;
}) {
  const Icon = side === "left" ? ChevronLeft : ChevronRight;
  return (
    <button
      type="button"
      disabled={disabled}
      aria-label={side === "left" ? "Previous image" : "Next image"}
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
      className={cn(
        "absolute top-1/2 -translate-y-1/2 rounded-full bg-black/40 p-2 text-white transition-colors hover:bg-black/60 disabled:pointer-events-none disabled:opacity-0",
        side === "left" ? "left-2 sm:left-4" : "right-2 sm:right-4",
      )}
    >
      <Icon className="size-6 sm:size-7" />
    </button>
  );
}
