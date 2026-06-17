"use client";

import { useEffect } from "react";
import { cn } from "@multica/ui/lib/utils";
import { useZoomPan } from "../hooks/use-zoom-pan";

// ---------------------------------------------------------------------------
// ZoomableImage — drop-in replacement for an <img> in a preview surface.
// ---------------------------------------------------------------------------
//
// Wraps the image in a clipping viewport wired to `useZoomPan`, so previews
// that were previously fixed-size get pinch-to-zoom on mobile and wheel-zoom
// on desktop (plus drag-pan and double-tap reset). The image keeps the same
// `object-contain` fit at 1x, so the default view is identical to a bare
// <img> — zoom only kicks in when the user gestures.

export interface ZoomableImageProps {
  src: string;
  alt: string;
  /** Classes for the IMAGE element (e.g. rounded corners). Sizing is handled
   *  by the viewport, so callers should not pass width/height utilities. */
  className?: string;
  /** Classes for the clipping viewport (the box the image fits inside). */
  viewportClassName?: string;
}

export function ZoomableImage({
  src,
  alt,
  className,
  viewportClassName,
}: ZoomableImageProps) {
  const { viewportRef, transform, isZoomed, reset } = useZoomPan();

  // A new image is a fresh preview — always start fit-to-screen.
  useEffect(() => {
    reset();
  }, [src, reset]);

  return (
    <div
      ref={viewportRef}
      className={cn(
        "relative flex h-full w-full items-center justify-center overflow-hidden touch-none select-none",
        isZoomed ? "cursor-grab active:cursor-grabbing" : "cursor-zoom-in",
        viewportClassName,
      )}
      // While zoomed, swallow the click so panning/viewing never triggers a
      // click-to-close backdrop. At fit (1x) clicks bubble normally.
      onClick={isZoomed ? (e) => e.stopPropagation() : undefined}
    >
      <img
        src={src}
        alt={alt}
        draggable={false}
        className={cn("max-h-full max-w-full object-contain", className)}
        style={{
          transform,
          transformOrigin: "center center",
          willChange: "transform",
        }}
      />
    </div>
  );
}
