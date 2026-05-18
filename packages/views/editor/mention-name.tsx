"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";

export type EntityMentionType = "agent" | "member" | "squad";

interface Props {
  type: EntityMentionType;
  /** The UUID embedded in the mention link (agent.id, user.id, or squad.id). */
  id: string;
  /** Markdown label text, with or without leading @. Shown only when the
   *  entity cannot be resolved against the workspace cache. */
  fallbackLabel: string;
}

/**
 * Renders the canonical entity name for an @-mention, looked up by UUID in
 * the workspace's TanStack Query cache. The markdown label (the text inside
 * `[...]`) is treated as a hint, not as truth — only the UUID determines
 * which entity is shown.
 *
 * This is the display-side defense for label/UUID mismatch. The backend's
 * mention.CanonicalizeMentions rewrites labels on write, so under normal
 * conditions the label and the resolved name already agree; this component
 * keeps comments honest for rows that predate that defense and for any
 * future write path that bypasses it.
 */
export function EntityMentionName({ type, id, fallbackLabel }: Props) {
  const wsId = useWorkspaceId();

  // Each useQuery is gated by `enabled` so we only subscribe to the list
  // that actually matters for this mention type — no fetches kicked off
  // for the other two.
  const { data: agents } = useQuery({
    ...agentListOptions(wsId),
    enabled: type === "agent",
  });
  const { data: members } = useQuery({
    ...memberListOptions(wsId),
    enabled: type === "member",
  });
  const { data: squads } = useQuery({
    ...squadListOptions(wsId),
    enabled: type === "squad",
  });

  let resolved: string | undefined;
  if (type === "agent") {
    resolved = agents?.find((a) => a.id === id)?.name;
  } else if (type === "member") {
    // Mention UUIDs for members are user IDs (see backend formatMention and
    // CanonicalizeMentions' GetUser lookup), not workspace_member row IDs.
    resolved = members?.find((m) => m.user_id === id)?.name;
  } else {
    resolved = squads?.find((s) => s.id === id)?.name;
  }

  const visible = resolved ?? stripLeadingAt(fallbackLabel);
  return <>@{visible}</>;
}

function stripLeadingAt(label: string): string {
  return label.startsWith("@") ? label.slice(1) : label;
}
