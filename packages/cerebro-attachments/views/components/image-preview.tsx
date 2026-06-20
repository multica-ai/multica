"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Download, Loader2, RotateCw } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { ZoomableImage } from "@multica/cerebro-ui";

// ---------------------------------------------------------------------------
// ImagePreview — image document viewer that recovers from a stalled load.
// ---------------------------------------------------------------------------
//
// FIR-1673: an image document could stop mid-load and never finish, leaving a
// half-blank preview with no way out but a full page reload. This wraps
// ZoomableImage with three recovery affordances:
//   - a loading indicator while the bytes are in flight,
//   - up to MAX_AUTO_RETRIES silent retries when the load fails,
//   - a manual "Reload image" button once the automatic retries are spent.
//
// Each retry re-points the <img> at a cache-busted URL so the browser actually
// re-fetches instead of reusing the failed response.

const MAX_AUTO_RETRIES = 2;
const RETRY_DELAY_MS = 600;

type Status = "loading" | "loaded" | "error";

function withReloadParam(src: string, attempt: number): string {
  if (attempt === 0) return src;
  const sep = src.includes("?") ? "&" : "?";
  return `${src}${sep}_reload=${attempt}`;
}

export interface ImagePreviewProps {
  src: string;
  alt: string;
  /** Classes for the clipping viewport (matches ZoomableImage). */
  viewportClassName?: string;
  /** Direct download URL, offered as a fallback when the preview can't load. */
  downloadUrl?: string;
}

export function ImagePreview({
  src,
  alt,
  viewportClassName,
  downloadUrl,
}: ImagePreviewProps) {
  const [status, setStatus] = useState<Status>("loading");
  // `attempt` doubles as the cache-buster: 0 = first try, then 1, 2, … on retry.
  const [attempt, setAttempt] = useState(0);
  const autoRetriesRef = useRef(0);
  const retryTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // A brand-new source is a fresh load — clear retry bookkeeping.
  useEffect(() => {
    autoRetriesRef.current = 0;
    setAttempt(0);
    setStatus("loading");
  }, [src]);

  useEffect(
    () => () => {
      if (retryTimer.current) clearTimeout(retryTimer.current);
    },
    [],
  );

  const handleLoad = useCallback(() => {
    autoRetriesRef.current = 0;
    setStatus("loaded");
  }, []);

  const handleError = useCallback(() => {
    if (autoRetriesRef.current < MAX_AUTO_RETRIES) {
      autoRetriesRef.current += 1;
      retryTimer.current = setTimeout(() => {
        setStatus("loading");
        setAttempt((a) => a + 1);
      }, RETRY_DELAY_MS);
      return;
    }
    setStatus("error");
  }, []);

  const handleManualReload = useCallback(() => {
    if (retryTimer.current) clearTimeout(retryTimer.current);
    autoRetriesRef.current = 0;
    setStatus("loading");
    setAttempt((a) => a + 1);
  }, []);

  return (
    <div className="relative">
      {status !== "error" && (
        <ZoomableImage
          src={withReloadParam(src, attempt)}
          alt={alt}
          viewportClassName={viewportClassName}
          onLoad={handleLoad}
          onError={handleError}
        />
      )}

      {status === "loading" && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
          <div className="flex items-center gap-2 rounded-md bg-background/80 px-3 py-2 text-sm text-muted-foreground shadow-sm">
            <Loader2 className="size-4 animate-spin" />
            Loading image…
          </div>
        </div>
      )}

      {status === "error" && (
        <div
          className={
            viewportClassName ??
            "mx-auto h-[80vh] w-full max-w-5xl rounded border border-border bg-muted/10"
          }
        >
          <div className="flex h-full flex-col items-center justify-center gap-3 p-6 text-center">
            <p className="text-sm text-muted-foreground">
              The image couldn’t finish loading.
            </p>
            <div className="flex items-center gap-2">
              <Button variant="outline" size="sm" onClick={handleManualReload}>
                <RotateCw className="mr-1 size-4" /> Reload image
              </Button>
              {downloadUrl && (
                <a
                  href={downloadUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 text-sm underline hover:text-foreground"
                >
                  <Download className="size-3.5" /> Download
                </a>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
