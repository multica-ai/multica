"use client";

import {
  ArrowUpRight,
  BookOpen,
  CircleHelp,
  Download,
  History,
  MessageCircle,
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
import { DISCORD_URL, DiscordIcon } from "./discord";
import { useT } from "../i18n";
import { docsLocalePrefix } from "../common/docs-locale";
import { RAIL_ICON_BUTTON_CLASS } from "./rail-styles";
import { CHANGELOG_URL, DOCS_URL, DOWNLOAD_URL } from "./help-links";


export function HelpLauncher() {
  const { t, i18n } = useT("layout");
  const serverVersion = useConfigStore((state) => state.serverVersion);
  // Web-only: offering "download the desktop app" inside the desktop app is
  // nonsense, and this rail is shared — apps/desktop renders the same app
  // shell as the web dashboard, so the entry has to be gated here.
  //
  // No `mounted` deferral (cf. browser-notification-setting.tsx): the desktop
  // renderer is a locally-bundled SPA with no SSR pass, and on web
  // `isDesktopShell()` is false both on the server and after hydration. The
  // markup matches either way, so the link can ship in the SSR payload instead
  // of popping in a frame late.
  const desktop = isDesktopShell();
  return (
    <DropdownMenu>
      <TooltipHint label={t(($) => $.help.trigger)} side="right">
        <DropdownMenuTrigger
          aria-label={t(($) => $.help.trigger)}
          className={RAIL_ICON_BUTTON_CLASS}
        >
          <CircleHelp />
        </DropdownMenuTrigger>
      </TooltipHint>
      <DropdownMenuContent
        align="end"
        side="right"
        sideOffset={8}
        className="min-w-40 max-w-56"
      >
        {!desktop && (
          <>
            <DropdownMenuItem
              render={
                <a
                  href={DOWNLOAD_URL}
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
        <DropdownMenuItem
          render={
            <a
              href={`${DOCS_URL}${docsLocalePrefix(i18n.language)}`}
              target="_blank"
              rel="noopener noreferrer"
            />
          }
        >
          <BookOpen className="h-3.5 w-3.5" />
          {t(($) => $.help.docs)}
          <ArrowUpRight className="size-3 translate-y-px text-faint-foreground" />
        </DropdownMenuItem>
        <DropdownMenuItem
          render={
            <a
              href={CHANGELOG_URL}
              target="_blank"
              rel="noopener noreferrer"
            />
          }
        >
          <History className="h-3.5 w-3.5" />
          {t(($) => $.help.changelog)}
          <ArrowUpRight className="size-3 translate-y-px text-faint-foreground" />
        </DropdownMenuItem>
        <DropdownMenuItem
          render={
            <a href={DISCORD_URL} target="_blank" rel="noopener noreferrer" />
          }
        >
          <DiscordIcon className="h-3.5 w-3.5" />
          {t(($) => $.help.discord)}
          <ArrowUpRight className="size-3 translate-y-px text-faint-foreground" />
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
                above the rail). */}
            <DropdownMenuGroup>
              <DropdownMenuLabel className="font-normal break-words">
                {t(($) => $.help.server_version, { version: serverVersion })}
              </DropdownMenuLabel>
            </DropdownMenuGroup>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
