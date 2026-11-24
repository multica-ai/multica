"use client";

import type { ReactNode } from "react";
import type { InboxItem } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { InboxDetailLabel } from "./inbox-detail-label";

/** Whoever caused the notification; the recipient when nobody did. */
export function InboxActorAvatar({ item, size }: { item: InboxItem; size: "xs" | "sm" }) {
  return (
    <ActorAvatar
      actorType={item.actor_type ?? item.recipient_type}
      actorId={item.actor_id ?? item.recipient_id}
      size={size}
    />
  );
}

/** Who did what: the actor's name, then what the notification says happened. */
export function InboxActorLine({ item, detail }: { item: InboxItem; detail?: ReactNode }) {
  const { getActorName } = useActorName();
  const name =
    item.actor_type && item.actor_type !== "system" && item.actor_id
      ? getActorName(item.actor_type, item.actor_id)
      : null;
  return (
    <>
      {name && <span className="font-medium">{name} </span>}
      {detail ?? <InboxDetailLabel item={item} />}
    </>
  );
}
