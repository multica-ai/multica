/**
 * Banner shown when the workspace has zero usable agents for the current
 * user. Mirrors the role of packages/views/chat/components/no-agent-banner.tsx
 * on web — distinct visual cue + a route into the place where users can
 * add agents.
 *
 * Rendered just under ChatHeader. Tap → More → Agents.
 */
import { Pressable } from "react-native";
import { router } from "expo-router";
import { Text } from "@/components/ui/text";
import { useWorkspaceStore } from "@/data/workspace-store";

export function NoAgentBanner() {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  const handlePress = () => {
    if (!wsSlug) return;
    router.push(`/${wsSlug}/more/agents`);
  };

  return (
    <Pressable
      onPress={handlePress}
      className="mx-3 mt-2 mb-1 rounded-xl border border-border bg-secondary/50 px-3 py-2 active:opacity-80"
      accessibilityRole="button"
      accessibilityLabel="No agents available, open agents settings"
    >
      <Text className="text-sm font-medium text-foreground">
        No agents available
      </Text>
      <Text className="text-xs text-muted-foreground mt-0.5">
        Add or enable an agent in More → Agents to start chatting.
      </Text>
    </Pressable>
  );
}
