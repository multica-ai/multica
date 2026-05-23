"use client";

// Wraps CerebroAvatarPicker for the agent detail inspector so an existing
// agent's avatar can be replaced via manual upload OR AI generation, with
// the resulting URL persisted via the inspector's onUpdate hook. Mirrors the
// affordance shipped for the create-agent dialog (JEH-1563). Relocated from
// the upstream zone under FIR-1918 — see docs/cerebro-patches.md
// (`agent-avatar-generate`).

import type { Agent } from "@multica/core/types";
import { ActorAvatar } from "@multica/views/common/actor-avatar";
import { CerebroAvatarPicker } from "./avatar-picker";

interface CerebroInspectorAvatarProps {
  agent: Agent;
  canEdit: boolean;
  onUpdate: (data: Record<string, unknown>) => Promise<void>;
}

export function CerebroInspectorAvatar({
  agent,
  canEdit,
  onUpdate,
}: CerebroInspectorAvatarProps) {
  if (!canEdit) {
    return (
      <div className="h-14 w-14 shrink-0 overflow-hidden rounded-lg bg-muted">
        <ActorAvatar
          actorType="agent"
          actorId={agent.id}
          size={56}
          className="rounded-none"
        />
      </div>
    );
  }

  // CerebroAvatarPicker calls onChange with the new URL after a successful
  // upload OR a successful AI generation, and with `null` when the user
  // clears the choice. In all three cases we want the same persistence
  // contract used by the rest of the inspector — flow through onUpdate so
  // the agent-detail-page wires cache invalidation + success toast.
  const handleChange = (url: string | null) => {
    void onUpdate({ avatar_url: url });
  };

  return (
    <CerebroAvatarPicker
      value={agent.avatar_url ?? null}
      onChange={handleChange}
      agentName={agent.name}
      size={56}
    />
  );
}
