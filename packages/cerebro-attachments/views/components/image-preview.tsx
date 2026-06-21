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
// FIR-1713: the FIR-1673 recovery only fired on the <img> `error` event, but a
// transfer that *stalls* mid-load (the actual reported symptom — top decoded,
// rest blank) emits no `error` event, so neither the auto-retry nor the manual
// button ever triggered. Two fixes here:
//   - a stall watchdog: if `load` hasn't fired within STALL_TIMEOUT_MS, treat
//     the attempt as failed (auto-retry, then surface the error state), so a
//     hung transfer recovers on its own instead of locking,
//   - an always-visible "Reload image" button on the page, so a user can
//     re-trigger the fetch at any time (manual recovery + test trigger) without
//     having to wait for the watchdog or reach the error state.
//
// Each retry re-points the <img> at a cache-busted URL so the browser actually
// re-fetches instead of reusing the failed response.

const MAX_AUTO_RETRIES = 2;
const RETRY_DELAY_MS = 600;
// A load that hasn't finished within this window is treated as stalled. Picked
// well above a normal slow-network image fetch so it only catches true hangs.
const STALL_TIMEOUT_MS = 10000;

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

  // A failed attempt — whether the browser fired `error` or the stall watchdog
  // tripped. Silently retry a couple of times with a cache-busted URL, then
  // surface the error state with the manual reload + download fallback.
  const registerFailure = useCallback(() => {
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

  const handleLoad = useCallback(() => {
    autoRetriesRef.current = 0;
    setStatus("loaded");
  }, []);

  const handleError = useCallback(() => {
    registerFailure();
  }, [registerFailure]);

  // Stall watchdog: while an attempt is in flight, a hung transfer never fires
  // `load` or `error`, so without this it would spin forever. If the image
  // hasn't loaded within STALL_TIMEOUT_MS, treat the attempt as failed.
  useEffect(() => {
    if (status !== "loading") return;
    const timer = setTimeout(() => {
      registerFailure();
    }, STALL_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [status, attempt, src, registerFailure]);

  const handleManualReload = useCallback(() => {
    if (retryTimer.current) clearTimeout(retryTimer.current);
    autoRetriesRef.current = 0;
    setStatus("loading");
    setAttempt((a) => a + 1);
  }, []);

  return (
    <div>
      {/* Always-visible controls (FIR-1713): a user can re-trigger the fetch at
          any time, both to recover a stuck image and to test the load. */}
      <div className="mb-2 flex items-center justify-end gap-3">
        {status === "loading" && (
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" />
            Loading…
          </span>
        )}
        <Button variant="outline" size="sm" onClick={handleManualReload}>
          <RotateCw className="mr-1 size-4" /> Reload image
        </Button>
      </div>

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
    </div>
  );
}
