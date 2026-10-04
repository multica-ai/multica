/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/switch-workspace.tsx.
 *
 * Lists every workspace the user belongs to; the current one is disabled
 * with a checkmark. Tapping a non-current row triggers a confirm — only
 * after the user confirms do we write the workspace store and notify the
 * caller. iOS presented this as a formSheet with a native Alert.alert; on
 * this stack it is a pushed screen with an RN Alert.confirm, same copy.
 *
 * Why a confirm step (carried over from the iOS file): instant switching
 * had no friction against fat-finger taps, and the user lost their entire
 * navigation context (tabs, scroll position) with one accidental tap.
 *
 * The store write alone is not the navigation: after setCurrentWorkspace
 * the shell's session effect observes the slug change (or onSwitched lets
 * the shell drive the stack explicitly).
 */
import React from "react";
import { ActivityIndicator, Alert, ScrollView, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type { Workspace } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { WorkspaceAvatar } from "@/components/workspace/workspace-avatar";
import { Icon } from "@/components/ui/icon";
import { IconButton } from "@/components/ui/icon-button";
import { SafeAreaView } from "@/lib/safe-area";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { useWorkspaceStore } from "@/data/workspace-store";
import type { ThemeColors } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

export function SwitchWorkspaceScreen({
  onBack,
  onSwitched,
}: {
  onBack?: () => void;
  onSwitched?: () => void;
}) {
  const c = useThemeColors();
  const s = styles(c);
  const activeSlug = useWorkspaceStore((state) => state.currentWorkspaceSlug);
  const setCurrentWorkspace = useWorkspaceStore(
    (state) => state.setCurrentWorkspace,
  );
  const { data, isLoading } = useQuery(workspaceListOptions());

  const onSelect = (ws: Workspace) => {
    if (ws.slug === activeSlug) return;
    Alert.alert("Switch workspace", `Switch to "${ws.name}"?`, [
      { text: "Cancel", style: "cancel" },
      {
        text: "Switch",
        onPress: () => {
          void (async () => {
            await setCurrentWorkspace(ws.id, ws.slug);
            onSwitched?.();
          })();
        },
      },
    ]);
  };

  return (
    <SafeAreaView edges={["top"]} style={s.screen}>
      <View style={s.titleRow}>
        {onBack ? (
          <IconButton name="chevron-back" onPress={onBack} accessibilityLabel="Back" />
        ) : null}
        <Text style={s.title}>Switch workspace</Text>
      </View>
      {isLoading ? (
        <View style={s.loadingWrap}>
          <ActivityIndicator color={c.mutedForeground} />
        </View>
      ) : (
        <ScrollView
          style={s.flex}
          showsVerticalScrollIndicator={false}
        >
          {(data ?? []).map((ws) => (
            <WorkspaceRow
              key={ws.id}
              workspace={ws}
              active={ws.slug === activeSlug}
              onPress={() => onSelect(ws)}
              iconTint={c.foreground}
              c={c}
              s={s}
            />
          ))}
        </ScrollView>
      )}
    </SafeAreaView>
  );
}

function WorkspaceRow({
  workspace,
  active,
  onPress,
  iconTint,
  c,
  s,
}: {
  workspace: Workspace;
  active: boolean;
  onPress: () => void;
  iconTint: string;
  c: ThemeColors;
  s: ReturnType<typeof styles>;
}) {
  return (
    <Pressable
      onPress={onPress}
      disabled={active}
      accessibilityLabel={
        active
          ? `${workspace.name}, current workspace`
          : `Switch to ${workspace.name}`
      }
      style={({ pressed }) => [
        s.row,
        pressed && !active ? s.pressedRow : null,
      ]}
    >
      <WorkspaceAvatar
        name={workspace.name}
        avatarUrl={workspace.avatar_url}
        size={24}
      />
      <Text
        style={[
          s.rowLabel,
          { color: c.foreground },
          active ? s.rowLabelActive : null,
        ]}
        numberOfLines={1}
      >
        {workspace.name}
      </Text>
      {active ? (
        <Icon name="checkmark" size={16} color={iconTint} />
      ) : null}
    </Pressable>
  );
}

const styles = (c: ThemeColors) =>
  StyleSheet.create({
    flex: { flex: 1 },
    screen: { flex: 1, backgroundColor: c.background },
    titleRow: {
      flexDirection: "row",
      alignItems: "center",
      height: 48,
      paddingLeft: 8,
      paddingRight: 8,
    },
    title: { fontSize: 18, fontWeight: "600", color: c.foreground },
    loadingWrap: { paddingVertical: 24, alignItems: "center" },
    row: {
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      paddingHorizontal: 16,
      paddingVertical: 12,
    },
    pressedRow: { backgroundColor: c.secondary },
    rowLabel: { flex: 1, fontSize: 14 },
    rowLabelActive: { fontWeight: "600" },
  });
