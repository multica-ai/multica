"use client";

/**
 * Simple image viewer with zoom controls.
 */
import { useState } from "react";
import { ZoomIn, ZoomOut, RotateCw } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";

export function ImageViewer({ url, alt }: { url: string; alt?: string }) {
  const [zoom, setZoom] = useState(100);

  return (
    <div className="flex h-full flex-col">
      {/* Toolbar */}
      <div className="flex items-center justify-center gap-1 border-b px-4 py-1.5 bg-muted/20">
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={() => setZoom((z) => Math.max(25, z - 25))}
                disabled={zoom <= 25}
                className="text-muted-foreground"
              >
                <ZoomOut className="size-3.5" />
              </Button>
            }
          />
          <TooltipContent>Zoom out</TooltipContent>
        </Tooltip>

        <span className="text-xs font-mono text-muted-foreground w-12 text-center">
          {zoom}%
        </span>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={() => setZoom((z) => Math.min(400, z + 25))}
                disabled={zoom >= 400}
                className="text-muted-foreground"
              >
                <ZoomIn className="size-3.5" />
              </Button>
            }
          />
          <TooltipContent>Zoom in</TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={() => setZoom(100)}
                className="text-muted-foreground"
              >
                <RotateCw className="size-3.5" />
              </Button>
            }
          />
          <TooltipContent>Reset zoom</TooltipContent>
        </Tooltip>
      </div>

      {/* Image canvas */}
      <div className="flex-1 min-h-0 overflow-auto flex items-center justify-center p-4 bg-[repeating-conic-gradient(#80808010_0%_25%,transparent_0%_50%)] bg-[length:20px_20px]">
        <img
          src={url}
          alt={alt ?? "Image preview"}
          className="max-w-none transition-transform"
          style={{ width: `${zoom}%` }}
        />
      </div>
    </div>
  );
}
