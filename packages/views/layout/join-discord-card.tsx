"use client";

import { X } from "lucide-react";
import { useAuthStore } from "@multica/core/auth";
import { DISCORD_URL, DiscordIcon } from "./discord";
import { useDiscordCardDismissed } from "./use-discord-card-dismissed";
import { useT } from "../i18n";

/**
 * Dismissible "Join our Discord" promo pinned to the bottom of the left
 * sidebar (above the help launcher). Once dismissed it stays hidden for
 * that user on this browser — see {@link useDiscordCardDismissed}.
 */
export function JoinDiscordCard() {
  const { t } = useT("layout");
  const userId = useAuthStore((s) => s.user?.id);
  const [dismissed, dismiss] = useDiscordCardDismissed(userId);

  if (dismissed) return null;

  return (
    <div className="relative">
      <a
        href={DISCORD_URL}
        target="_blank"
        rel="noopener noreferrer"
        className="flex items-start gap-2.5 rounded-md border border-sidebar-border bg-sidebar-accent/50 p-2.5 pr-7 transition-colors hover:bg-sidebar-accent"
      >
        {/* Discord blurple (#5865F2) is the brand mark color — an intentional
            exception to the semantic-token rule, like the landing social icons. */}
        <DiscordIcon className="mt-px size-4 shrink-0 text-[#5865F2]" />
        <span className="min-w-0">
          <span className="block text-xs font-medium text-sidebar-foreground">
            {t(($) => $.sidebar.discord_card.title)}
          </span>
          <span className="mt-0.5 block text-[11px] leading-snug text-muted-foreground">
            {t(($) => $.sidebar.discord_card.description)}
          </span>
        </span>
      </a>
      <button
        type="button"
        aria-label={t(($) => $.sidebar.discord_card.dismiss)}
        onClick={dismiss}
        className="absolute top-1.5 right-1.5 flex size-5 items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-sidebar-border hover:text-foreground"
      >
        <X className="size-3" />
      </button>
    </div>
  );
}
