import React, { useRef, useEffect, useState, useCallback, useImperativeHandle, forwardRef } from "react";
import { Play, Pause, Maximize2, SkipBack, SkipForward } from "lucide-react";
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from "@multica/ui/components/ui/tooltip";
import type { ReviewAsset, ReviewComment } from "@multica/core/types";

export interface MediaReviewPlayerProps {
  asset: ReviewAsset;
  onTimeUpdate?: (currentTime: number) => void;
  comments?: ReviewComment[];
  selectedCommentId?: string;
  onSelectComment?: (id: string) => void;
  onDrawingShapeChange?: (shape: any) => void;
}

export interface MediaReviewPlayerRef {
  seek: (time: number) => void;
  pause: () => void;
  getCanvasShapes: () => any;
  clearCanvasShapes: () => void;
  getCurrentTime: () => number;
}

export const MediaReviewPlayer = forwardRef<MediaReviewPlayerRef, MediaReviewPlayerProps>(
  ({ asset, onTimeUpdate, comments, selectedCommentId, onSelectComment, onDrawingShapeChange }, ref) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const mediaRef = useRef<HTMLVideoElement | HTMLImageElement>(null);
  const overlayRef = useRef<HTMLDivElement>(null);

  const [layout, setLayout] = useState({ x: 0, y: 0, width: 0, height: 0 });
  const [drawingShape, setDrawingShape] = useState<any>(null);
  const [isDrawing, setIsDrawing] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);

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
      renderHeight = container.height;
      renderWidth = renderHeight * mediaAspect;
      offsetX = (container.width - renderWidth) / 2;
      offsetY = 0;
    } else {
      renderWidth = container.width;
      renderHeight = renderWidth / mediaAspect;
      offsetX = 0;
      offsetY = (container.height - renderHeight) / 2;
    }

    setLayout({ x: offsetX, y: offsetY, width: renderWidth, height: renderHeight });
  }, [asset.asset_type]);

  useEffect(() => {
    if (!containerRef.current) return;
    const observer = new ResizeObserver(() => requestAnimationFrame(calculateTrueLayout));
    observer.observe(containerRef.current);
    return () => observer.disconnect();
  }, [calculateTrueLayout]);

  useEffect(() => {
    requestAnimationFrame(calculateTrueLayout);
  }, [calculateTrueLayout]);

  useEffect(() => {
    setLayout({ x: 0, y: 0, width: 0, height: 0 });
    setDrawingShape(null);
    onDrawingShapeChange?.(null);
  }, [asset.id]);

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
      if (!drawingShape) return [];
      const shape = { ...drawingShape };
      if (shape.width < 0) { shape.x += shape.width; shape.width = Math.abs(shape.width); }
      if (shape.height < 0) { shape.y += shape.height; shape.height = Math.abs(shape.height); }
      if (shape.width < 0.01 && shape.height < 0.01) return [];
      return [shape];
    },
    clearCanvasShapes: () => {
      setDrawingShape(null);
      onDrawingShapeChange?.(null);
    },
    getCurrentTime: () => currentTime,
  }));

  const handleTimeUpdate = () => {
    if (asset.asset_type === "video" && mediaRef.current) {
      const time = (mediaRef.current as HTMLVideoElement).currentTime;
      setCurrentTime(time);
      if (onTimeUpdate) onTimeUpdate(time);
    }
  };

  const handlePointerDown = (e: React.PointerEvent) => {
    if (e.button !== 0 || !overlayRef.current) return;
    const rect = overlayRef.current.getBoundingClientRect();
    const x = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    const y = Math.max(0, Math.min(1, (e.clientY - rect.top) / rect.height));
    
    setIsDrawing(true);
    const color = ['#ef4444', '#f59e0b', '#10b981', '#3b82f6', '#a855f7', '#ec4899'][Math.floor(Math.random() * 6)];
    const newShape = { type: 'rectangle', x, y, width: 0, height: 0, color, strokeWidth: 2 };
    setDrawingShape(newShape);
    onDrawingShapeChange?.(newShape);
    
    if (asset.asset_type === "video" && mediaRef.current) {
      (mediaRef.current as HTMLVideoElement).pause();
    }
  };

  const handlePointerMove = (e: React.PointerEvent) => {
    if (!isDrawing || !drawingShape || !overlayRef.current) return;
    const rect = overlayRef.current.getBoundingClientRect();
    const x = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    const y = Math.max(0, Math.min(1, (e.clientY - rect.top) / rect.height));
    
    const newShape = {
      ...drawingShape,
      width: x - drawingShape.x,
      height: y - drawingShape.y,
    };
    setDrawingShape(newShape);
    onDrawingShapeChange?.(newShape);
  };

  const handlePointerUp = () => {
    setIsDrawing(false);
  };

  const visibleComments = (comments || []).filter(c => {
    if (asset.asset_type === 'image') return true;
    if (c.start_time !== null && c.start_time !== undefined && c.end_time !== null && c.end_time !== undefined) {
      if (c.start_time === c.end_time) {
        return Math.abs(currentTime - c.start_time) <= 0.25;
      }
      return currentTime >= c.start_time && currentTime <= c.end_time;
    }
    return false;
  });

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (asset.asset_type !== "video" || !mediaRef.current) return;
    const video = mediaRef.current as HTMLVideoElement;
    
    // Ignore keyboard events if we're focused in an input/textarea
    if (document.activeElement?.tagName === 'INPUT' || document.activeElement?.tagName === 'TEXTAREA') return;

    switch (e.key) {
      case ' ':
        e.preventDefault();
        video.paused ? video.play() : video.pause();
        break;
      case 'ArrowLeft':
        e.preventDefault();
        video.currentTime = Math.max(0, video.currentTime - 5);
        break;
      case 'ArrowRight':
        e.preventDefault();
        if (asset.duration) {
          video.currentTime = Math.min(asset.duration, video.currentTime + 5);
        }
        break;
    }
  };

  const [isPlaying, setIsPlaying] = useState(false);
  const handlePlayPause = () => {
    if (!mediaRef.current || asset.asset_type !== "video") return;
    const video = mediaRef.current as HTMLVideoElement;
    if (video.paused) video.play();
    else video.pause();
  };

  const handleFullscreen = () => {
    if (containerRef.current) {
      if (document.fullscreenElement) {
        document.exitFullscreen();
      } else {
        containerRef.current.requestFullscreen();
      }
    }
  };

  const stepFrame = (frames: number) => {
    if (!mediaRef.current || asset.asset_type !== "video") return;
    const video = mediaRef.current as HTMLVideoElement;
    // Assume 30fps for stepping
    video.currentTime = Math.max(0, Math.min(asset.duration || 0, video.currentTime + (frames * (1/30))));
  };

  return (
    <TooltipProvider>
      <div 
        ref={containerRef} 
        className="relative w-full h-full overflow-hidden flex items-center justify-center select-none rounded-md outline-none bg-black group"
        tabIndex={0}
        onKeyDown={handleKeyDown}
      >
        {asset.asset_type === "video" ? (
          <video
            ref={mediaRef as React.RefObject<HTMLVideoElement>}
            src={asset.src_url}
            className="absolute inset-0 w-full h-full object-contain shadow-lg rounded-sm"
            onLoadedMetadata={calculateTrueLayout}
            onTimeUpdate={handleTimeUpdate}
            onPlay={() => setIsPlaying(true)}
            onPause={() => setIsPlaying(false)}
            onClick={handlePlayPause}
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

      {layout.width > 0 && (
        <div
          ref={overlayRef}
          className="absolute pointer-events-auto cursor-crosshair touch-none"
          style={{
            left: layout.x,
            top: layout.y,
            width: layout.width,
            height: layout.height,
          }}
          onPointerDown={handlePointerDown}
          onPointerMove={handlePointerMove}
          onPointerUp={handlePointerUp}
          onPointerCancel={handlePointerUp}
        >
          {visibleComments.map(c => 
            c.shapes?.map((s: any, i: number) => {
              const isSelected = selectedCommentId === c.id;
              return (
                <div 
                  key={`${c.id}-${i}`} 
                  className="absolute pointer-events-auto transition-all cursor-pointer" 
                  style={{
                    left: `${s.x * 100}%`,
                    top: `${s.y * 100}%`,
                    width: `${s.width * 100}%`,
                    height: `${s.height * 100}%`,
                    border: `2px solid ${s.color}`,
                    boxShadow: isSelected 
                      ? `0 0 0 2px rgba(255,255,255,0.8), 0 0 15px ${s.color}` 
                      : '0 0 0 1px rgba(0,0,0,0.5), inset 0 0 0 1px rgba(0,0,0,0.5)',
                    backgroundColor: isSelected ? `${s.color}40` : `${s.color}20`,
                    zIndex: isSelected ? 10 : 1
                  }}
                  onPointerDown={(e) => {
                    e.stopPropagation();
                    if (onSelectComment) onSelectComment(c.id);
                  }}
                />
              );
            })
          )}

          {drawingShape && (
            <div className="absolute border-2 pointer-events-none z-20" style={{
              left: `${Math.min(drawingShape.x, drawingShape.x + drawingShape.width) * 100}%`,
              top: `${Math.min(drawingShape.y, drawingShape.y + drawingShape.height) * 100}%`,
              width: `${Math.abs(drawingShape.width) * 100}%`,
              height: `${Math.abs(drawingShape.height) * 100}%`,
              borderColor: drawingShape.color,
              backgroundColor: `${drawingShape.color}30`,
              boxShadow: '0 0 0 1px rgba(0,0,0,0.5), inset 0 0 0 1px rgba(0,0,0,0.5)'
            }} />
          )}
        </div>
      )}

      {asset.asset_type === "video" && asset.duration && comments && (
        <div className="absolute bottom-12 left-0 right-0 h-2 pointer-events-none z-10 px-4">
          {comments
            .filter(c => c.start_time !== null && c.start_time !== undefined && !c.parent_id)
            .map((comment) => {
              const isSelected = selectedCommentId === comment.id;
              const color = comment.shapes?.[0]?.color || (comment.resolved ? '#22c55e' : '#3b82f6');
              
              return (
              <div
                key={comment.id}
                className={`absolute top-0 bottom-0 pointer-events-auto cursor-pointer transition-all ${
                  isSelected 
                    ? 'scale-y-[2.5] opacity-100 z-20 border-y border-white' 
                    : 'hover:scale-y-150 opacity-70 hover:opacity-100 z-10'
                }`}
                style={{
                  left: `calc(1rem + ${(comment.start_time! / asset.duration!) * 100}%)`,
                  width: `${((comment.end_time! - comment.start_time!) / asset.duration!) * 100}%`,
                  minWidth: '6px',
                  backgroundColor: color,
                  borderRadius: '3px',
                  boxShadow: isSelected ? `0 0 12px ${color}` : `0 0 6px ${color}80`,
                  transformOrigin: 'bottom'
                }}
                title={comment.content}
                onClick={(e) => {
                  e.stopPropagation();
                  if (mediaRef.current) {
                    (mediaRef.current as HTMLVideoElement).currentTime = comment.start_time!;
                  }
                  if (onSelectComment) onSelectComment(comment.id);
                }}
              />
            )})}
        </div>
        </div>
      )}

      {/* Glassmorphism Custom Controls (only for video) */}
      {asset.asset_type === "video" && (
        <div className="absolute bottom-4 left-1/2 -translate-x-1/2 flex items-center gap-2 px-4 py-2 rounded-full backdrop-blur-md bg-background/80 border border-border/50 shadow-lg opacity-0 group-hover:opacity-100 transition-opacity duration-300 z-30">
          <Tooltip>
            <TooltipTrigger asChild>
              <button onClick={() => stepFrame(-1)} className="p-1.5 hover:bg-muted rounded-full text-foreground transition-colors">
                <SkipBack className="w-4 h-4" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="top">Frame Back</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <button onClick={handlePlayPause} className="p-2 bg-foreground text-background hover:scale-105 rounded-full transition-transform">
                {isPlaying ? <Pause className="w-4 h-4" /> : <Play className="w-4 h-4 ml-0.5" />}
              </button>
            </TooltipTrigger>
            <TooltipContent side="top">{isPlaying ? "Pause" : "Play"}</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <button onClick={() => stepFrame(1)} className="p-1.5 hover:bg-muted rounded-full text-foreground transition-colors">
                <SkipForward className="w-4 h-4" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="top">Frame Forward</TooltipContent>
          </Tooltip>

          <div className="w-px h-4 bg-border mx-1" />

          <Tooltip>
            <TooltipTrigger asChild>
              <button onClick={handleFullscreen} className="p-1.5 hover:bg-muted rounded-full text-foreground transition-colors">
                <Maximize2 className="w-4 h-4" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="top">Fullscreen</TooltipContent>
          </Tooltip>
        </div>
      )}
    </div>
    </TooltipProvider>
  );
});
