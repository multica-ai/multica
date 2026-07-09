"use client";

import { useEffect, useState } from "react";
import Cropper, { type Area, type Point } from "react-easy-crop";
import { Loader2, Minus, Plus, RotateCw } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Slider } from "@multica/ui/components/ui/slider";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../i18n";
import {
  AVATAR_OUTPUT_SIZE,
  blobToAvatarFile,
  getCroppedAvatarBlob,
  pickOutputType,
  type PixelCrop,
} from "./avatar-crop";

const MIN_ZOOM = 1;
const MAX_ZOOM = 3;
const ZOOM_STEP = 0.1;
const ROUNDED_SQUARE_CROP_AREA_STYLE = { borderRadius: "0.5rem" };

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
 * Avatar cropper. The image pans/zooms/rotates beneath a fixed crop window
 * (round for people, rounded-square for non-human actors); everything outside
 * the window is dimmed. Output is a square {@link AVATAR_OUTPUT_SIZE}px image —
 * the round/square mask is display-only, never baked into the pixels.
 *
 * Interaction (drag / zoom / rotate + the dim overlay) is delegated to
 * react-easy-crop; this component owns the chrome (header, zoom slider, rotate
 * / reset controls) and the encode/upload handoff.
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
  const [objectUrl, setObjectUrl] = useState<string | null>(null);
  const [crop, setCrop] = useState<Point>({ x: 0, y: 0 });
  const [zoom, setZoom] = useState(MIN_ZOOM);
  const [rotation, setRotation] = useState(0);
  const [croppedAreaPixels, setCroppedAreaPixels] = useState<PixelCrop | null>(
    null,
  );
  const [loadError, setLoadError] = useState(false);

  // One object URL per picked file, alive for the dialog's lifetime; reset the
  // transform so a new pick starts centered and unrotated.
  useEffect(() => {
    if (!file) {
      setObjectUrl(null);
      return;
    }
    const url = URL.createObjectURL(file);
    setObjectUrl(url);
    setCrop({ x: 0, y: 0 });
    setZoom(MIN_ZOOM);
    setRotation(0);
    setCroppedAreaPixels(null);
    setLoadError(false);
    return () => URL.revokeObjectURL(url);
  }, [file]);

  const clampZoom = (value: number) =>
    Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, Math.round(value * 100) / 100));

  const resetTransform = () => {
    setCrop({ x: 0, y: 0 });
    setZoom(MIN_ZOOM);
    setRotation(0);
  };

  const handleConfirm = async () => {
    if (!objectUrl || !croppedAreaPixels || !file) return;
    const { type, quality } = pickOutputType();
    try {
      const blob = await getCroppedAvatarBlob(objectUrl, croppedAreaPixels, rotation, {
        output: AVATAR_OUTPUT_SIZE,
        type,
        quality,
        // JPEG has no alpha; give transparent/rotated corners a white bed
        // instead of the canvas default black.
        background: type === "image/jpeg" ? "#ffffff" : undefined,
      });
      onCropped(blobToAvatarFile(blob, file.name, type));
    } catch {
      setLoadError(true);
    }
  };

  const disabled = busy || !objectUrl || loadError || !croppedAreaPixels;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        // Never let a close slip through while an upload is committing.
        if (busy) return;
        onOpenChange(next);
      }}
    >
      <DialogContent showCloseButton={!busy} className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.avatar_crop.title)}</DialogTitle>
        </DialogHeader>

        <div className="relative aspect-square w-full overflow-hidden rounded-lg bg-neutral-900">
          {objectUrl && !loadError ? (
            <>
              <Cropper
                image={objectUrl}
                crop={crop}
                zoom={zoom}
                rotation={rotation}
                aspect={1}
                minZoom={MIN_ZOOM}
                maxZoom={MAX_ZOOM}
                cropShape={shape === "circle" ? "round" : "rect"}
                style={{
                  cropAreaStyle:
                    shape === "circle" ? undefined : ROUNDED_SQUARE_CROP_AREA_STYLE,
                }}
                showGrid={false}
                onCropChange={setCrop}
                onZoomChange={setZoom}
                onRotationChange={setRotation}
                onCropComplete={(_: Area, areaPixels: Area) =>
                  setCroppedAreaPixels(areaPixels)
                }
              />
              <button
                type="button"
                onClick={() => setRotation((r) => (r + 90) % 360)}
                disabled={busy}
                aria-label={t(($) => $.avatar_crop.rotate)}
                className="absolute right-2 top-2 z-10 flex h-8 w-8 items-center justify-center rounded-full bg-background/90 text-foreground shadow-sm transition-colors hover:bg-background disabled:opacity-50"
              >
                <RotateCw className="h-4 w-4" />
              </button>
            </>
          ) : (
            <div className="flex h-full w-full items-center justify-center text-xs text-muted-foreground">
              {loadError
                ? t(($) => $.avatar_crop.load_failed)
                : t(($) => $.avatar_crop.loading)}
            </div>
          )}
        </div>

        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={() => setZoom((z) => clampZoom(z - ZOOM_STEP))}
            disabled={disabled}
            aria-label={t(($) => $.avatar_crop.zoom_out)}
            className="shrink-0 text-muted-foreground transition-colors hover:text-foreground disabled:opacity-40"
          >
            <Minus className="h-4 w-4" />
          </button>
          <Slider
            value={[zoom]}
            min={MIN_ZOOM}
            max={MAX_ZOOM}
            step={0.01}
            disabled={disabled}
            onValueChange={(value) =>
              setZoom((Array.isArray(value) ? value[0] : value) ?? MIN_ZOOM)
            }
            aria-label={t(($) => $.avatar_crop.zoom)}
            className="flex-1"
          />
          <button
            type="button"
            onClick={() => setZoom((z) => clampZoom(z + ZOOM_STEP))}
            disabled={disabled}
            aria-label={t(($) => $.avatar_crop.zoom_in)}
            className="shrink-0 text-muted-foreground transition-colors hover:text-foreground disabled:opacity-40"
          >
            <Plus className="h-4 w-4" />
          </button>
        </div>

        <div className="flex items-center justify-between">
          <Button variant="ghost" onClick={resetTransform} disabled={busy}>
            {t(($) => $.avatar_crop.reset)}
          </Button>
          <div className="flex gap-2">
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
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
