"use client";

import { useRef, useState } from "react";
import { Camera, ImagePlus, Loader2, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

interface AvatarPickerProps {
  /** Current avatar URL. null when nothing chosen yet. */
  value: string | null;
  /** Fires after a successful upload — the parent stashes the URL for the
   *  create call. Re-fires with null when the user clears the choice. */
  onChange: (url: string | null) => void;
  /** Pixel size of the square. Defaults to 56 (h-14 / w-14), which lines
   *  up vertically with the Name + Description stack in the create-agent
   *  form so the two read as a single visual row. */
  size?: number;
}

/**
 * Compact avatar picker — a single square that lives next to the Name
 * input in the create-agent form. Mirrors the visual language of
 * agent-detail-inspector.tsx (Camera overlay on hover, file input behind
 * the scenes), so users who've configured an avatar elsewhere in the app
 * recognise the affordance immediately.
 *
 * No avatar yet → dashed placeholder with an ImagePlus icon.
 * Has avatar    → image fills the square, hover dims it with a Camera
 *                 overlay for "click to change". A small × in the corner
 *                 clears the choice.
 */
export function AvatarPicker({ value, onChange, size = 56 }: AvatarPickerProps) {
  const { t } = useT("agents");
  const fileInputRef = useRef<HTMLInputElement>(null);
  const { upload, uploading } = useFileUpload(api);
  const [previewError, setPreviewError] = useState(false);

  const handleFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    e.target.value = ""; // allow re-selecting the same file
    if (!file.type.startsWith("image/")) {
      toast.error(t(($) => $.create_dialog.avatar.select_image_toast));
      return;
    }
    try {
      const result = await upload(file);
      if (!result) return;
      setPreviewError(false);
      onChange(result.link);
    } catch (err) {
      toast.error(
        err instanceof Error
          ? err.message
          : t(($) => $.create_dialog.avatar.upload_failed_toast),
      );
    }
  };

  const hasValue = !!value && !previewError;
  const dimensionStyle = { width: size, height: size };

  return (
    <div className="relative shrink-0" style={dimensionStyle}>
      <button
        type="button"
        onClick={() => fileInputRef.current?.click()}
        disabled={uploading}
        className={cn(
          "group relative h-full w-full overflow-hidden rounded-lg outline-none transition-colors",
          "focus-visible:ring-2 focus-visible:ring-ring",
          hasValue
            ? "border bg-muted"
            : "border border-dashed bg-muted/40 hover:bg-muted",
        )}
        aria-label={
          hasValue
            ? t(($) => $.create_dialog.avatar.change_aria)
            : t(($) => $.create_dialog.avatar.upload_aria)
        }
        style={dimensionStyle}
      >
        {hasValue ? (
          <img
            src={value ?? undefined}
            alt=""
            className="h-full w-full object-cover"
            onError={() => setPreviewError(true)}
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-muted-foreground">
            {uploading ? (
              <Loader2 className="h-5 w-5 animate-spin" />
            ) : (
              <ImagePlus className="h-5 w-5" />
            )}
          </div>
        )}

        {/* Hover overlay only when there's already an image — otherwise the
            placeholder icon already invites the click. */}
        {hasValue && (
          <div className="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity group-hover:opacity-100">
            {uploading ? (
              <Loader2 className="h-4 w-4 animate-spin text-white" />
            ) : (
              <Camera className="h-4 w-4 text-white" />
            )}
          </div>
        )}
      </button>

      {/* Tiny X to clear, only shown when there's a value. Positioned just
          outside the avatar's top-right corner so it doesn't cover the
          image. */}
      {hasValue && !uploading && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onChange(null);
            setPreviewError(false);
          }}
          className="absolute -right-1.5 -top-1.5 flex h-5 w-5 items-center justify-center rounded-full border bg-background text-muted-foreground shadow-sm transition-colors hover:bg-muted hover:text-foreground"
          aria-label={t(($) => $.create_dialog.avatar.remove_aria)}
        >
          <X className="h-3 w-3" />
        </button>
      )}

      <input
        ref={fileInputRef}
        type="file"
        accept="image/*"
        className="hidden"
        onChange={handleFile}
      />
    </div>
  );
}
