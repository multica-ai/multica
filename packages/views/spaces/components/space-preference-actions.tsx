"use client";

import { Bell, BellOff, Pin, PinOff } from "lucide-react";
import { useUpdateSpacePreference } from "@multica/core/spaces";
import type { Space } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import { useT } from "../../i18n";

export function SpacePreferenceActions({
  space,
  iconOnly = false,
}: {
  space: Space;
  iconOnly?: boolean;
}) {
  const { t } = useT("spaces");
  const updatePreference = useUpdateSpacePreference();

  const update = async (patch: { is_pinned?: boolean; is_followed?: boolean }) => {
    try {
      await updatePreference.mutateAsync({ id: space.id, ...patch });
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.toast_preference_failed),
      );
    }
  };

  const pinLabel = space.is_pinned
    ? t(($) => $.actions.unpin)
    : t(($) => $.actions.pin);
  const followLabel = space.is_followed
    ? t(($) => $.actions.unfollow)
    : t(($) => $.actions.follow);
  const PinIcon = space.is_pinned ? PinOff : Pin;
  const FollowIcon = space.is_followed ? BellOff : Bell;

  return (
    <div className="flex items-center gap-1">
      <Button
        type="button"
        size={iconOnly ? "icon-sm" : "sm"}
        variant={space.is_pinned ? "secondary" : "ghost"}
        aria-label={pinLabel}
        title={pinLabel}
        disabled={!!space.archived_at || updatePreference.isPending}
        onClick={() => void update({ is_pinned: !space.is_pinned })}
      >
        <PinIcon className="size-3.5" aria-hidden />
        {!iconOnly && pinLabel}
      </Button>
      <Button
        type="button"
        size={iconOnly ? "icon-sm" : "sm"}
        variant={space.is_followed ? "secondary" : "ghost"}
        aria-label={followLabel}
        title={followLabel}
        disabled={!!space.archived_at || updatePreference.isPending}
        onClick={() => void update({ is_followed: !space.is_followed })}
      >
        <FollowIcon className="size-3.5" aria-hidden />
        {!iconOnly && followLabel}
      </Button>
    </div>
  );
}
