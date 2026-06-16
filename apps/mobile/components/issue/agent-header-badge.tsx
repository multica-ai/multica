/**
 * Ambient status badge for the issue detail Stack header (right side).
 * Renders only when ≥1 agent task is active on this issue; otherwise null.
 *
 * Why this exists: the in-card `<AgentActivityRow>` is the first-time-
 * discovery surface (full "Working" text + larger avatars), but it scrolls
 * away with the timeline. Agent tasks run for minutes to tens of minutes;
 * users actively scroll during that window to read past comments. The
 * "is anything still working" signal needs a consistent location — see
 * Apple HIG "Progress Indicators" + the agent-UX "ambient status badge"
 * pattern (https://www.aiuxdesign.guide/patterns/agent-status-monitoring).
 *
 * Tap pushes the `issue/[id]/runs` formSheet route — the in-card
 * AgentActivityRow does the same. One route, two entry points, no
 * duplicate sheet state.
 */
import { Pressable } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { AvatarStack, type StackActor } from "@/components/ui/avatar-stack";
import { PulseDot } from "@/components/ui/pulse-dot";
import { issueActiveTasksOptions } from "@/data/queries/issues";
import { useWorkspaceStore } from "@/data/workspace-store";

interface Props {
  issueId: string;
}

export function AgentHeaderBadge({ issueId }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
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
      onPress={() => {
        if (!wsSlug) return;
        router.push({
          pathname: "/[workspace]/issue/[id]/runs",
          params: { workspace: wsSlug, id: issueId },
        });
      }}
      hitSlop={8}
      accessibilityLabel="Agent working — open runs"
      className="flex-row items-center gap-1.5 px-2 py-1 active:opacity-60"
    >
      <AvatarStack actors={actors} max={2} size={20} />
      <PulseDot size={6} />
    </Pressable>
  );
}
