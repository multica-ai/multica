"use client";

import { X } from "lucide-react";
import { useAuthStore } from "@multica/core/auth";
import { DISCORD_URL, DiscordIcon } from "./discord";
import { useDiscordCardDismissed } from "./use-discord-card-dismissed";
import { useT } from "../i18n";

/**
 * Dismissible "Join our Discord" entry sharing the sidebar footer strip with
 * the help launcher. Once dismissed it stays hidden for that user on this
 * browser — see {@link useDiscordCardDismissed}.
 *
 * Deliberately shaped as a sidebar row, not a bordered card: it matches
 * SidebarMenuButton metrics (h-8 / rounded-md / gap-2 / text-body) and
 * carries no resting border or fill. A dismissible promo is the
 * lowest-priority item in the sidebar, so it must not be drawn heavier than
 * the navigation above it.
 *
 * No external-link arrow, unlike the Help menu's outbound items: sharing one
 * 224px strip with the help trigger leaves 128px for the label, and the arrow
 * plus the dismiss button together overflow that in the widest locale (en,
 * 109px). The dismiss affordance wins because it is a user-facing capability
 * and the arrow is only a hint — the Discord mark already signals the
 * destination. `flex-1 min-w-0` makes the label truncate rather than push the
 * trigger off.
 *
 * That 128px is the budget at the DEFAULT 256px sidebar, and the sidebar is
 * user-resizable down to 200px — every 1px of drag takes 1px from the label.
 * So a title that merely fits at 256px still truncates for anyone who has
 * narrowed their sidebar; keep every locale's title comfortably under budget,
 * not just under it (MUL-5704).
 */
export function JoinDiscordCard() {
  const { t } = useT("layout");
  const userId = useAuthStore((s) => s.user?.id);
  const [dismissed, dismiss] = useDiscordCardDismissed(userId);

  if (dismissed) return null;

  return (
    <div className="group/discord relative min-w-0 flex-1">
      <a
        href={DISCORD_URL}
        target="_blank"
        rel="noopener noreferrer"
        className="flex h-8 items-center gap-2 rounded-md px-2 pr-8 text-body text-muted-foreground ring-sidebar-ring outline-hidden transition-colors hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground focus-visible:ring-2"
      >
        <DiscordIcon className="size-4 shrink-0" />
        <span className="truncate">
          {t(($) => $.sidebar.discord_card.title)}
        </span>
      </a>
      {/* Revealed on hover/focus so the resting row stays quiet. Coarse
          pointers get no hover, so keep it permanently visible there. */}
      <button
        type="button"
        aria-label={t(($) => $.sidebar.discord_card.dismiss)}
        onClick={dismiss}
        className="absolute inset-y-0 right-1 my-auto flex size-6 cursor-pointer items-center justify-center rounded-sm text-muted-foreground opacity-0 ring-sidebar-ring transition-opacity hover:bg-sidebar-accent hover:text-foreground focus-visible:opacity-100 focus-visible:ring-2 group-hover/discord:opacity-100 [@media(hover:none)]:opacity-100"
      >
        <X className="size-3.5" />
      </button>
    </div>
  );
}
