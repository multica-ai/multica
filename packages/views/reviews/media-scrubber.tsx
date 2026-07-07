"use client";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { cn } from "@multica/ui/lib/utils";
import type { ReviewComment } from "@multica/core/types";
import { ActorAvatar } from "../common/actor-avatar";
import { useActorName } from "@multica/core/workspace/hooks";

// --- Time formatting ---
export function formatTimecode(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  const ms = Math.floor((seconds % 1) * 100);
  if (h > 0) return `${h}:${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
  return `${m}:${s.toString().padStart(2, "0")}.${ms.toString().padStart(2, "0")}`;
}

export function formatTime(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (h > 0) return `${h}:${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
  return `${m}:${s.toString().padStart(2, "0")}`;
}

export function formatFrames(seconds: number, fps = 30): string {
  return Math.floor(seconds * fps).toString();
}

// --- Frame Preview Hook ---
function useFramePreview(streamUrl: string | undefined) {
  const previewVideoRef = useRef<HTMLVideoElement | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const seekResolveRef = useRef<(() => void) | null>(null);
  const readyRef = useRef(false);
  const [previewImage, setPreviewImage] = useState<string | null>(null);

  useEffect(() => {
    if (!streamUrl) return;

    const video = document.createElement("video");
    video.muted = true;
    video.playsInline = true;
    video.preload = "auto";
    video.crossOrigin = "anonymous";
    video.style.display = "none";
    document.body.appendChild(video);
    previewVideoRef.current = video;

    const canvas = document.createElement("canvas");
    canvas.width = 160;
    canvas.height = 90;
    canvasRef.current = canvas;

    const onReady = () => {
      readyRef.current = true;
    };

    video.addEventListener("loadeddata", onReady);

    video.addEventListener("seeked", () => {
      try {
        const ctx = canvas.getContext("2d");
        if (ctx && video.videoWidth > 0) {
          const aspectRatio = video.videoWidth / video.videoHeight;
          const w = 160;
          const h = Math.round(w / aspectRatio);
          canvas.width = w;
          canvas.height = h;
          ctx.drawImage(video, 0, 0, w, h);
          setPreviewImage(canvas.toDataURL("image/jpeg", 0.7));
        }
      } catch {
        // CORS fail gracefully
      }
      seekResolveRef.current?.();
      seekResolveRef.current = null;
    });

    video.src = streamUrl;

    return () => {
      readyRef.current = false;
      video.removeEventListener("loadeddata", onReady);
      video.src = "";
      video.remove();
      previewVideoRef.current = null;
      canvasRef.current = null;
      setPreviewImage(null);
    };
  }, [streamUrl]);

  const seekPreview = useCallback((time: number) => {
    const video = previewVideoRef.current;
    if (!video || !readyRef.current) return;
    if (seekResolveRef.current) return; // Debounce
    seekResolveRef.current = () => {};
    video.currentTime = Math.max(0, time);
  }, []);

  const clearPreview = useCallback(() => {
    setPreviewImage(null);
  }, []);

  return { previewImage, seekPreview, clearPreview };
}

interface CommentMarkerProps {
  comment: ReviewComment;
  leftPercent: number;
  color: string;
  isHovered: boolean;
  isSelected: boolean;
  onHover: () => void;
  onLeave: () => void;
  onClick: () => void;
}

function CommentMarker({
  comment,
  leftPercent,
  color,
  isHovered,
  isSelected,
  onHover,
  onLeave,
  onClick,
}: CommentMarkerProps) {
  const markerRef = useRef<HTMLDivElement>(null);
  const [tooltipPos, setTooltipPos] = useState<{ left: number; top: number } | null>(null);
  const { getActorName } = useActorName();
  const authorName = getActorName("member", comment.author_id) || "Unknown";

  useEffect(() => {
    if (!isHovered || !markerRef.current) {
      setTooltipPos(null);
      return;
    }
    const rect = markerRef.current.getBoundingClientRect();
    const tooltipWidth = 240;
    let left = rect.left + rect.width / 2 - tooltipWidth / 2;
    if (left < 8) left = 8;
    if (left + tooltipWidth > window.innerWidth - 8) left = window.innerWidth - 8 - tooltipWidth;
    setTooltipPos({ left, top: rect.top - 8 });
  }, [isHovered]);

  return (
    <div
      ref={markerRef}
      className="absolute top-0 -translate-x-1/2 cursor-pointer z-20"
      style={{ left: `${leftPercent}%` }}
      onMouseEnter={onHover}
      onMouseLeave={onLeave}
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
    >
      <div
        className={cn(
          "w-5 h-5 rounded-full shadow-md border-2 transition-transform hover:scale-110 overflow-hidden flex items-center justify-center",
          isSelected ? "scale-125 ring-2 ring-primary border-primary" : "border-background"
        )}
        style={{ backgroundColor: color }}
      >
        <ActorAvatar actorType="member" actorId={comment.author_id} size={16} />
      </div>

      {isHovered && tooltipPos && createPortal(
        <div
          style={{
            position: "fixed",
            left: tooltipPos.left,
            top: tooltipPos.top,
            width: 240,
            transform: "translateY(-100%)",
            zIndex: 9999,
            pointerEvents: "none",
          }}
        >
          <div className="bg-popover border border-border rounded-lg shadow-2xl p-3">
            <div className="flex items-center gap-2 mb-1.5">
              <ActorAvatar actorType="member" actorId={comment.author_id} size={16} />
              <span className="text-xs font-medium text-popover-foreground truncate">{authorName}</span>
              {comment.start_time !== null && comment.start_time !== undefined && (
                <span className="ml-auto text-[10px] font-mono text-primary bg-primary/10 px-1.5 py-0.5 rounded">
                  {formatTimecode(comment.start_time)}
                </span>
              )}
            </div>
            <p className="text-xs text-muted-foreground line-clamp-2 leading-relaxed">
              {comment.content}
            </p>
          </div>
          <div className="flex justify-center">
            <div className="w-2 h-2 bg-popover border-b border-r border-border rotate-45 -mt-1" />
          </div>
        </div>,
        document.body
      )}
    </div>
  );
}

export interface MediaScrubberProps {
  currentTime: number;
  duration: number;
  buffered?: number;
  comments?: ReviewComment[];
  streamUrl?: string;
  selectedCommentId?: string;
  onSeek: (time: number) => void;
  onSelectComment?: (id: string) => void;
  className?: string;
}

export function MediaScrubber({
  currentTime,
  duration,
  buffered = 0,
  comments = [],
  streamUrl,
  selectedCommentId,
  onSeek,
  onSelectComment,
  className,
}: MediaScrubberProps) {
  const trackRef = useRef<HTMLDivElement>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [hoverTime, setHoverTime] = useState<number | null>(null);
  const [hoverX, setHoverX] = useState(0);
  const [hoveredCommentId, setHoveredCommentId] = useState<string | null>(null);

  const { previewImage, seekPreview, clearPreview } = useFramePreview(streamUrl);

  const timeToPercent = useCallback(
    (time: number): number => {
      if (!duration) return 0;
      return Math.max(0, Math.min(100, (time / duration) * 100));
    },
    [duration]
  );

  const getTimeFromEvent = useCallback(
    (clientX: number): number => {
      const track = trackRef.current;
      if (!track || !duration) return 0;
      const rect = track.getBoundingClientRect();
      const ratio = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
      return ratio * duration;
    },
    [duration]
  );

  const handleMouseMove = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      const time = getTimeFromEvent(e.clientX);
      setHoverTime(time);
      const track = trackRef.current;
      if (track) {
        const rect = track.getBoundingClientRect();
        setHoverX(e.clientX - rect.left);
      }
      if (isDragging) onSeek(time);
      seekPreview(time);
    },
    [isDragging, getTimeFromEvent, onSeek, seekPreview]
  );

  const handleMouseLeave = useCallback(() => {
    if (!isDragging) {
      setHoverTime(null);
      clearPreview();
    }
  }, [isDragging, clearPreview]);

  const handleMouseDown = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      e.preventDefault();
      setIsDragging(true);
      onSeek(getTimeFromEvent(e.clientX));
    },
    [getTimeFromEvent, onSeek]
  );

  useEffect(() => {
    if (!isDragging) return;
    const handleGlobalMouseMove = (e: MouseEvent) => onSeek(getTimeFromEvent(e.clientX));
    const handleGlobalMouseUp = (e: MouseEvent) => {
      setIsDragging(false);
      setHoverTime(null);
      clearPreview();
      onSeek(getTimeFromEvent(e.clientX));
    };
    window.addEventListener("mousemove", handleGlobalMouseMove);
    window.addEventListener("mouseup", handleGlobalMouseUp);
    return () => {
      window.removeEventListener("mousemove", handleGlobalMouseMove);
      window.removeEventListener("mouseup", handleGlobalMouseUp);
    };
  }, [isDragging, getTimeFromEvent, onSeek, clearPreview]);

  const pointMarkers = comments.filter((c) => c.start_time !== null && c.start_time !== undefined && (c.end_time === null || c.end_time === undefined || c.start_time === c.end_time));
  const rangeMarkers = comments.filter((c) => c.start_time !== null && c.start_time !== undefined && c.end_time !== null && c.end_time !== undefined && c.start_time !== c.end_time);

  const playPercent = timeToPercent(currentTime);
  const bufferedPercent = timeToPercent(buffered);

  return (
    <div className={cn("relative flex flex-col w-full group/progress py-1", className)}>
      <div
        ref={trackRef}
        className="relative w-full h-1 group-hover/progress:h-1.5 transition-all duration-150 cursor-pointer bg-border rounded-full"
        onMouseMove={handleMouseMove}
        onMouseLeave={handleMouseLeave}
        onMouseDown={handleMouseDown}
      >
        <div className="absolute inset-y-0 left-0 bg-muted-foreground/30 rounded-full" style={{ width: `${bufferedPercent}%` }} />

        {rangeMarkers.map((c) => {
          if (c.start_time === null || c.start_time === undefined || c.end_time === null || c.end_time === undefined) return null;
          const left = timeToPercent(c.start_time);
          const right = timeToPercent(c.end_time);
          const isSelected = selectedCommentId === c.id;
          return (
            <div
              key={c.id}
              className={cn("absolute inset-y-0 rounded-full pointer-events-none transition-colors", isSelected ? "bg-primary/60" : "bg-primary/30")}
              style={{ left: `${left}%`, width: `${right - left}%` }}
            />
          );
        })}

        <div className="absolute inset-y-0 left-0 rounded-full bg-primary" style={{ width: `${playPercent}%` }} />

        <div
          className="absolute top-1/2 -translate-y-1/2 w-3 h-3 rounded-full bg-primary shadow-lg opacity-0 group-hover/progress:opacity-100 transition-opacity pointer-events-none z-10"
          style={{ left: `${playPercent}%`, transform: "translateX(-50%) translateY(-50%)" }}
        />
      </div>

      {pointMarkers.length > 0 && (
        <div className="relative w-full h-6 mt-0.5">
          {pointMarkers.map((c) => {
            if (c.start_time === null || c.start_time === undefined) return null;
            const left = timeToPercent(c.start_time);
            const color = c.shapes?.[0]?.color || (c.resolved ? "#22c55e" : "#3b82f6");
            const isHovered = hoveredCommentId === c.id;
            const isSelected = selectedCommentId === c.id;

            return (
              <CommentMarker
                key={c.id}
                comment={c}
                leftPercent={left}
                color={color}
                isHovered={isHovered}
                isSelected={isSelected}
                onHover={() => setHoveredCommentId(c.id)}
                onLeave={() => setHoveredCommentId(null)}
                onClick={() => {
                  if (c.start_time !== undefined) onSeek(c.start_time);
                  onSelectComment?.(c.id);
                }}
              />
            );
          })}
        </div>
      )}

      {hoverTime !== null && (
        <div className="absolute -top-2 z-30 pointer-events-none" style={{ left: hoverX, transform: "translateX(-50%) translateY(-100%)" }}>
          {previewImage && (
            <div className="mb-1 rounded-md overflow-hidden border border-border shadow-2xl bg-black">
              <img src={previewImage} alt="preview" className="w-40 object-contain" />
            </div>
          )}
          <div className="flex justify-center">
            <span className="bg-popover text-popover-foreground text-[11px] font-mono px-2 py-0.5 rounded-md border shadow">
              {formatTimecode(hoverTime)}
            </span>
          </div>
        </div>
      )}
    </div>
  );
}
