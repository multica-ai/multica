"use client";

import { ArrowUpRight, BookOpen, CircleHelp, History, MessageCircle } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useModalStore } from "@multica/core/modals";
import { useConfigStore } from "@multica/core/config";
import { DISCORD_URL, DiscordIcon } from "./discord";
import { useT } from "../i18n";

const DOCS_URL = "https://multica.ai/docs";
const CHANGELOG_URL = "https://multica.ai/changelog";

export function HelpLauncher() {
  const { t } = useT("layout");
  // Read frontend and backend observations independently so a partial
  // rollout or a missing backend metadata can render each row with its own
  // state (tag / loading / unavailable) instead of inventing a value.
  const frontendBaseline = useConfigStore((state) => state.frontendBaseline);
  const backendBaseline = useConfigStore((state) => state.backendBaseline);
  const backendBaselineStatus = useConfigStore(
    (state) => state.backendBaselineStatus,
  );

  // The provenance rows are intentionally always present in the DOM (even
  // when unavailable): self-host operators rely on them to confirm what is
  // deployed. A hidden row would make a stale or rolled-back artifact look
  // identical to a missing one, defeating the whole feature.
  const frontendText = frontendBaseline
    ? frontendBaseline
    : t(($) => $.help.frontend_unavailable);
  const backendText =
    backendBaseline ||
    (backendBaselineStatus === "loading"
      ? t(($) => $.help.backend_loading)
      : t(($) => $.help.backend_unavailable));

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label={t(($) => $.help.trigger)}
        title={t(($) => $.help.trigger)}
        className="inline-flex size-7 items-center justify-center rounded-full text-muted-foreground transition-colors cursor-pointer hover:bg-accent hover:text-foreground data-popup-open:bg-accent data-popup-open:text-foreground"
      >
        <CircleHelp className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        side="top"
        sideOffset={8}
        className="min-w-40 max-w-56"
      >
        <DropdownMenuItem
          render={
            <a href={DOCS_URL} target="_blank" rel="noopener noreferrer" />
          }
        >
          <BookOpen className="h-3.5 w-3.5" />
          {t(($) => $.help.docs)}
          <ArrowUpRight className="size-3 translate-y-px text-muted-foreground/50" />
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
          <ArrowUpRight className="size-3 translate-y-px text-muted-foreground/50" />
        </DropdownMenuItem>
        <DropdownMenuItem
          render={
            <a href={DISCORD_URL} target="_blank" rel="noopener noreferrer" />
          }
        >
          <DiscordIcon className="h-3.5 w-3.5" />
          {t(($) => $.help.discord)}
          <ArrowUpRight className="size-3 translate-y-px text-muted-foreground/50" />
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => useModalStore.getState().open("feedback")}
        >
          <MessageCircle className="h-3.5 w-3.5" />
          {t(($) => $.help.feedback)}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        {/* DropdownMenuLabel renders Base UI's Menu.GroupLabel, which reads
            a Menu.Group context and throws if it has no Group ancestor. It
            must always be wrapped in a DropdownMenuGroup — without it the
            Help menu crashes the whole app on open (no error boundary sits
            above the sidebar). Two rows share one Group so a single
            dropdown-label context surrounds both. */}
        <DropdownMenuGroup>
          <DropdownMenuLabel className="flex items-center gap-2 font-normal text-muted-foreground">
            <span>{t(($) => $.help.frontend_label)}</span>
            <span className="text-foreground">{frontendText}</span>
          </DropdownMenuLabel>
          <DropdownMenuLabel className="flex items-center gap-2 font-normal text-muted-foreground">
            <span>{t(($) => $.help.backend_label)}</span>
            <span className="text-foreground">{backendText}</span>
          </DropdownMenuLabel>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}