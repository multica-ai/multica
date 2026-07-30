"use client";

import { useAgentRuntimeProvider } from "@multica/core/agents";
import { providerDisplayName } from "@multica/core/runtimes";
import { useCurrentWorkspace } from "@multica/core/paths";
import { AVATAR_SIZE_PX, type AvatarSize } from "@multica/ui/lib/avatar-size";
import { ProviderLogo } from "../runtimes/components/provider-logo";

/**
 * Smallest avatar that can carry a provider mark.
 *
 * A badge gets roughly 40% of the avatar's diameter, and provider marks are
 * detailed artwork (Claude's glyph, CodeBuddy's tile) rather than a flat shape
 * like the presence dot — below ~12px they read as a smudge. 32px is the first
 * tier that clears it. That rules the badge out of exactly the surfaces the
 * status dot lives on: nearly every `showStatusDot` call site is a 20px picker
 * row, where "can it take work right now" is the question and the provider
 * isn't.
 */
export const RUNTIME_BADGE_MIN_AVATAR_PX = 32;

/**
 * Provider mark overlaid on the top-right of an agent avatar — the opposite
 * end from the presence dot, so a surface can carry both without them
 * touching.
 *
 * Derived from the agent's current runtime on every render, so switching
 * runtimes moves the mark with it. Renders nothing when the provider can't be
 * resolved (loading, archived agent, deleted runtime): a wrong mark is worse
 * than none.
 *
 * Lives in its own module rather than beside `AgentStatusDot` because the
 * agent profile card needs it, and `actor-avatar` already imports that card
 * for its hover payload — importing back would close a cycle.
 */
export function AgentRuntimeBadge({
  agentId,
  size,
}: {
  agentId: string;
  size?: AvatarSize;
}) {
  const ws = useCurrentWorkspace();
  const provider = useAgentRuntimeProvider(ws?.id, agentId);
  const px = size ? AVATAR_SIZE_PX[size] : 24;
  if (!provider || px < RUNTIME_BADGE_MIN_AVATAR_PX) return null;

  // The disc is background-coloured so a multi-coloured mark stays legible
  // over an emoji or photo avatar; the mark itself fills most of it. The
  // hairline is what makes it read as a chip sitting ON the avatar rather than
  // an icon floating beside it — several provider marks (Claude's asterisk,
  // Codex's ring) are airy line art with no silhouette of their own, and
  // without an edge the badge loses its anchor. Same treatment as ProviderChip.
  const disc = Math.round(px * 0.42);
  // Push the badge out past the bounding box so its centre lands on the
  // avatar's rim instead of inside it. Flush to the corner (`top-0 right-0`)
  // looks correct on a square but not on a circle: at 45° the edge has already
  // curved ~29% of the diameter away from that corner, so the badge ends up
  // sitting over the face. Offsetting by (badge radius − rim inset) leaves
  // roughly half of it overhanging, which is the usual platform-badge look.
  const offset = Math.round(px * 0.064);

  return (
    <span
      aria-label={`Runtime: ${providerDisplayName(provider)}`}
      className="absolute inline-flex items-center justify-center rounded-full bg-background ring-1 ring-border"
      style={{ width: disc, height: disc, top: -offset, right: -offset }}
    >
      <ProviderLogo provider={provider} className="h-[72%] w-[72%]" />
    </span>
  );
}
