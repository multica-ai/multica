import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import type { Squad } from "@multica/core/types";

// Thin Squad adapter over ActorAvatar — `isSquad` drives the square tile and
// the Users-icon fallback for a squad with no image.
export function SquadAvatar({
  squad,
  size = 36,
  className,
}: {
  squad: Pick<Squad, "name" | "avatar_url">;
  size?: number;
  className?: string;
}) {
  return (
    <ActorAvatarBase
      name={squad.name}
      initials=""
      isSquad
      avatarUrl={squad.avatar_url}
      size={size}
      className={className}
    />
  );
}
