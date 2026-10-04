/**
 * HarmonyOS port of apps/mobile/components/issue/pickers/label-picker-body.tsx
 * (+ `issue/[id]/picker/label.tsx`) — multi-select with toggle-on-tap and
 * inline create. Two key behaviors carried over:
 *
 *   1. Multi-select: tap toggles attach/detach and does NOT close the
 *      sheet. The user dismisses via backdrop tap / grabber / Back.
 *   2. Inline create: when the query has no exact match, the top row
 *      becomes a "Create '<query>'" affordance — taps create-and-attach
 *      in one motion.
 *
 * Mirrors `packages/views/issues/components/pickers/label-picker.tsx` for
 * the createAndAttach + pickInlineColor logic. A synchronous lock prevents
 * double-submit on rapid taps before React state updates (the iOS route's
 * `creatingRef` pattern).
 */
import React, { useMemo, useRef, useState } from "react";
import { FlatList, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type { Label } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Icon } from "@/components/ui/icon";
import { TextField } from "@/components/ui/text-field";
import { labelListOptions } from "@/data/queries/labels";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useScrollToTopOnChange } from "@/lib/use-scroll-to-top-on-change";
import { pickInlineColor } from "@/lib/inline-color";
import { useThemeColors } from "@/lib/use-theme-colors";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { issueDetailOptions } from "@/data/queries/issues";
import { useAttachLabel, useDetachLabel } from "@/data/mutations/issues";
import { useCreateLabel } from "@/data/mutations/labels";

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
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: labels = [] } = useQuery(labelListOptions(wsId));
  const listRef = useScrollToTopOnChange(query);

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
      style={styles.list}
      keyboardShouldPersistTaps="handled"
      keyExtractor={(row) =>
        row.kind === "create" ? `create:${row.name}` : `l:${row.label.id}`
      }
      renderItem={({ item }) =>
        item.kind === "create" ? (
          <Pressable
            onPress={() => onCreate(item.name, pickInlineColor(item.name))}
            accessibilityRole="button"
            accessibilityLabel={`Create label ${item.name}`}
            style={({ pressed }) => [
              styles.row,
              pressed ? { backgroundColor: c.secondary } : null,
            ]}
          >
            <View
              style={[styles.dot, { backgroundColor: pickInlineColor(item.name) }]}
            />
            <Text style={[styles.rowLabel, { color: c.foreground }]} numberOfLines={1}>
              Create “{item.name}”
            </Text>
            <Icon name="add" size={20} color={c.primary} />
          </Pressable>
        ) : (
          <Pressable
            onPress={() => onToggle(item.label)}
            accessibilityRole="button"
            accessibilityLabel={item.label.name}
            style={({ pressed }) => [
              styles.row,
              pressed ? { backgroundColor: c.secondary } : null,
            ]}
          >
            <View
              style={[styles.dot, { backgroundColor: item.label.color }]}
            />
            <Text style={[styles.rowLabel, { color: c.foreground }]} numberOfLines={1}>
              {item.label.name}
            </Text>
            {attachedIds.has(item.label.id) ? (
              <Icon name="checkmark" size={20} color={c.primary} />
            ) : null}
          </Pressable>
        )
      }
      ListEmptyComponent={
        <View style={styles.empty}>
          <Text
            style={[styles.emptyText, { color: c.mutedForeground }]}
          >
            {query
              ? "No matches."
              : "No labels in this workspace yet."}
          </Text>
        </View>
      }
    />
  );
}

interface SheetProps {
  issueId: string;
  visible: boolean;
  onClose: () => void;
}

/**
 * The iOS formSheet route. The sheet stays open across toggles; iOS resets
 * the native search text on dismiss — local query state replicates that by
 * clearing on close.
 */
export function IssueLabelPickerSheet({ issueId, visible, onClose }: SheetProps) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issue } = useQuery(issueDetailOptions(wsId, issueId));
  const attachLabel = useAttachLabel(issueId);
  const detachLabel = useDetachLabel(issueId);
  const createLabel = useCreateLabel();
  const [query, setQuery] = useState("");

  // Synchronous lock to prevent double-submit on rapid taps on the Create
  // row before React state updates — mirrors web's `creatingRef` pattern.
  const creatingRef = useRef(false);

  const attached = issue?.labels ?? [];

  return (
    <BottomSheet
      visible={visible}
      onClose={() => {
        setQuery("");
        onClose();
      }}
      maxHeightRatio={0.7}
    >
      <View style={styles.sheetHeader}>
        <Text style={[styles.sheetTitle, { color: c.foreground }]}>Label</Text>
        <TextField
          value={query}
          onChangeText={setQuery}
          placeholder="Search labels"
          autoCapitalize="none"
          style={styles.search}
        />
      </View>
      <View style={styles.sheetList}>
        <LabelPickerBody
          attached={attached}
          query={query}
          onAttach={(label) => attachLabel.mutate({ label })}
          onDetach={(labelId) => detachLabel.mutate({ labelId })}
          onCreate={(name, color) => {
            if (creatingRef.current) return;
            creatingRef.current = true;
            createLabel.mutate(
              { name, color },
              {
                onSuccess: (label) => {
                  attachLabel.mutate({ label });
                },
                onSettled: () => {
                  creatingRef.current = false;
                },
              },
            );
          }}
        />
      </View>
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  // flex-1
  list: { flex: 1 },
  // flex-row items-center gap-3 px-4 py-3
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  // size-3 rounded-full
  dot: { width: 12, height: 12, borderRadius: 6 },
  // flex-1 text-base
  rowLabel: { flex: 1, fontSize: 16 },
  // px-3 py-8 items-center
  empty: { paddingHorizontal: 12, paddingVertical: 32, alignItems: "center" },
  // text-sm text-center
  emptyText: { fontSize: 14, textAlign: "center" },
  sheetHeader: {
    paddingHorizontal: 16,
    paddingTop: 12,
    paddingBottom: 8,
    gap: 8,
  },
  sheetTitle: { fontSize: 18, fontWeight: "600" },
  search: { fontSize: 15 },
  sheetList: { height: 420 },
});
