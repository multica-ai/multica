"use client";

import { useEffect, useRef, useState } from "react";
import { Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Slider } from "@multica/ui/components/ui/slider";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import {
  AVATAR_OUTPUT_SIZE,
  blobToAvatarFile,
  clampTransform,
  coverBaseScale,
  pickOutputType,
  renderCroppedAvatar,
  type CropTransform,
} from "./avatar-crop";

/** On-screen crop square side, px. The geometry math uses this exact value. */
const VIEWPORT = 256;
const MIN_SCALE = 1;
const MAX_SCALE = 3;
const SCALE_STEP = 0.01;

const IDENTITY: CropTransform = { scale: MIN_SCALE, offsetX: 0, offsetY: 0 };

export type AvatarCropShape = "circle" | "square";

interface AvatarCropDialogProps {
  /** The picked source file. `null` keeps the dialog empty. */
  file: File | null;
  open: boolean;
  shape: AvatarCropShape;
  /** Parent's upload/save is in flight — locks the controls and blocks close. */
  busy?: boolean;
  onOpenChange: (open: boolean) => void;
  /** Fires with the cropped, compressed file. The parent uploads + persists. */
  onCropped: (file: File) => void;
}

/**
 * Fixed 1:1 avatar cropper. The crop square is fixed; the image pans and zooms
 * beneath it (the near-universal avatar-editor model — users place their face
 * in the frame, they don't resize the frame). Output is a square
 * {@link AVATAR_OUTPUT_SIZE}px image; the round vs square display mask is only
 * applied at render time, never baked into the pixels.
 */
export function AvatarCropDialog({
  file,
  open,
  shape,
  busy = false,
  onOpenChange,
  onCropped,
}: AvatarCropDialogProps) {
  const { t } = useT("common");
  const imgRef = useRef<HTMLImageElement>(null);
  const [objectUrl, setObjectUrl] = useState<string | null>(null);
  const [natural, setNatural] = useState<{ w: number; h: number } | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [transform, setTransform] = useState<CropTransform>(IDENTITY);
  const dragRef = useRef<{
    startX: number;
    startY: number;
    ox: number;
    oy: number;
  } | null>(null);

  // One object URL per picked file, alive for the dialog's lifetime and used
  // for both the preview and the drawImage source. Reset the transform so a
  // new pick starts centered.
  useEffect(() => {
    if (!file) {
      setObjectUrl(null);
      setNatural(null);
      setLoadError(false);
      return;
    }
    const url = URL.createObjectURL(file);
    setObjectUrl(url);
    setNatural(null);
    setLoadError(false);
    setTransform(IDENTITY);
    return () => URL.revokeObjectURL(url);
  }, [file]);

  const geometry = natural
    ? { imageWidth: natural.w, imageHeight: natural.h, viewport: VIEWPORT }
    : null;

  const base = natural
    ? coverBaseScale(natural.w, natural.h, VIEWPORT)
    : 1;
  const drawnW = natural ? natural.w * base * transform.scale : VIEWPORT;
  const drawnH = natural ? natural.h * base * transform.scale : VIEWPORT;

  const applyTransform = (next: CropTransform) => {
    if (!geometry) {
      setTransform(next);
      return;
    }
    setTransform(clampTransform({ ...geometry, ...next }));
  };

  const handlePointerDown = (e: React.PointerEvent) => {
    if (busy || !natural) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    dragRef.current = {
      startX: e.clientX,
      startY: e.clientY,
      ox: transform.offsetX,
      oy: transform.offsetY,
    };
  };

  const handlePointerMove = (e: React.PointerEvent) => {
    const drag = dragRef.current;
    if (!drag) return;
    applyTransform({
      scale: transform.scale,
      offsetX: drag.ox + (e.clientX - drag.startX),
      offsetY: drag.oy + (e.clientY - drag.startY),
    });
  };

  const handlePointerUp = (e: React.PointerEvent) => {
    dragRef.current = null;
    try {
      e.currentTarget.releasePointerCapture(e.pointerId);
    } catch {
      // Capture may already be gone (pointercancel); ignore.
    }
  };

  const handleConfirm = async () => {
    const image = imgRef.current;
    if (!image || !natural || !file) return;
    const { type, quality } = pickOutputType();
    try {
      const blob = await renderCroppedAvatar(
        image,
        {
          imageWidth: natural.w,
          imageHeight: natural.h,
          viewport: VIEWPORT,
          ...transform,
        },
        {
          output: AVATAR_OUTPUT_SIZE,
          type,
          quality,
          // JPEG has no alpha; give transparent source pixels a white bed
          // instead of the canvas default black.
          background: type === "image/jpeg" ? "#ffffff" : undefined,
        },
      );
      onCropped(blobToAvatarFile(blob, file.name, type));
    } catch {
      setLoadError(true);
    }
  };

  const disabled = busy || !natural || loadError;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        // Never let a close slip through while an upload is committing.
        if (busy) return;
        onOpenChange(next);
      }}
    >
      <DialogContent showCloseButton={!busy}>
        <DialogHeader>
          <DialogTitle>{t(($) => $.avatar_crop.title)}</DialogTitle>
        </DialogHeader>

        <div className="flex flex-col items-center gap-4">
          <div
            className={cn(
              "relative overflow-hidden bg-muted select-none touch-none",
              shape === "circle" ? "rounded-full" : "rounded-lg",
              busy ? "cursor-default" : "cursor-grab active:cursor-grabbing",
            )}
            style={{ width: VIEWPORT, height: VIEWPORT }}
            onPointerDown={handlePointerDown}
            onPointerMove={handlePointerMove}
            onPointerUp={handlePointerUp}
            onPointerCancel={handlePointerUp}
          >
            {objectUrl && !loadError ? (
              <img
                ref={imgRef}
                src={objectUrl}
                alt=""
                draggable={false}
                onLoad={(e) =>
                  setNatural({
                    w: e.currentTarget.naturalWidth,
                    h: e.currentTarget.naturalHeight,
                  })
                }
                onError={() => setLoadError(true)}
                className="pointer-events-none absolute max-w-none select-none"
                style={{
                  width: drawnW,
                  height: drawnH,
                  left: (VIEWPORT - drawnW) / 2 + transform.offsetX,
                  top: (VIEWPORT - drawnH) / 2 + transform.offsetY,
                }}
              />
            ) : (
              <div className="flex h-full w-full items-center justify-center text-xs text-muted-foreground">
                {loadError
                  ? t(($) => $.avatar_crop.load_failed)
                  : t(($) => $.avatar_crop.loading)}
              </div>
            )}
          </div>

          <div className="flex w-full max-w-xs items-center gap-3">
            <span className="text-xs text-muted-foreground shrink-0">
              {t(($) => $.avatar_crop.zoom)}
            </span>
            <Slider
              value={[transform.scale]}
              min={MIN_SCALE}
              max={MAX_SCALE}
              step={SCALE_STEP}
              disabled={disabled}
              onValueChange={(value) =>
                applyTransform({
                  scale: (Array.isArray(value) ? value[0] : value) ?? MIN_SCALE,
                  offsetX: transform.offsetX,
                  offsetY: transform.offsetY,
                })
              }
              aria-label={t(($) => $.avatar_crop.zoom)}
            />
          </div>

          <p className="text-xs text-muted-foreground">
            {t(($) => $.avatar_crop.hint)}
          </p>
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={busy}
          >
            {t(($) => $.avatar_crop.cancel)}
          </Button>
          <Button onClick={handleConfirm} disabled={disabled}>
            {busy ? (
              <>
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                {t(($) => $.avatar_crop.uploading)}
              </>
            ) : (
              t(($) => $.avatar_crop.apply)
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
