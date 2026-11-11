"use client";

import { useState } from "react";
import { ExternalLink, Loader2, RefreshCw } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { openExternal } from "../../platform";
import { useT } from "../../i18n";
import { CommandRow } from "../steps/cli-install-instructions";
import { toolDisplayName } from "../provisioning";
import type { InstallableTool } from "./tool-catalog";

/**
 * How to get a missing tool onto this computer: the vendor's install command,
 * the one sign-in it needs, and their guide for everything else. The picker
 * closes this the moment the tool registers, so "come back here" is literal.
 */
export function InstallToolDialog({
  tool,
  onOpenChange,
  canRescan,
  onRescan,
}: {
  tool: InstallableTool | null;
  onOpenChange: (open: boolean) => void;
  canRescan: boolean;
  onRescan: () => Promise<void>;
}) {
  const { t } = useT("onboarding");
  const [rescanning, setRescanning] = useState(false);
  // Keeps the content in place while the dialog animates closed.
  const [shown, setShown] = useState(tool);
  if (tool && tool !== shown) setShown(tool);

  const rescan = async () => {
    setRescanning(true);
    try {
      await onRescan();
    } finally {
      setRescanning(false);
    }
  };

  return (
    <Dialog open={tool !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {shown ? (
          <>
            <DialogHeader>
              <div className="flex items-center gap-3">
                <ToolTile provider={shown.provider} />
                <DialogTitle>
                  {t(($) => $.install.title, { name: toolDisplayName(shown.provider) })}
                </DialogTitle>
              </div>
              <DialogDescription>
                {t(($) => $.install.description, { vendor: shown.vendor })}
              </DialogDescription>
            </DialogHeader>
            <ol className="flex min-w-0 flex-col gap-4">
              <li className="flex flex-col gap-1.5">
                <span className="text-caption font-medium text-foreground">
                  {`1. ${t(($) => $.install.step_install)}`}
                </span>
                <CommandRow cmd={shown.installCommand} />
              </li>
              <li className="flex flex-col gap-1.5">
                <span className="text-caption font-medium text-foreground">
                  {`2. ${t(($) => $.install.step_sign_in, { vendor: shown.vendor })}`}
                </span>
                <CommandRow cmd={shown.signInCommand} />
              </li>
              {canRescan ? (
                <li className="text-caption text-muted-foreground">
                  {`3. ${t(($) => $.install.step_return_desktop)}`}
                </li>
              ) : (
                <li className="flex flex-col gap-1.5">
                  <span className="text-caption font-medium text-foreground">
                    {`3. ${t(($) => $.install.step_return_web)}`}
                  </span>
                  <CommandRow cmd="multica daemon restart" />
                </li>
              )}
            </ol>
            <DialogFooter className="sm:justify-between">
              <Button
                variant="ghost"
                className="text-muted-foreground"
                onClick={() => openExternal(shown.guideUrl)}
              >
                <ExternalLink />
                {t(($) => $.install.guide)}
              </Button>
              {canRescan ? (
                <Button
                  variant="outline"
                  onClick={rescan}
                  disabled={rescanning}
                  aria-busy={rescanning}
                >
                  {rescanning ? <Loader2 className="animate-spin" /> : <RefreshCw />}
                  {t(($) => $.install.rescan)}
                </Button>
              ) : null}
            </DialogFooter>
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

export function ToolTile({ provider }: { provider: string }) {
  return (
    <span className="flex size-8 shrink-0 items-center justify-center rounded-md border bg-card">
      <ProviderLogo provider={provider} className="size-4" />
    </span>
  );
}
