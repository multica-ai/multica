"use client";

import { useEffect, useRef, useState } from "react";
import { Loader2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import {
  TEXT_AVATAR_COLORS,
  TEXT_AVATAR_DEFAULT_COLOR,
  defaultAvatarText,
  normalizeAvatarText,
  renderTextAvatar,
  textAvatarFontRatio,
  textAvatarTextColor,
} from "./text-avatar";

/** Sizes shown side by side: the settings avatar and the sidebar list. */
const PREVIEW_SIZES = [64, 20] as const;

interface TextAvatarDialogProps {
  open: boolean;
  /** Name the starting text is taken from (its first character). */
  name: string;
  /** Parent's upload/save is in flight — locks the controls and blocks close. */
  busy?: boolean;
  onOpenChange: (open: boolean) => void;
  /** Fires with the generated PNG. The parent uploads + persists. */
  onGenerated: (file: File) => void;
}

function TextAvatarPreview({
  text,
  color,
  size,
}: {
  text: string;
  color: string;
  size: number;
}) {
  return (
    <span
      aria-hidden
      className="flex shrink-0 select-none items-center justify-center overflow-hidden rounded-full font-semibold leading-none"
      style={{
        width: size,
        height: size,
        backgroundColor: color,
        color: textAvatarTextColor(color),
        fontSize: size * textAvatarFontRatio(text),
      }}
    >
      {text}
    </span>
  );
}

/**
 * Builds an avatar from typed text: 1–2 characters on a preset color,
 * with live previews at the sizes it is shown at. Save draws a PNG (see
 * text-avatar.ts) and hands it to the parent, which uploads it exactly like a
 * cropped image.
 */
export function TextAvatarDialog({
  open,
  name,
  busy = false,
  onOpenChange,
  onGenerated,
}: TextAvatarDialogProps) {
  const { t } = useT("common");
  const [text, setText] = useState("");
  const [color, setColor] = useState<string>(TEXT_AVATAR_DEFAULT_COLOR);
  const [encoding, setEncoding] = useState(false);
  const [failed, setFailed] = useState(false);
  const encodingRef = useRef(false);
  const composingRef = useRef(false);
  const pending = busy || encoding;

  // Every open starts from the name's first character; the color carries over
  // so a second try only needs the text changed.
  useEffect(() => {
    if (!open) return;
    setText(defaultAvatarText(name));
    setFailed(false);
  }, [open, name]);

  const value = normalizeAvatarText(text);
  const canSave = !!value && !pending;

  // Cut to the limit as the user types, but not mid-composition: an IME's
  // pinyin/romaji buffer is longer than the characters it ends up as.
  const handleTextChange = (next: string) => {
    setFailed(false);
    setText(composingRef.current ? next : normalizeAvatarText(next));
  };

  const handleSave = async () => {
    if (!value || busy || encodingRef.current) return;
    encodingRef.current = true;
    setEncoding(true);
    try {
      const file = await renderTextAvatar(value, color);
      onGenerated(file);
    } catch {
      setFailed(true);
    } finally {
      encodingRef.current = false;
      setEncoding(false);
    }
  };

  const handleOpenChange = (next: boolean) => {
    if (busy || encodingRef.current) return;
    onOpenChange(next);
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent showCloseButton={!pending} className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>{t(($) => $.text_avatar.title)}</DialogTitle>
        </DialogHeader>

        <div
          className="flex items-center justify-center gap-4 rounded-lg bg-muted py-5"
          role="img"
          aria-label={t(($) => $.text_avatar.preview)}
        >
          {PREVIEW_SIZES.map((size) => (
            <TextAvatarPreview key={size} text={value} color={color} size={size} />
          ))}
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="text-avatar-input" className="text-body font-medium">
            {t(($) => $.text_avatar.text_label)}
          </label>
          <Input
            id="text-avatar-input"
            autoComplete="off"
            autoFocus
            value={text}
            disabled={pending}
            onCompositionStart={() => {
              composingRef.current = true;
            }}
            onCompositionEnd={(event) => {
              composingRef.current = false;
              handleTextChange(event.currentTarget.value);
            }}
            onChange={(event) => handleTextChange(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.nativeEvent.isComposing) {
                event.preventDefault();
                void handleSave();
              }
            }}
          />
          <p className="text-caption text-muted-foreground">
            {t(($) => $.text_avatar.text_hint)}
          </p>
        </div>

        <div className="flex flex-col gap-1.5">
          <p className="text-body font-medium">{t(($) => $.text_avatar.color_label)}</p>
          <div className="grid grid-cols-5 gap-2">
            {TEXT_AVATAR_COLORS.map((preset) => (
              <button
                key={preset}
                type="button"
                aria-label={preset}
                aria-pressed={color === preset}
                disabled={pending}
                onClick={() => setColor(preset)}
                className={cn(
                  // eslint-disable-next-line no-restricted-syntax -- color swatch
                  "size-7 justify-self-center rounded-full transition-transform hover:scale-110 disabled:opacity-50",
                  color === preset &&
                    "ring-2 ring-ring ring-offset-2 ring-offset-background",
                )}
                style={{ backgroundColor: preset }}
              />
            ))}
          </div>
        </div>

        {failed && (
          <p className="text-caption text-destructive">
            {t(($) => $.text_avatar.render_failed)}
          </p>
        )}

        <div className="flex justify-end gap-2">
          <Button
            variant="outline"
            onClick={() => handleOpenChange(false)}
            disabled={pending}
          >
            {t(($) => $.avatar_crop.cancel)}
          </Button>
          <Button onClick={handleSave} disabled={!canSave} aria-busy={pending}>
            {pending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            {busy
              ? t(($) => $.avatar_crop.uploading)
              : t(($) => $.avatar_crop.apply)}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
