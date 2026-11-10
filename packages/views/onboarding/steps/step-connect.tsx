"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { ChevronDown, Download, Loader2, Terminal } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import type { AgentRuntime } from "@multica/core/types";
import { useT } from "../../i18n";
import { StepFooter, StepHeading } from "../components/step-shell";

/** The /download page owns platform detection and the release links. */
const DOWNLOAD_PAGE_URL = "/download";

/**
 * Web only: agents run on a computer the member owns, and a browser is not
 * one. Desktop bundles the daemon; the terminal path installs the CLI and
 * runs `multica setup`. Either way the computer registers on its own and this
 * step moves on by itself.
 */
export function StepConnect({
  runtimes,
  cliInstructions,
  onNext,
  onSkip,
}: {
  runtimes: AgentRuntime[];
  cliInstructions: ReactNode;
  onNext: () => void;
  onSkip: () => Promise<void>;
}) {
  const { t } = useT("onboarding");
  const [terminalOpen, setTerminalOpen] = useState(false);
  const [skipping, setSkipping] = useState(false);
  const connected = runtimes.length > 0;

  // Only advance on a computer that connected while this step was on screen;
  // someone who came Back here from the tool step chose to be here.
  const connectedOnEntry = useRef(connected);
  useEffect(() => {
    if (connected && !connectedOnEntry.current) onNext();
  }, [connected, onNext]);

  const skip = async () => {
    setSkipping(true);
    try {
      await onSkip();
    } finally {
      setSkipping(false);
    }
  };

  return (
    <>
      <div className="flex flex-col gap-6 pt-6">
        <StepHeading
          title={t(($) => $.step_connect.title)}
          description={t(($) => $.step_connect.description)}
        />

        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-4 rounded-lg border bg-card px-4 py-4">
            <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-foreground text-background">
              <Download className="size-4" />
            </span>
            <div className="min-w-0 flex-1">
              <div className="text-body font-medium text-foreground">
                {t(($) => $.step_connect.desktop_title)}
              </div>
              <div className="text-caption text-muted-foreground">
                {t(($) => $.step_connect.desktop_body)}
              </div>
            </div>
            <Button
              size="sm"
              render={<a href={DOWNLOAD_PAGE_URL} target="_blank" rel="noreferrer" />}
              nativeButton={false}
            >
              {t(($) => $.step_connect.desktop_action)}
            </Button>
          </div>

          <div className="rounded-lg border bg-card">
            <button
              type="button"
              onClick={() => setTerminalOpen((open) => !open)}
              aria-expanded={terminalOpen}
              className="flex w-full items-center gap-4 rounded-lg px-4 py-4 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <span className="flex size-8 shrink-0 items-center justify-center rounded-md border">
                <Terminal className="size-4" />
              </span>
              <div className="min-w-0 flex-1">
                <div className="text-body font-medium text-foreground">
                  {t(($) => $.step_connect.terminal_title)}
                </div>
                <div className="text-caption text-muted-foreground">
                  {t(($) => $.step_connect.terminal_body)}
                </div>
              </div>
              <ChevronDown
                className={cn(
                  "size-4 shrink-0 text-muted-foreground transition-transform",
                  terminalOpen && "rotate-180",
                )}
              />
            </button>
            {terminalOpen ? <div className="border-t p-3">{cliInstructions}</div> : null}
          </div>
        </div>
      </div>

      <StepFooter
        hint={
          connected ? (
            t(($) => $.step_connect.connected)
          ) : (
            <span className="inline-flex items-center gap-2">
              <Loader2 className="size-3.5 animate-spin" />
              {t(($) => $.step_connect.waiting)}
            </span>
          )
        }
      >
        <Button className="w-full" disabled={!connected} onClick={onNext}>
          {t(($) => $.common.continue)}
        </Button>
        <Button
          variant="ghost"
          className="w-full text-muted-foreground"
          disabled={skipping}
          aria-busy={skipping}
          onClick={skip}
        >
          {skipping ? <Loader2 className="animate-spin" /> : null}
          {t(($) => $.step_connect.skip)}
        </Button>
      </StepFooter>
    </>
  );
}
