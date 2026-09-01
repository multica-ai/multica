"use client";

import { useEffect, useRef, useState } from "react";
import { LoaderCircle, Mic } from "lucide-react";
import { toast } from "sonner";
import type { DictationAdapter } from "@multica/core/types/dictation";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import type { VoiceInputButtonProps } from "./voice-input-button";

export function NativeDictationButton({
  adapter,
  editorRef,
  disabled = false,
  className,
  size = "default",
  onBeforeRecord,
}: VoiceInputButtonProps & { adapter: DictationAdapter }) {
  const { t } = useT("editor");
  const [pending, setPending] = useState(false);
  const pendingRef = useRef(false);
  const mountedRef = useRef(true);
  const disabledRef = useRef(disabled);
  disabledRef.current = disabled;

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  const toggle = async () => {
    if (pendingRef.current || disabledRef.current) return;
    pendingRef.current = true;
    setPending(true);
    try {
      onBeforeRecord?.();
      // Readonly-first composers mount Tiptap after this click. Wait only for
      // the live visible editor, and fail closed if it unmounts or stays hidden.
      const deadline = Date.now() + 1500;
      let focused = false;
      while (mountedRef.current && !disabledRef.current && Date.now() < deadline) {
        if (editorRef.current?.focusForNativeInput()) {
          focused = true;
          break;
        }
        await new Promise<void>((resolve) => setTimeout(resolve, 16));
      }
      if (!mountedRef.current || disabledRef.current) return;
      if (!focused) {
        toast.error(t(($) => $.desktop.dictation.editor_not_ready));
        return;
      }

      const result = await adapter.toggle();
      if (!mountedRef.current) return;
      if (result.ok) {
        toast.info(t(($) => $.desktop.dictation.sent, { shortcut: result.shortcut }));
      } else {
        switch (result.reason) {
          case "not_configured":
            toast.error(t(($) => $.desktop.dictation.not_configured));
            break;
          case "app_not_running":
            toast.error(t(($) => $.desktop.dictation.app_not_running));
            break;
          case "not_focused":
            toast.error(t(($) => $.desktop.dictation.not_focused));
            break;
          case "busy":
            toast.error(t(($) => $.desktop.dictation.busy));
            break;
          case "cleanup_failed":
            toast.error(t(($) => $.desktop.dictation.cleanup_failed));
            break;
          default:
            toast.error(t(($) => $.desktop.dictation.unavailable));
        }
      }
    } catch {
      if (mountedRef.current) toast.error(t(($) => $.desktop.dictation.unavailable));
    } finally {
      pendingRef.current = false;
      if (mountedRef.current) setPending(false);
    }
  };

  const label = pending
    ? t(($) => $.desktop.dictation.opening)
    : t(($) => $.desktop.dictation.toggle);
  const iconClassName = size === "sm" ? "size-3.5" : "size-4";
  return (
    <Button
      type="button"
      data-native-dictation=""
      variant="ghost"
      size={size === "sm" ? "icon-xs" : "icon-sm"}
      aria-label={label}
      aria-busy={pending || undefined}
      title={label}
      disabled={disabled || pending}
      onPointerDown={(event) => event.preventDefault()}
      onKeyDown={(event) => {
        if (event.key !== "Enter") return;
        // Native buttons click on Enter keydown; the helper correctly refuses
        // a chord while Enter is still held. Dispatch only after its release.
        event.preventDefault();
        event.stopPropagation();
      }}
      onKeyUp={(event) => {
        if (event.key !== "Enter") return;
        event.preventDefault();
        event.stopPropagation();
        void toggle();
      }}
      onClick={() => void toggle()}
      className={cn("text-muted-foreground", className)}
    >
      {pending ? (
        <LoaderCircle className={cn(iconClassName, "animate-spin")} />
      ) : (
        <Mic className={iconClassName} />
      )}
    </Button>
  );
}
