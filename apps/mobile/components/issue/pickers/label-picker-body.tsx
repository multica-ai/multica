/**
 * Pure picker body for issue labels — multi-select with toggle-on-tap.
 * Mirrors the assignee picker shape (native nav header + UISearchController
 * registered in `_layout.tsx`; `query` flows in as a prop via
 * `useNativeSearchBar`) with two key differences:
 *
 *   1. Multi-select: tap toggles attach/detach and does NOT close the
 *      sheet. The user dismisses via grabber drag-down or Back.
 *   2. Inline create: when the query has no exact match, the top row
 *      becomes a "Create '<query>'" affordance — taps create-and-attach
 *      in one motion.
 *
 * Mirrors `packages/views/issues/components/pickers/label-picker.tsx` for
 * the createAndAttach + pickInlineColor logic.
 */
import { useMemo } from "react";
import { FlatList, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Ionicons } from "@expo/vector-icons";
import { useColorScheme } from "nativewind";
import type { Label } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { labelListOptions } from "@/data/queries/labels";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useScrollToTopOnChange } from "@/lib/use-scroll-to-top-on-change";
import { pickInlineColor } from "@/lib/inline-color";
import { THEME } from "@/lib/theme";

type Row =
  | { kind: "create"; name: string }
  | { kind: "label"; label: Label };

interface Props {
  attached: Label[];
  query: string;
  onAttach: (label: Label) => void;
  onDetach: (labelId: string) => void;
  /** Create-and-attach in one motion. `query` is the entered text. */
  onCreate: (name: string, color: string) => void;
}

export function LabelPickerBody({
  attached,
  query,
  onAttach,
  onDetach,
  onCreate,
}: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: labels = [] } = useQuery(labelListOptions(wsId));
  const listRef = useScrollToTopOnChange(query);
  const { colorScheme } = useColorScheme();
  const checkColor =
    colorScheme === "dark" ? THEME.dark.primary : THEME.light.primary;

  const attachedIds = useMemo(
    () => new Set(attached.map((l) => l.id)),
    [attached],
  );

  const rows = useMemo<Row[]>(() => {
    const q = query.trim();
    const qLower = q.toLowerCase();

    const sorted = [...labels].sort((a, b) =>
      a.name.localeCompare(b.name),
    );
    const filtered = qLower
      ? sorted.filter((l) => l.name.toLowerCase().includes(qLower))
      : sorted;
    const exactMatch = sorted.some(
      (l) => l.name.toLowerCase() === qLower,
    );

    // No query → pin attached labels at top, others below.
    if (!q) {
      const attachedRows: Row[] = sorted
        .filter((l) => attachedIds.has(l.id))
        .map((l) => ({ kind: "label" as const, label: l }));
      const otherRows: Row[] = sorted
        .filter((l) => !attachedIds.has(l.id))
        .map((l) => ({ kind: "label" as const, label: l }));
      return [...attachedRows, ...otherRows];
    }

    // Query active → show Create row first when no exact match, then matches.
    const labelRows: Row[] = filtered.map((l) => ({
      kind: "label" as const,
      label: l,
    }));
    return exactMatch ? labelRows : [{ kind: "create", name: q }, ...labelRows];
  }, [labels, query, attachedIds]);

  const onToggle = (label: Label) => {
    if (attachedIds.has(label.id)) onDetach(label.id);
    else onAttach(label);
  };

  return (
    <FlatList
      ref={listRef}
      data={rows}
      className="flex-1"
      keyboardShouldPersistTaps="handled"
      automaticallyAdjustKeyboardInsets
      contentInsetAdjustmentBehavior="automatic"
      keyExtractor={(row) =>
        row.kind === "create" ? `create:${row.name}` : `l:${row.label.id}`
      }
      renderItem={({ item }) =>
        item.kind === "create" ? (
          <Pressable
            onPress={() => onCreate(item.name, pickInlineColor(item.name))}
            className="flex-row items-center gap-3 px-4 py-3 active:bg-secondary"
          >
            <View
              className="size-3 rounded-full"
              style={{ backgroundColor: pickInlineColor(item.name) }}
            />
            <Text className="flex-1 text-base text-foreground">
              Create &ldquo;{item.name}&rdquo;
            </Text>
            <Ionicons name="add" size={20} color={checkColor} />
          </Pressable>
        ) : (
          <Pressable
            onPress={() => onToggle(item.label)}
            className="flex-row items-center gap-3 px-4 py-3 active:bg-secondary"
          >
            <View
              className="size-3 rounded-full"
              style={{ backgroundColor: item.label.color }}
            />
            <Text
              className="flex-1 text-base text-foreground"
              numberOfLines={1}
            >
              {item.label.name}
            </Text>
            {attachedIds.has(item.label.id) ? (
              <Ionicons name="checkmark" size={20} color={checkColor} />
            ) : null}
          </Pressable>
        )
      }
      ListEmptyComponent={
        <View className="px-3 py-8 items-center">
          <Text className="text-sm text-muted-foreground text-center">
            {query
              ? "No matches."
              : "No labels in this workspace yet."}
          </Text>
        </View>
      }
    />
  );
}
