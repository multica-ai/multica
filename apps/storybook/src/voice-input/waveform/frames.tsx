import type { ComponentType } from "react";
import { ArrowUp, Check, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Kbd } from "@multica/ui/components/ui/kbd";
import { cn } from "@multica/ui/lib/utils";
import { ComposerSurface } from "../chat-frame";
import { useStoryEnv } from "../env";
import { RecDot, formatElapsed } from "../primitives";
import { useLevelSamples } from "./stream";
import type { WaveProps } from "./variants";

function Elapsed({ className }: { className?: string }) {
  const { elapsedMs } = useLevelSamples();
  return <span className={cn("text-caption tabular-nums", className)}>{formatElapsed(elapsedMs)}</span>;
}

const DRAFT = {
  zh: "帮我看一下这个 PR，重点关注",
  en: "Take a look at this PR, especially",
};

/** The composer's recording strip as it ships: cancel, timer, waveform, insert, send. */
export function StripFrame({ Wave, className }: { Wave: ComponentType<WaveProps>; className?: string }) {
  const { lang, copy } = useStoryEnv();
  return (
    <div className={cn("w-[560px]", className)}>
      <ComposerSurface
        active
        bottom={
          <>
            <IconButton label={copy.cancel} shortcut={<Kbd>Esc</Kbd>} className="text-muted-foreground">
              <X />
            </IconButton>
            <span className="flex shrink-0 items-center gap-1.5 pl-1 pr-2">
              <RecDot />
              <Elapsed className="text-muted-foreground" />
            </span>
            <div className="min-w-0 flex-1 overflow-hidden text-foreground">
              <Wave density="strip" />
            </div>
            <IconButton label={copy.insert} className="ml-1 text-foreground">
              <Check />
            </IconButton>
            <Button
              size="icon-sm"
              // eslint-disable-next-line no-restricted-syntax -- composer send button, the one round button
              className="shrink-0 rounded-full"
              aria-label={copy.send}
            >
              <ArrowUp />
            </Button>
          </>
        }
      >
        <p className="text-body">{DRAFT[lang]}</p>
      </ComposerSurface>
    </div>
  );
}

/** The push-to-talk pill as it ships: always dark ink, 86 px of waveform. */
export function HudFrame({ Wave }: { Wave: ComponentType<WaveProps> }) {
  const { copy } = useStoryEnv();
  return (
    <div className="flex h-10 w-fit items-center gap-2.5 whitespace-nowrap rounded-full bg-multica-ink pl-3.5 pr-4 text-multica-paper shadow-[var(--floating-shadow)]">
      <RecDot className="bg-[#ff5a52]" />
      <div className="w-[86px]">
        <Wave density="hud" />
      </div>
      <Elapsed className="opacity-70" />
      <span className="text-caption opacity-70">{copy.releaseToInsert}</span>
    </div>
  );
}
