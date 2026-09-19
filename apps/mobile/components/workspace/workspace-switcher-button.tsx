/**
 * Persistent current-workspace indicator for the tab-root header's leading
 * slot — the Slack / Linear / Notion pattern: the active workspace's avatar is
 * always visible top-left, and tapping it opens the switcher.
 *
 * Lives in the header (not just the More tab) so you can always tell which
 * workspace you're in at a glance, and switch from anywhere.
 */
import { Pressable } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { WorkspaceAvatar } from "@/components/workspace/workspace-avatar";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { useWorkspaceStore } from "@/data/workspace-store";

export function WorkspaceSwitcherButton() {
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { data } = useQuery(workspaceListOptions());
  const workspace = data?.find((w) => w.slug === slug);

  if (!workspace || !slug) return null;

  return (
    <Pressable
      onPress={() => router.push(`/${slug}/switch-workspace`)}
      accessibilityLabel={`Current workspace: ${workspace.name}. Tap to switch workspace.`}
      hitSlop={8}
      className="px-1 active:opacity-60"
    >
      <WorkspaceAvatar
        name={workspace.name}
        avatarUrl={workspace.avatar_url}
        size={28}
      />
    </Pressable>
  );
}
