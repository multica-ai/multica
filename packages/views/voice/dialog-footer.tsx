"use client";

import type { ReactNode } from "react";
import type { ComposerVoice } from "./use-composer-voice";
import { VoiceSetupDialog } from "./voice-models";
import { VoiceErrorRow, VoiceHud, VoiceStrip } from "./voice-ui";

/**
 * A dialog footer that voice input can take over: the recording strip
 * replaces the footer row while dictating, and the push-to-talk pill floats
 * above it.
 */
export function VoiceDialogFooter({ voice, children }: { voice: ComposerVoice; children: ReactNode }) {
  return (
    <div className="relative shrink-0">
      <VoiceHud voice={voice} />
      <VoiceSetupDialog voice={voice} />
      <VoiceErrorRow voice={voice} className="mx-4 mb-2" />
      {voice.showStrip ? (
        <div className="flex items-center border-t px-4 py-3">
          <VoiceStrip voice={voice} />
        </div>
      ) : (
        children
      )}
    </div>
  );
}
