"use client";

import { useQuery } from "@tanstack/react-query";
import { Lock } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { NoteLock } from "../core";

// NoteLockBanner (Wave 3 / G3): shown above the editor when a note is being
// edited by SOMEONE ELSE (a live lock the caller doesn't hold). It names the
// holder and offers "Take over", which steals the lock so the caller can edit.
export function NoteLockBanner({
  lock,
  acquiring,
  onTakeOver,
}: {
  lock: NoteLock;
  acquiring: boolean;
  onTakeOver: () => void;
}) {
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const holderName =
    members.find((m) => m.user_id === lock.locked_by)?.name ?? "Someone";

  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-amber-500/30 bg-amber-500/10 px-4 py-2 text-sm sm:px-5">
      <Lock className="size-4 shrink-0 text-amber-600" />
      <span className="min-w-0">
        <span className="font-medium">{holderName}</span> is editing this note.
        You can read it; take over to edit.
      </span>
      <Button
        size="sm"
        variant="secondary"
        className="ml-auto shrink-0"
        onClick={onTakeOver}
        disabled={acquiring}
      >
        {acquiring ? "Taking over…" : "Take over"}
      </Button>
    </div>
  );
}
