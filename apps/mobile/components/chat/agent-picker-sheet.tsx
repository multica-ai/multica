/**
 * Agent picker — bottom Modal listing agents the current user can assign /
 * chat with. Shown when the user taps `+ New Chat` and the workspace has
 * more than one usable agent; with exactly one, the chat screen skips this
 * sheet and goes straight to the blank state for that agent.
 *
 * Filtering is delegated to the caller (the screen passes a pre-filtered
 * `agents` list) so the same filter logic — archived + canAssignAgent +
 * order — stays in one place.
 *
 * Layout mirrors `components/issue/my-issues-filter-sheet.tsx`: transparent
 * Modal + dimmed backdrop + centered card. Bottom-sheet anchoring would be
 * nicer but the current codebase doesn't pull in a bottom-sheet lib and
 * centered cards already work well on iOS.
 */
import { Modal, Pressable, ScrollView, View } from "react-native";
import type { Agent } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { cn } from "@/lib/utils";

interface Props {
  visible: boolean;
  agents: Agent[];
  currentAgentId: string | null;
  onPick: (agent: Agent) => void;
  onClose: () => void;
}

export function AgentPickerSheet({
  visible,
  agents,
  currentAgentId,
  onPick,
  onClose,
}: Props) {
  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      onRequestClose={onClose}
    >
      <Pressable className="flex-1 bg-black/40" onPress={onClose}>
        <View className="flex-1 items-center justify-center px-6">
          <Pressable onPress={() => {}} className="w-full max-w-sm">
            <View className="bg-popover rounded-2xl overflow-hidden">
              <View className="px-4 py-3 border-b border-border">
                <Text className="text-base font-semibold text-foreground">
                  Choose an agent
                </Text>
              </View>

              <ScrollView className="max-h-96">
                {agents.length === 0 ? (
                  <View className="px-4 py-8">
                    <Text className="text-sm text-muted-foreground text-center">
                      No agents available.
                    </Text>
                  </View>
                ) : (
                  agents.map((agent) => {
                    const selected = agent.id === currentAgentId;
                    return (
                      <Pressable
                        key={agent.id}
                        onPress={() => {
                          onPick(agent);
                          onClose();
                        }}
                        className={cn(
                          "flex-row items-center gap-3 px-4 py-3 active:bg-secondary",
                          selected && "bg-secondary/60",
                        )}
                      >
                        <ActorAvatar type="agent" id={agent.id} size={32} showPresence />
                        <View className="flex-1">
                          <Text
                            className="text-sm font-medium text-foreground"
                            numberOfLines={1}
                          >
                            {agent.name}
                          </Text>
                          {agent.description ? (
                            <Text
                              className="text-xs text-muted-foreground mt-0.5"
                              numberOfLines={1}
                            >
                              {agent.description}
                            </Text>
                          ) : null}
                        </View>
                        {selected ? (
                          <Text className="text-sm text-primary font-semibold">
                            ✓
                          </Text>
                        ) : null}
                      </Pressable>
                    );
                  })
                )}
              </ScrollView>
            </View>
          </Pressable>
        </View>
      </Pressable>
    </Modal>
  );
}
