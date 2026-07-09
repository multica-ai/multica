"use client";

import { useRef, useState } from "react";
import { Bot, Camera, Loader2, Users, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { AvatarCropDialog, type AvatarCropShape } from "./avatar-crop-dialog";

export type AvatarUploadVariant = "user" | "agent" | "squad" | "workspace";

interface AvatarUploadControlProps {
  /** Current avatar URL, raw (unresolved). `null` renders the empty state. */
  value: string | null;
  /** Drives display shape and the empty-state fallback icon/initials. */
  variant: AvatarUploadVariant;
  /** Name used for initials / first-letter fallback and the image alt. */
  name?: string;
  /** Pixel side of the square/circle. Defaults to 64. */
  size?: number;
  disabled?: boolean;
  /**
   * Fires with the uploaded file URL after a successful crop + upload. The
   * parent persists it (updateMe / updateWorkspace / updateAgent /
   * updateSquad, or stashing it for a create call). The crop dialog stays in
   * its busy state until this resolves, then closes.
   */
  onUploaded: (url: string) => void | Promise<unknown>;
  /**
   * When provided, shows a small clear affordance. Used by create flows to
   * drop a not-yet-persisted choice; edit flows omit it (removing a saved
   * avatar is out of scope).
   */
  onClear?: () => void;
  className?: string;
  ariaLabel?: string;
}

// Humans read as a circle; non-human actors (agent, squad, workspace) get a
// rounded square so they don't read as a single person. Matches the shared
// display avatar (packages/ui/components/common/actor-avatar.tsx).
const VARIANT_SHAPE: Record<AvatarUploadVariant, AvatarCropShape> = {
  user: "circle",
  agent: "square",
  squad: "square",
  workspace: "square",
};

function initialsOf(name: string): string {
  return name
    .split(" ")
    .map((word) => word[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
}

function AvatarFallback({
  variant,
  name,
  size,
}: {
  variant: AvatarUploadVariant;
  name: string;
  size: number;
}) {
  if (variant === "agent") {
    return <Bot style={{ width: size * 0.5, height: size * 0.5 }} />;
  }
  if (variant === "squad") {
    return <Users style={{ width: size * 0.5, height: size * 0.5 }} />;
  }
  const text =
    variant === "workspace"
      ? name.charAt(0).toUpperCase()
      : initialsOf(name);
  return (
    <span className="font-semibold" style={{ fontSize: size * 0.4 }}>
      {text}
    </span>
  );
}

/**
 * Shared click-to-upload avatar control for web/desktop. Renders the current
 * avatar with a hover "change" affordance; on pick it opens {@link
 * AvatarCropDialog} for reposition/zoom, then uploads the cropped image
 * through the existing `/api/upload-file` chain and hands the URL back via
 * `onUploaded`. Business persistence stays with the caller.
 */
export function AvatarUploadControl({
  value,
  variant,
  name = "",
  size = 64,
  disabled = false,
  onUploaded,
  onClear,
  className,
  ariaLabel,
}: AvatarUploadControlProps) {
  const { t } = useT("common");
  const { upload } = useFileUpload(api);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [pickedFile, setPickedFile] = useState<File | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [previewError, setPreviewError] = useState(false);

  const resolved = value ? resolvePublicFileUrl(value) : null;
  const hasImage = !!resolved && !previewError;
  const shape = VARIANT_SHAPE[variant];
  const radiusClass = shape === "circle" ? "rounded-full" : "rounded-lg";

  const handlePick = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = ""; // allow re-selecting the same file
    if (!file) return;
    if (!file.type.startsWith("image/")) {
      toast.error(t(($) => $.avatar_upload.select_image));
      return;
    }
    setPreviewError(false);
    setPickedFile(file);
    setDialogOpen(true);
  };

  const handleCropped = async (cropped: File) => {
    setBusy(true);
    try {
      const result = await upload(cropped);
      if (!result) return;
      await onUploaded(result.link);
      setDialogOpen(false);
      setPickedFile(null);
      toast.success(t(($) => $.avatar_upload.updated));
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : t(($) => $.avatar_upload.failed),
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="relative shrink-0" style={{ width: size, height: size }}>
      <button
        type="button"
        onClick={() => fileInputRef.current?.click()}
        disabled={disabled || busy}
        aria-label={ariaLabel ?? t(($) => $.avatar_upload.change)}
        className={cn(
          "group relative h-full w-full overflow-hidden bg-muted text-muted-foreground outline-none",
          "flex items-center justify-center",
          "focus-visible:ring-2 focus-visible:ring-ring",
          "disabled:cursor-not-allowed disabled:opacity-60",
          radiusClass,
          className,
        )}
        style={{ width: size, height: size }}
      >
        {hasImage ? (
          <img
            src={resolved ?? undefined}
            alt={name}
            className="h-full w-full object-cover"
            onError={() => setPreviewError(true)}
          />
        ) : (
          <AvatarFallback variant={variant} name={name} size={size} />
        )}

        {!disabled && (
          <div className="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity group-hover:opacity-100">
            {busy ? (
              <Loader2 className="h-5 w-5 animate-spin text-white" />
            ) : (
              <Camera className="h-5 w-5 text-white" />
            )}
          </div>
        )}
      </button>

      {onClear && hasImage && !busy && !disabled && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            setPreviewError(false);
            onClear();
          }}
          className="absolute -right-1.5 -top-1.5 flex h-5 w-5 items-center justify-center rounded-full border bg-background text-muted-foreground shadow-sm transition-colors hover:bg-muted hover:text-foreground"
          aria-label={t(($) => $.avatar_upload.remove)}
        >
          <X className="h-3 w-3" />
        </button>
      )}

      <input
        ref={fileInputRef}
        type="file"
        accept="image/*"
        className="hidden"
        onChange={handlePick}
      />

      <AvatarCropDialog
        file={pickedFile}
        open={dialogOpen}
        shape={shape}
        busy={busy}
        onOpenChange={(next) => {
          if (busy) return;
          setDialogOpen(next);
          if (!next) setPickedFile(null);
        }}
        onCropped={handleCropped}
      />
    </div>
  );
}
