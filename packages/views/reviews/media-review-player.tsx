import React, { useRef, useEffect, useState, useCallback, useImperativeHandle, forwardRef } from "react";
import { ReviewAsset } from "@multica/core/types";
import "@multica/canvas-drawing-editor";

export interface MediaReviewPlayerProps {
  asset: ReviewAsset;
  onTimeUpdate?: (currentTime: number) => void;
}

export interface MediaReviewPlayerRef {
  seek: (time: number) => void;
  pause: () => void;
  getCanvasShapes: () => any;
  clearCanvasShapes: () => void;
}

export const MediaReviewPlayer = forwardRef<MediaReviewPlayerRef, MediaReviewPlayerProps>(
  ({ asset, onTimeUpdate }, ref) => {
    const containerRef = useRef<HTMLDivElement>(null);
  const mediaRef = useRef<HTMLVideoElement | HTMLImageElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);

  const [layout, setLayout] = useState({ x: 0, y: 0, width: 0, height: 0 });

  // Calculate the true rendered dimensions of the video/image to account for letterboxing
  const calculateTrueLayout = useCallback(() => {
    if (!containerRef.current || !mediaRef.current || !canvasRef.current) return;

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

    if (mediaWidth === 0 || mediaHeight === 0) return;

    const containerAspect = container.width / container.height;
    const mediaAspect = mediaWidth / mediaHeight;

    let renderWidth, renderHeight, offsetX, offsetY;

    if (containerAspect > mediaAspect) {
      // Container is wider than media -> pillarboxed (bars on sides)
      renderHeight = container.height;
      renderWidth = renderHeight * mediaAspect;
      offsetX = (container.width - renderWidth) / 2;
      offsetY = 0;
    } else {
      // Container is taller than media -> letterboxed (bars on top/bottom)
      renderWidth = container.width;
      renderHeight = renderWidth / mediaAspect;
      offsetX = 0;
      offsetY = (container.height - renderHeight) / 2;
    }

    setLayout({
      x: offsetX,
      y: offsetY,
      width: renderWidth,
      height: renderHeight,
    });

    // Update canvas resolution to match rendered size for crisp drawing
    canvasRef.current.width = renderWidth;
    canvasRef.current.height = renderHeight;
  }, [asset.asset_type]);

  // Convert a mouse event (clientX/Y) to a normalized 0.0-1.0 coordinate
  // @ts-ignore
  const getNormalizedCoordinates = useCallback(
    (clientX: number, clientY: number) => {
      if (!canvasRef.current) return { x: 0, y: 0 };
      const rect = canvasRef.current.getBoundingClientRect();
      return {
        x: (clientX - rect.left) / layout.width,
        y: (clientY - rect.top) / layout.height,
      };
    },
    [layout]
  );

  // Convert a normalized 0.0-1.0 coordinate to a render pixel coordinate
  // @ts-ignore
  const getRenderCoordinates = useCallback(
    (nx: number, ny: number) => {
      return {
        x: nx * layout.width,
        y: ny * layout.height,
      };
    },
    [layout]
  );

  useEffect(() => {
    if (!containerRef.current) return;

    const observer = new ResizeObserver(() => {
      requestAnimationFrame(calculateTrueLayout);
    });

    observer.observe(containerRef.current);
    return () => observer.disconnect();
  }, [calculateTrueLayout]);

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
    getCanvasShapes: () => {
      if (canvasRef.current && (canvasRef.current as any).exportJSON) {
        return (canvasRef.current as any).exportJSON().objects;
      }
      return [];
    },
    clearCanvasShapes: () => {
      if (canvasRef.current && (canvasRef.current as any).clear) {
        (canvasRef.current as any).clear();
      }
    }
  }));

  const handleTimeUpdate = () => {
    if (asset.asset_type === "video" && mediaRef.current && onTimeUpdate) {
      onTimeUpdate((mediaRef.current as HTMLVideoElement).currentTime);
    }
  };

  return (
    <div ref={containerRef} className="relative w-full h-full flex items-center justify-center bg-black overflow-hidden">
      {asset.asset_type === "video" ? (
        <video 
          ref={mediaRef as React.RefObject<HTMLVideoElement>} 
          src={asset.file_url} 
          className="max-w-full max-h-full" 
          controls 
          onLoadedMetadata={calculateTrueLayout}
          onTimeUpdate={handleTimeUpdate}
        />
      ) : (
        <img 
          ref={mediaRef as React.RefObject<HTMLImageElement>} 
          src={asset.file_url} 
          alt={asset.name} 
          className="max-w-full max-h-full object-contain" 
          onLoad={calculateTrueLayout}
        />
      )}
      
      
      {/* 
        The canvas editor is absolutely positioned exactly over the rendered pixels of the media. 
        pointer-events-auto ensures it catches mouse events for drawing. 
      */}
      <canvas-drawing-editor 
        ref={canvasRef as any}
        class="absolute pointer-events-auto touch-none"
        style={{
          left: `${layout.x}px`,
          top: `${layout.y}px`,
          width: `${layout.width}px`,
          height: `${layout.height}px`,
          backgroundColor: 'transparent'
        }}
      />
    </div>
  );
});
// Register the custom element type for TypeScript
declare global {
  namespace JSX {
    interface IntrinsicElements {
      'canvas-drawing-editor': any;
    }
  }
}
