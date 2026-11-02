"use client";

import { DOWNLOAD_PAGE_URL } from "@multica/core/deployment";

import { useState } from "react";
import {
  ArrowUpRight,
  CircleHelp,
  Download,
  MessageCircle,
  MessagesSquare as QrCode,
} from "@multica/ui/icons";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { useModalStore } from "@multica/core/modals";
import { useConfigStore } from "@multica/core/config";
import { isDesktopShell } from "../platform/local-directory";
import { FeishuQrDialog } from "./feishu-group-qr-dialog";
import { useT } from "../i18n";

export function HelpLauncher() {
  const { t } = useT("layout");
  const { t: tLark } = useT("lark");
  const serverVersion = useConfigStore((state) => state.serverVersion);
  const [qrOpen, setQrOpen] = useState(false);
  // Web-only: offering "download the desktop app" inside the desktop app is
  // nonsense, and this sidebar is shared — apps/desktop renders the same
  // AppSidebar as the web dashboard, so the entry has to be gated here.
  //
  // No `mounted` deferral (cf. browser-notification-setting.tsx): the desktop
  // renderer is a locally-bundled SPA with no SSR pass, and on web
  // `isDesktopShell()` is false both on the server and after hydration. The
  // markup matches either way, so the link can ship in the SSR payload instead
  // of popping in a frame late.
  const desktop = isDesktopShell();
  return (
    <>
      <DropdownMenu>
        <TooltipHint label={t(($) => $.help.trigger)}>
        <DropdownMenuTrigger
        aria-label={t(($) => $.help.trigger)}
        className="inline-flex size-7 items-center justify-center rounded-full text-muted-foreground transition-colors cursor-pointer hover:bg-accent hover:text-foreground data-popup-open:bg-accent data-popup-open:text-foreground"
      >
        <CircleHelp className="size-4" />
      </DropdownMenuTrigger>
        </TooltipHint>
        <DropdownMenuContent
          align="end"
          side="top"
          sideOffset={8}
          className="min-w-40 max-w-56"
        >
          {!desktop && (
            <>
              <DropdownMenuItem
                render={
                  <a
                    href={DOWNLOAD_PAGE_URL}
                    target="_blank"
                    rel="noopener noreferrer"
                  />
                }
              >
                <Download className="h-3.5 w-3.5" />
                {t(($) => $.help.download_desktop)}
                <ArrowUpRight className="size-3 translate-y-px text-faint-foreground" />
              </DropdownMenuItem>
              <DropdownMenuSeparator />
            </>
          )}
          <DropdownMenuItem onClick={() => setQrOpen(true)}>
            <QrCode className="h-3.5 w-3.5" />
            {tLark(($) => $.help.discord)}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => useModalStore.getState().open("feedback")}
          >
            <MessageCircle className="h-3.5 w-3.5" />
            {t(($) => $.help.feedback)}
          </DropdownMenuItem>
          {serverVersion && (
            <>
              <DropdownMenuSeparator />
              {/* DropdownMenuLabel renders Base UI's Menu.GroupLabel, which reads
                  a Menu.Group context and throws if it has no Group ancestor. It
                  must always be wrapped in a DropdownMenuGroup — without it the
                  Help menu crashes the whole app on open (no error boundary sits
                  above the sidebar). */}
              <DropdownMenuGroup>
                <DropdownMenuLabel className="font-normal break-words">
                  {t(($) => $.help.server_version, { version: serverVersion })}
                </DropdownMenuLabel>
              </DropdownMenuGroup>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      {/* Outside the dropdown: the dialog portals to document.body, and a
          trigger inside closed menu content would unmount with it. */}
      <FeishuQrDialog open={qrOpen} onOpenChange={setQrOpen} />
    </>
  );
}
