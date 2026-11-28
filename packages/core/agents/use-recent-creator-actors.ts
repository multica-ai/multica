"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { defaultStorage } from "../platform/storage";
import {
  type CreatorActorRef,
  creatorActorsStorageKey,
  parseCreatorActors,
  prependCreatorActor,
} from "./recent-creator-actors";

export function useRecentCreatorActors({
  userId,
  wsId,
  enabled,
}: {
  userId: string | null | undefined;
  wsId: string | null | undefined;
  enabled: boolean;
}) {
  const scopedUserId = userId ?? "";
  const scopedWsId = wsId ?? "";
  const storageKey =
    enabled && scopedUserId && scopedWsId
      ? creatorActorsStorageKey(scopedUserId, scopedWsId)
      : "";
  const [localActors, setLocalActors] = useState<CreatorActorRef[]>([]);

  useEffect(() => {
    setLocalActors(
      storageKey ? parseCreatorActors(defaultStorage.getItem(storageKey)) : [],
    );
  }, [storageKey]);

  const recordRecentCreator = useCallback(
    (next: CreatorActorRef) => {
      if (!storageKey) return;
      setLocalActors((current) => {
        const updated = prependCreatorActor(current, next);
        defaultStorage.setItem(storageKey, JSON.stringify(updated));
        return updated;
      });
    },
    [storageKey],
  );

  return useMemo(
    () => ({ localActors, recordRecentCreator }),
    [localActors, recordRecentCreator],
  );
}
