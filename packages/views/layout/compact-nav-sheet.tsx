"use client";

import { useEffect } from "react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { useSidebar } from "@multica/ui/components/ui/sidebar";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { AppRail } from "./app-rail";
import { useAppShell } from "./app-shell-context";
import { SpacePanelContent } from "./space-panel";

/**
 * Below `md` the rail and the current space's panel move into one sheet,
 * opened by the page header's nav trigger.
 */
export function CompactNavSheet({ globalActions = true }: { globalActions?: boolean }) {
  const { t } = useT("layout");
  const { isCompact, openMobile, setOpenMobile } = useSidebar();
  const { pathname } = useNavigation();
  const shell = useAppShell();

  // The sheet covers the page, so leaving through it has to dismiss it or
  // the destination renders underneath and the tap reads as nothing
  // happened. Keying on `pathname` covers every route out at once: rail
  // links, panel rows, pins, and the workspace switcher.
  useEffect(() => {
    setOpenMobile(false);
  }, [pathname, setOpenMobile]);

  if (!isCompact) return null;
  const hasPanel = !!shell?.hasPanel;

  return (
    <Sheet open={openMobile} onOpenChange={setOpenMobile}>
      <SheetContent
        side="left"
        showCloseButton={false}
        data-slot="compact-nav-sheet"
        className="flex-row gap-0 overflow-hidden bg-app-shell p-0 data-[side=left]:w-auto data-[side=left]:max-w-[calc(100vw-3rem)]"
      >
        <SheetHeader className="sr-only">
          <SheetTitle>{t(($) => $.rail.label)}</SheetTitle>
          <SheetDescription>{t(($) => $.rail.sheet_description)}</SheetDescription>
        </SheetHeader>
        <AppRail globalActions={globalActions} />
        {hasPanel && (
          <div className="my-2 mr-2 flex w-[260px] min-w-0 flex-col overflow-hidden rounded-xl bg-page-canvas shadow-[var(--surface-shadow)] ring-1 ring-surface-border">
            <SpacePanelContent space={shell?.space ?? null} />
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}
