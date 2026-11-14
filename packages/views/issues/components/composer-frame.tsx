"use client";

import { useState, type FocusEvent, type MouseEvent, type ReactNode } from "react";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { cn } from "@multica/ui/lib/utils";
import { VoiceMicButton, VoiceStrip, type ComposerVoice } from "../../voice";

/**
 * Shared by the issue comment box and thread replies: one line with attach
 * and mic at rest, a toolbar row with recipients and send once focused or
 * written in.
 */

/** Send stays a light tint until there is something to send. */
export const QUIET_SEND_CLASS =
  "disabled:opacity-100 disabled:bg-muted disabled:text-faint-foreground aria-disabled:opacity-100 aria-disabled:bg-muted aria-disabled:text-faint-foreground";

const IDLE_TOOLS = "data-composer-idle-tools";

/**
 * Whether focus is inside the box. Focus on the resting attach and mic does
 * not count: expanding would unmount the button before its click lands.
 */
export function useComposerFocus() {
  const [focused, setFocused] = useState(false);
  return {
    focused,
    focusProps: {
      onFocus: (event: FocusEvent<HTMLElement>) => {
        if (!(event.target as HTMLElement).closest(`[${IDLE_TOOLS}]`)) setFocused(true);
      },
      onBlur: (event: FocusEvent<HTMLElement>) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setFocused(false);
      },
    },
  };
}

/**
 * Whether the box shows its toolbar. Frozen while the mic is held: dictation
 * focuses the editor, and the pressed mic must not unmount before release.
 */
export function useComposerExpanded(wanted: boolean, voice: ComposerVoice): boolean {
  const [settled, setSettled] = useState(wanted);
  if (!voice.holding && settled !== wanted) setSettled(wanted);
  return voice.holding ? settled : wanted;
}

/** Attach, then mic: the mic sits next to send, where its recording strip grows from. */
export function ComposerTools({ voice, onAttach }: { voice: ComposerVoice; onAttach: (file: File) => void }) {
  return (
    <>
      <FileUploadButton multiple onSelect={onAttach} />
      <VoiceMicButton voice={voice} />
    </>
  );
}

/** The tools at the end of the one-line box at rest. */
export function ComposerIdleTools({
  voice,
  onAttach,
  className,
}: {
  voice: ComposerVoice;
  onAttach: (file: File) => void;
  className?: string;
}) {
  return (
    <div {...{ [IDLE_TOOLS]: "" }} className={cn("flex shrink-0 items-center gap-1", className)}>
      <ComposerTools voice={voice} onAttach={onAttach} />
    </div>
  );
}

/** A click on the toolbar's empty space must not take focus out of the box and fold it. */
function keepFocus(event: MouseEvent<HTMLElement>) {
  if (!(event.target as HTMLElement).closest("button, a, input, [role='button']")) event.preventDefault();
}

/**
 * The expanded box's bottom row. Recipients keep their place on the left
 * while dictating; the strip takes the rest of the row instead of covering
 * them.
 */
export function ComposerToolbar({
  voice,
  recipients,
  hasRecipients,
  tools,
  send,
  className,
}: {
  voice: ComposerVoice;
  recipients: ReactNode;
  hasRecipients: boolean;
  tools: ReactNode;
  send: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn("flex h-9 items-center gap-1", className)}
      onMouseDown={keepFocus}
    >
      {hasRecipients && (
        <div className={cn("min-w-0 overflow-hidden", voice.showStrip ? "max-w-[45%] shrink-0" : "flex-1")}>
          {recipients}
        </div>
      )}
      {voice.showStrip ? (
        <VoiceStrip voice={voice} />
      ) : (
        <>
          {!hasRecipients && <div className="flex-1" />}
          {tools}
          {send}
        </>
      )}
    </div>
  );
}
