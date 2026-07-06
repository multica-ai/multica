import React, { useRef, useEffect, useState, useCallback, useImperativeHandle, forwardRef } from "react";
import type { ReviewAsset } from "@multica/core/types";

export interface MediaReviewPlayerProps {
  asset: ReviewAsset;
  onTimeUpdate?: (currentTime: number) => void;
  comments?: any[];
}

export interface MediaReviewPlayerRef {
  seek: (time: number) => void;
  pause: () => void;
  getCanvasShapes: () => any;
  clearCanvasShapes: () => void;
}

export const MediaReviewPlayer = forwardRef<MediaReviewPlayerRef, MediaReviewPlayerProps>(
  ({ asset, onTimeUpdate, comments }, ref) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const mediaRef = useRef<HTMLVideoElement | HTMLImageElement>(null);
  const canvasRef = useRef<any>(null);

  useEffect(() => {
    import("@multica/canvas-drawing-editor").catch(() => {});
  }, []);

  const [layout, setLayout] = useState({ x: 0, y: 0, width: 0, height: 0 });

  const calculateTrueLayout = useCallback(() => {
    if (!containerRef.current || !mediaRef.current) return;

    const container = containerRef.current.getBoundingClientRect();
    let mediaWidth = 0;
    let mediaHeight = 0;

    if (asset.asset_type === "video") {
      const video = mediaRef.current as HTMLVideoElement;
      mediaWidth = video.videoWidth;
      mediaHeight = video.videoHeight;
    } else {
      const img = mediaRef.current as HTMLImageElement;
      mediaWidth = img.naturalWidth;
      mediaHeight = img.naturalHeight;
    }

    if (!mediaWidth || !mediaHeight || !container.width || !container.height) return;

    const containerAspect = container.width / container.height;
    const mediaAspect = mediaWidth / mediaHeight;

    let renderWidth: number, renderHeight: number, offsetX: number, offsetY: number;

    if (containerAspect > mediaAspect) {
      // Pillarboxed: bars on sides
      renderHeight = container.height;
      renderWidth = renderHeight * mediaAspect;
      offsetX = (container.width - renderWidth) / 2;
      offsetY = 0;
    } else {
      // Letterboxed: bars on top/bottom
      renderWidth = container.width;
      renderHeight = renderWidth / mediaAspect;
      offsetX = 0;
      offsetY = (container.height - renderHeight) / 2;
    }

    setLayout({ x: offsetX, y: offsetY, width: renderWidth, height: renderHeight });
  }, [asset.asset_type]);

  // ResizeObserver — covers dialog-open animation and window resize
  useEffect(() => {
    if (!containerRef.current) return;
    const observer = new ResizeObserver(() => requestAnimationFrame(calculateTrueLayout));
    observer.observe(containerRef.current);
    return () => observer.disconnect();
  }, [calculateTrueLayout]);

  // Retry after mount: handles cached images whose naturalWidth is already set
  useEffect(() => {
    requestAnimationFrame(calculateTrueLayout);
  }, [calculateTrueLayout]);

  // Reset when switching assets
  useEffect(() => {
    setLayout({ x: 0, y: 0, width: 0, height: 0 });
  }, [asset.id]);

  // Sync canvas editor size whenever the overlay dimensions change.
  // The editor is only rendered when layout.width > 0, so canvasRef is always
  // mounted with the correct host size — resize() just keeps it in sync on change.
  useEffect(() => {
    if (layout.width > 0) {
      requestAnimationFrame(() => (canvasRef.current as any)?.resize?.());
    }
  }, [layout]);

  useImperativeHandle(ref, () => ({
    seek: (time: number) => {
      if (asset.asset_type === "video" && mediaRef.current) {
        (mediaRef.current as HTMLVideoElement).currentTime = time;
      }
    },
    pause: () => {
      if (asset.asset_type === "video" && mediaRef.current) {
        (mediaRef.current as HTMLVideoElement).pause();
      }
    },
    getCanvasShapes: () => (canvasRef.current as any)?.exportJSON?.()?.objects ?? [],
    clearCanvasShapes: () => (canvasRef.current as any)?.clear?.(),
  }));

  const handleTimeUpdate = () => {
    if (asset.asset_type === "video" && mediaRef.current && onTimeUpdate) {
      onTimeUpdate((mediaRef.current as HTMLVideoElement).currentTime);
    }
  };

  return (
    <div ref={containerRef} className="relative w-full h-full bg-black overflow-hidden">
      {/*
        Media element: fills the container via absolute positioning + object-contain.
        The browser handles the letterbox; calculateTrueLayout then computes where
        the visible image pixels start so we can position the annotation canvas on top.
      */}
      {asset.asset_type === "video" ? (
        <video
          ref={mediaRef as React.RefObject<HTMLVideoElement>}
          src={asset.src_url}
          className="absolute inset-0 w-full h-full object-contain"
          controls
          onLoadedMetadata={calculateTrueLayout}
          onTimeUpdate={handleTimeUpdate}
        />
      ) : (
        <img
          ref={mediaRef as React.RefObject<HTMLImageElement>}
          src={asset.src_url}
          alt={asset.name}
          className="absolute inset-0 w-full h-full object-contain"
          onLoad={calculateTrueLayout}
        />
      )}

      {/*
        Annotation overlay: only rendered once we have a valid layout so the
        canvas-drawing-editor always mounts at the correct size. A 0×0 mount
        would leave the canvas buffer empty and break all drawing tools.

        The editor's built-in mouse-wheel zoom (handleWheel in its source) and
        space+drag pan work normally — use those to navigate a zoomed-in frame.
      */}
      {layout.width > 0 && (
        /* @ts-ignore */
        <canvas-drawing-editor
          ref={canvasRef}
          overlay=""
          lang="en"
          class="absolute pointer-events-auto touch-none"
          style={{
            left: layout.x,
            top: layout.y,
            width: layout.width,
            height: layout.height,
          }}
        />
      )}

      {/* Video comment timestamp markers along the bottom */}
      {asset.asset_type === "video" && asset.duration && comments && (
        <div className="absolute bottom-0 left-0 right-0 h-1 bg-gray-600/50 pointer-events-none z-10">
          {comments
            .filter(c => c.timestamp !== null && c.timestamp !== undefined && !c.parent_id)
            .map((comment) => (
              <div
                key={comment.id}
                className="absolute top-0 bottom-0 w-1.5 -ml-0.5 rounded-full pointer-events-auto cursor-pointer hover:scale-150 transition-transform"
                style={{
                  left: `${(comment.timestamp / asset.duration!) * 100}%`,
                  backgroundColor: comment.resolved ? '#22c55e' : '#3b82f6',
                }}
                title={comment.content}
                onClick={(e) => {
                  e.stopPropagation();
                  if (mediaRef.current) {
                    (mediaRef.current as HTMLVideoElement).currentTime = comment.timestamp;
                  }
                }}
              />
            ))}
        </div>
      )}
    </div>
  );
});

declare global {
  namespace JSX {
    interface IntrinsicElements {
      'canvas-drawing-editor': any;
    }
  }
}
