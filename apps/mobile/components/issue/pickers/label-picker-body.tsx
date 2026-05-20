/**
 * Pure picker body for issue labels — multi-select with toggle-on-tap. See
 * status-picker-body.tsx for the split rationale.
 *
 * Phase 1 does not support inline label creation; mobile users who want a
 * new label create it on web (matches the previous picker-sheet behaviour).
 */
import { useMemo, useState } from "react";
import { ActivityIndicator, FlatList, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import type { Label } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { labelListOptions } from "@/data/queries/labels";
import { useWorkspaceStore } from "@/data/workspace-store";
import { cn } from "@/lib/utils";

interface Props {
  attached: Label[];
  onAttach: (label: Label) => void;
  onDetach: (labelId: string) => void;
}

export function LabelPickerBody({ attached, onAttach, onDetach }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: labels, isLoading } = useQuery(labelListOptions(wsId));
  const [query, setQuery] = useState("");

  const attachedIds = useMemo(
    () => new Set(attached.map((l) => l.id)),
    [attached],
  );

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const all = labels ?? [];
    const sorted = [...all].sort((a, b) => a.name.localeCompare(b.name));
    if (!q) return sorted;
    return sorted.filter((l) => l.name.toLowerCase().includes(q));
  }, [labels, query]);

  const onToggle = (label: Label) => {
    if (attachedIds.has(label.id)) onDetach(label.id);
    else onAttach(label);
  };

  return (
    <View className="flex-1">
      <View className="px-4 pt-3 pb-2">
        <Text className="text-lg font-semibold text-foreground">Labels</Text>
      </View>
      <View className="px-3 pb-2 border-b border-border">
        <TextField
          value={query}
          onChangeText={setQuery}
          placeholder="Search labels"
          autoCapitalize="none"
          autoCorrect={false}
        />
      </View>
      {isLoading ? (
        <View className="px-3 py-8 items-center">
          <ActivityIndicator />
        </View>
      ) : (
        <FlatList
          data={filtered}
          className="flex-1"
          keyExtractor={(item) => item.id}
          keyboardShouldPersistTaps="handled"
          automaticallyAdjustKeyboardInsets
          renderItem={({ item }) => {
            const checked = attachedIds.has(item.id);
            return (
              <Pressable
                onPress={() => onToggle(item)}
                className={cn(
                  "flex-row items-center gap-3 px-3 py-2.5 active:bg-secondary",
                  checked && "bg-secondary",
                )}
              >
                <View
                  className="size-3 rounded-full"
                  style={{ backgroundColor: item.color }}
                />
                <Text className="flex-1 text-sm text-foreground">
                  {item.name}
                </Text>
                {checked ? (
                  <Text className="text-xs text-muted-foreground">✓</Text>
                ) : null}
              </Pressable>
            );
          }}
          ListEmptyComponent={
            <View className="px-3 py-6 items-center">
              <Text className="text-xs text-muted-foreground text-center">
                {query
                  ? "No matches."
                  : "No labels in this workspace yet.\nCreate them on web."}
              </Text>
            </View>
          }
        />
      )}
    </View>
  );
}
