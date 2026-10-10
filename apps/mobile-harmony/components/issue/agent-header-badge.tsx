/**
 * HarmonyOS port of apps/mobile/components/issue/agent-header-badge.tsx —
 * ambient status badge for the issue-detail header (right side). Renders
 * only when ≥1 agent task is active on this issue; otherwise null.
 *
 * Why this exists: the in-card AgentActivityRow is the first-time-discovery
 * surface (full "Working" text + larger avatars), but it scrolls away with
 * the timeline. Agent tasks run for minutes to tens of minutes; users
 * actively scroll during that window to read past comments. The "is
 * anything still working" signal needs a consistent location.
 *
 * Tap opens the runs surface via the `onOpenRuns` callback prop — the same
 * destination the in-card row opens. One destination, two entry points.
 */
import React from "react";
import { StyleSheet } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import { AvatarStack, type StackActor } from "@/components/ui/avatar-stack";
import { PulseDot } from "@/components/ui/pulse-dot";
import { issueActiveTasksOptions } from "@/data/queries/issues";
import { useWorkspaceStore } from "@/data/workspace-store";

interface Props {
  issueId: string;
  /** Opens the issue's runs surface (screen push or sheet — the host picks). */
  onOpenRuns: (issueId: string) => void;
}

export function AgentHeaderBadge({ issueId, onOpenRuns }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: active = [] } = useQuery(
    issueActiveTasksOptions(wsId, issueId),
  );

  if (active.length === 0) return null;

  const actors = active.map<StackActor>((t) => ({
    type: "agent",
    id: t.agent_id,
  }));

  return (
    <Pressable
      onPress={() => onOpenRuns(issueId)}
      hitSlop={8}
      accessibilityLabel="Agent working — open runs"
      style={({ pressed }) => [
        // flex-row items-center gap-1.5 px-2 py-1
        styles.row,
        pressed ? { opacity: 0.6 } : null,
      ]}
    >
      <AvatarStack actors={actors} max={2} size={20} />
      <PulseDot size={6} />
    </Pressable>
  );
}

// flex-row items-center gap-1.5 px-2 py-1
const styles = StyleSheet.create({
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    paddingHorizontal: 8,
    paddingVertical: 4,
  },
});
