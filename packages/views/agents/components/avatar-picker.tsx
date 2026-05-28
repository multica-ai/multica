"use client";

import { useRef, useState, useCallback } from "react";
import { Camera, ImagePlus, Loader2, X, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import ReactNiceAvatar, { genConfig } from "react-nice-avatar";
import domtoimage from "dom-to-image";
import { api } from "@multica/core/api";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
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
type Mode = "upload" | "generate";

/**
 * Compact avatar picker with upload/generate tab switch.
 * - Upload mode: file input → upload to server → URL stored
 * - Generate mode: react-nice-avatar → canvas → blob → upload → URL stored
 */
export function AvatarPicker({ value, onChange, size = 56 }: AvatarPickerProps) {
  const { t } = useT("agents");
  const fileInputRef = useRef<HTMLInputElement>(null);
  const avatarRef = useRef<HTMLDivElement>(null);
  const { upload, uploading } = useFileUpload(api);
  const [previewError, setPreviewError] = useState(false);
  const [mode, setMode] = useState<Mode>("upload");
  const [avatarConfig, setAvatarConfig] = useState(() => genConfig());
  const [generating, setGenerating] = useState(false);

  const handleFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    e.target.value = "";
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

  const uploadGeneratedAvatar = useCallback(async () => {
    if (!avatarRef.current) return;
    setGenerating(true);
    try {
      const blob = await domtoimage.toBlob(avatarRef.current, {
        width: size * 2,
        height: size * 2,
        style: {
          width: `${size * 2}px`,
          height: `${size * 2}px`,
        },
      });
      if (!blob) return;
      const file = new File([blob], "avatar.png", { type: "image/png" });
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
    } finally {
      setGenerating(false);
    }
  }, [avatarRef, size, upload, onChange, t]);

  const handleRegenerate = () => {
    setAvatarConfig({ ...genConfig() });
  };

  const hasValue = !!value && !previewError;
  const dimensionStyle = { width: size, height: size };

  return (
    <div className="relative shrink-0" style={dimensionStyle}>
      {/* Mode tabs */}
      <div className="absolute -top-6 left-0 z-10 flex gap-1">
        <button
          type="button"
          onClick={() => setMode("upload")}
          className={cn(
            "flex items-center gap-1 rounded-t-xs border border-b-0 px-2 py-0.5 text-xs transition-colors",
            mode === "upload"
              ? "border-input bg-background text-foreground"
              : "border-transparent bg-muted/50 text-muted-foreground hover:bg-muted",
          )}
        >
          <ImagePlus className="h-3 w-3" />
          {t(($) => $.create_dialog.avatar.tab_upload ?? "Upload")}
        </button>
        <button
          type="button"
          onClick={() => setMode("generate")}
          className={cn(
            "flex items-center gap-1 rounded-t-xs border border-b-0 px-2 py-0.5 text-xs transition-colors",
            mode === "generate"
              ? "border-input bg-background text-foreground"
              : "border-transparent bg-muted/50 text-muted-foreground hover:bg-muted",
          )}
        >
          <RefreshCw className="h-3 w-3" />
          {t(($) => $.create_dialog.avatar.tab_generate ?? "Generate")}
        </button>
      </div>

      {/* Upload mode */}
      {mode === "upload" && (
        <button
          type="button"
          onClick={() => fileInputRef.current?.click()}
          disabled={uploading}
          className={cn(
            "group relative h-full w-full overflow-hidden rounded-lg outline-none transition-colors",
            "focus-visible:ring-2 focus-visible:ring-ring",
            hasValue
              ? "border"
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
              src={resolvePublicFileUrl(value) ?? undefined}
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
      )}

      {/* Generate mode */}
      {mode === "generate" && (
        <div className="relative h-full w-full overflow-hidden rounded-lg border">
          {/* Hidden avatar for canvas capture */}
          <div
            ref={avatarRef}
            className="absolute left-0 top-0 overflow-hidden"
            style={{ width: size * 2, height: size * 2 }}
          >
            <ReactNiceAvatar {...avatarConfig} style={{ width: size * 2, height: size * 2 }} shape="rounded" />
          </div>

          {/* Visible preview */}
          <div
            className="h-full w-full cursor-pointer"
            onClick={uploadGeneratedAvatar}
          >
            <ReactNiceAvatar {...avatarConfig} style={{ width: size, height: size }} shape="rounded" />
          </div>

          {/* Hover overlay */}
          <div className="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity group-hover:opacity-100">
            {generating ? (
              <Loader2 className="h-4 w-4 animate-spin text-white" />
            ) : (
              <Camera className="h-4 w-4 text-white" />
            )}
          </div>

          {/* Regenerate button */}
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              handleRegenerate();
            }}
            className="absolute -right-1.5 -top-1.5 flex h-5 w-5 items-center justify-center rounded-full border bg-background text-muted-foreground shadow-sm transition-colors hover:bg-muted hover:text-foreground"
            aria-label={t(($) => $.create_dialog.avatar.regenerate_aria)}
          >
            <RefreshCw className="h-3 w-3" />
          </button>
        </div>
      )}

      {/* Clear button for upload mode */}
      {hasValue && mode === "upload" && !uploading && (
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
