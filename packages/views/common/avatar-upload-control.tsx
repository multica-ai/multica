"use client";

import { Suspense, lazy, useRef, useState } from "react";
import { Bot, Camera, ImagePlus, Loader2, Users, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import {
  AVATAR_EMOJI_SUGGESTIONS,
  formatAvatarEmoji,
  parseAvatarEmoji,
} from "@multica/ui/lib/avatar-emoji";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Separator } from "@multica/ui/components/ui/separator";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { AvatarCropDialog } from "./avatar-crop-dialog";

// The full emoji-mart picker is ~1MB of emoji data. Only the handful of
// suggestions render eagerly; the searchable set loads when asked for.
const EmojiPicker = lazy(() =>
  import("@multica/ui/components/common/emoji-picker").then((m) => ({
    default: m.EmojiPicker,
  })),
);

export type AvatarUploadVariant = "user" | "agent" | "squad" | "workspace";

interface AvatarUploadControlProps {
  /** Current avatar URL, raw (unresolved). `null` renders the empty state. */
  value: string | null;
  /** Drives the empty-state fallback icon/initials. */
  variant: AvatarUploadVariant;
  /** Name used for initials / first-letter fallback and the image alt. */
  name?: string;
  /** Pixel diameter of the circle. Defaults to 64. */
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
   * When provided, the avatar offers emoji as an alternative to an image:
   * clicking it opens a picker with the suggested set, full emoji search, and
   * the upload entry. Fires with the value to persist (`emoji:🚀`) — the same
   * `avatar_url` shape as `onUploaded`, so the caller stores both the same
   * way. Omit it and the control keeps its click-straight-to-upload behavior.
   */
  onEmojiSelected?: (value: string) => void | Promise<unknown>;
  /**
   * When provided, shows a small clear affordance. Used by create flows to
   * drop a not-yet-persisted choice; edit flows omit it (removing a saved
   * avatar is out of scope).
   */
  onClear?: () => void;
  className?: string;
  ariaLabel?: string;
}

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
 * Runs the caller's persistence callback and reports whether it succeeded.
 *
 * A rejection is deliberately swallowed rather than toasted: the caller that
 * performed the write already owns that feedback. `AgentDetailPage.handleUpdate`
 * toasts and then rethrows so autosave can render a failed state, and the
 * settings tabs toast their own; the create flows only stash the value in
 * local state and cannot fail at all. Reporting it here too would show the
 * same failure twice. The upload this control runs itself has no other owner,
 * so that one is still announced.
 */
async function persistedByCaller(
  save: () => void | Promise<unknown>,
): Promise<boolean> {
  try {
    await save();
    return true;
  } catch {
    return false;
  }
}

/**
 * Shared click-to-upload avatar control for web/desktop. Renders the current
 * avatar with a hover "change" affordance; on pick it opens {@link
 * AvatarCropDialog} for reposition/zoom, then uploads the cropped image
 * through the existing `/api/upload-file` chain and hands the URL back via
 * `onUploaded`. With `onEmojiSelected` the click opens a picker that offers
 * an emoji instead — both paths produce an `avatar_url` value. Business
 * persistence stays with the caller.
 */
export function AvatarUploadControl({
  value,
  variant,
  name = "",
  size = 64,
  disabled = false,
  onUploaded,
  onEmojiSelected,
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
  const [pickerOpen, setPickerOpen] = useState(false);
  const [emojiSearchOpen, setEmojiSearchOpen] = useState(false);

  const emoji = parseAvatarEmoji(value);
  const resolved = value && !emoji ? resolvePublicFileUrl(value) : null;
  const hasImage = !!resolved && !previewError;
  const hasAvatar = !!emoji || hasImage;
  const emojiEnabled = !!onEmojiSelected;

  const openFileDialog = () => fileInputRef.current?.click();

  const closePicker = () => {
    setPickerOpen(false);
    setEmojiSearchOpen(false);
  };

  const handlePickerOpenChange = (next: boolean) => {
    setPickerOpen(next);
    if (!next) setEmojiSearchOpen(false);
  };

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
      if (!(await persistedByCaller(() => onUploaded(result.link)))) return;
      setDialogOpen(false);
      setPickedFile(null);
      toast.success(t(($) => $.avatar_upload.updated));
    } catch (err) {
      // Only the upload above can land here — see persistedByCaller.
      toast.error(
        err instanceof Error ? err.message : t(($) => $.avatar_upload.failed),
      );
    } finally {
      setBusy(false);
    }
  };

  // Busy for the whole save, same as an upload: an edit caller PATCHes on every
  // pick, so leaving the trigger live would let a second pick race the first.
  // Both writes are last-one-to-arrive-wins on the server, and the loser can be
  // the one the user chose last — a permanently wrong avatar, since the
  // invalidate that follows only converges on whatever the server kept.
  //
  // No success toast on purpose: the avatar swaps to the chosen emoji in place,
  // so the change is already visible.
  const handleEmojiSelected = async (picked: string) => {
    closePicker();
    setPreviewError(false);
    setBusy(true);
    try {
      await persistedByCaller(() =>
        onEmojiSelected?.(formatAvatarEmoji(picked)),
      );
    } finally {
      setBusy(false);
    }
  };

  const avatarButton = (
    <button
      type="button"
      // With emoji enabled this button is the popover trigger and Base UI
      // supplies the click handler; without it the click goes straight to the
      // file dialog, which is this control's original single-purpose shape.
      onClick={emojiEnabled ? undefined : openFileDialog}
      disabled={disabled || busy}
      aria-label={ariaLabel ?? t(($) => $.avatar_upload.change)}
      className={cn(
        "group relative h-full w-full overflow-hidden bg-muted text-muted-foreground outline-none",
        "flex items-center justify-center",
        "focus-visible:ring-2 focus-visible:ring-ring",
        "disabled:cursor-not-allowed disabled:opacity-60",
        "rounded-full",
        className,
      )}
      style={{ width: size, height: size }}
    >
      {emoji ? (
        <span
          role="img"
          aria-label={name}
          className="select-none leading-none"
          style={{ fontSize: size * 0.58 }}
        >
          {emoji}
        </span>
      ) : hasImage ? (
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
  );

  return (
    <div className="relative shrink-0" style={{ width: size, height: size }}>
      {emojiEnabled ? (
        <Popover open={pickerOpen} onOpenChange={handlePickerOpenChange}>
          <PopoverTrigger render={avatarButton} />
          <PopoverContent
            align="start"
            // The emoji-mart picker sizes itself; only the suggestion grid is
            // laid out against the popover's own width.
            className={cn("gap-0 p-0", emojiSearchOpen ? "w-auto" : "w-72")}
          >
            {emojiSearchOpen ? (
              <Suspense
                fallback={
                  <div className="p-4 text-body text-muted-foreground">
                    {t(($) => $.avatar_upload.emoji_loading)}
                  </div>
                }
              >
                <EmojiPicker onSelect={handleEmojiSelected} />
              </Suspense>
            ) : (
              <>
                <div className="p-1">
                  <button
                    type="button"
                    onClick={() => {
                      closePicker();
                      openFileDialog();
                    }}
                    className="flex w-full items-center gap-1.5 rounded-md px-1.5 py-1.5 text-body outline-hidden transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:bg-accent focus-visible:text-accent-foreground"
                  >
                    <ImagePlus className="size-4 shrink-0 text-muted-foreground" />
                    {t(($) => $.avatar_upload.upload_image)}
                  </button>
                </div>

                <Separator />

                <div className="p-1">
                  <p className="px-1.5 py-1 text-caption font-medium text-muted-foreground">
                    {t(($) => $.avatar_upload.emoji_label)}
                  </p>
                  <div className="grid grid-cols-8 gap-0.5">
                    {AVATAR_EMOJI_SUGGESTIONS.map((suggestion) => (
                      <button
                        key={suggestion}
                        type="button"
                        aria-pressed={emoji === suggestion}
                        onClick={() => handleEmojiSelected(suggestion)}
                        className={cn(
                          "flex h-8 w-8 items-center justify-center rounded-md text-title-sm leading-none outline-hidden transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring",
                          emoji === suggestion && "bg-accent ring-1 ring-ring",
                        )}
                      >
                        {suggestion}
                      </button>
                    ))}
                  </div>
                  <button
                    type="button"
                    onClick={() => setEmojiSearchOpen(true)}
                    className="mt-1 w-full rounded-md px-1.5 py-1 text-caption text-muted-foreground outline-hidden transition-colors hover:bg-accent hover:text-foreground focus-visible:bg-accent focus-visible:text-foreground"
                  >
                    {t(($) => $.avatar_upload.more_emojis)}
                  </button>
                </div>
              </>
            )}
          </PopoverContent>
        </Popover>
      ) : (
        avatarButton
      )}

      {onClear && hasAvatar && !busy && !disabled && (
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
