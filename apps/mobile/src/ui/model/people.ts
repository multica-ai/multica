import { useCallback } from "react";
import { useActorName } from "@multica/core/workspace/hooks";
import type { AvatarActor } from "../components/Avatar";

export type ActorResolver = (type: string | null | undefined, id: string | null | undefined, name?: string) => AvatarActor;

export function actorKind(type: string | null | undefined): AvatarActor["kind"] {
  if (type === "member") return "member";
  if (type === "agent" || type === "squad") return "agent";
  return "system";
}

/** Names and avatars of members and agents in the current workspace. */
export function useActorResolver(): ActorResolver {
  const { getActorName, getActorAvatarUrl } = useActorName();
  return useCallback(
    (type, id, name) => ({
      kind: actorKind(type),
      name: name ?? (type && id ? getActorName(type, id) : "Multica"),
      avatarUrl: type && id ? getActorAvatarUrl(type, id) : null,
    }),
    [getActorName, getActorAvatarUrl],
  );
}
