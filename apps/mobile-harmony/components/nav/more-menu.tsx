/**
 * Popover opened by the More tab press — HarmonyOS port of
 * apps/mobile/components/nav/more-tab-dropdown.tsx. Same content order
 * (user card → workspace card → Pinned/Issues/Projects rows), rendered as
 * a right-aligned card anchored above the tab bar. The @rn-primitives
 * dropdown is replaced by plain Views: the popover is always anchored to
 * the More tab, so no generic anchoring machinery is needed.
 */
import React from "react";
import { Image, Pressable, StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "@/lib/safe-area";
import type { User, Workspace } from "@multica/core/types";
import { Icon } from "@/components/ui/icon";
import { useThemeColors } from "@/lib/use-theme-colors";
import { TAB_BAR_HEIGHT } from "@/src/navigation/tabs";

export type MoreMenuPath = "pins" | "issues" | "projects";

const MENU_ROWS: { path: MoreMenuPath; label: string; icon: string }[] = [
  { path: "pins", label: "Pinned", icon: "pin" },
  { path: "issues", label: "Issues", icon: "list" },
  { path: "projects", label: "Projects", icon: "albums" },
];

export function MoreMenu({
  user,
  currentWorkspace,
  canSwitch,
  activePaths,
  onPressUser,
  onPressWorkspace,
  onPressPath,
  onClose,
}: {
  user: User | null;
  currentWorkspace: Workspace | undefined;
  canSwitch: boolean;
  activePaths: Set<string>;
  onPressUser: () => void;
  onPressWorkspace: () => void;
  onPressPath: (path: MoreMenuPath) => void;
  onClose: () => void;
}) {
  const c = useThemeColors();
  const insets = useSafeAreaInsets();
  const s = styles(c);

  const initial = (user?.name ?? user?.email ?? "U").charAt(0).toUpperCase();

  return (
    <View style={StyleSheet.absoluteFill} pointerEvents="auto">
      <Pressable accessibilityLabel="Close menu" style={s.backdrop} onPress={onClose} />
      <View
        style={[
          s.card,
          { backgroundColor: c.popover, bottom: TAB_BAR_HEIGHT + insets.bottom },
        ]}
      >
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Account settings"
          style={s.row12}
          onPress={onPressUser}
        >
          {user?.avatar_url ? (
            <Image source={{ uri: user.avatar_url }} style={s.avatar} />
          ) : (
            <View style={[s.avatar, s.avatarFallback, { backgroundColor: c.muted }]}>
              <Text style={[s.avatarInitial, { color: c.mutedForeground }]}>{initial}</Text>
            </View>
          )}
          <View style={s.rowMain}>
            <Text style={[s.rowTitle, { color: c.foreground }]} numberOfLines={1}>
              {user?.name ?? "—"}
            </Text>
            {user?.email ? (
              <Text style={[s.rowSubtitle, { color: c.mutedForeground }]} numberOfLines={1}>
                {user.email}
              </Text>
            ) : null}
          </View>
          <Icon name="chevron-forward" size={12} color={c.mutedForeground} />
        </Pressable>

        <View style={[s.separator, { backgroundColor: c.border }]} />

        <Pressable
          accessibilityRole="button"
          accessibilityLabel={canSwitch ? "Switch workspace" : "Workspace"}
          style={s.row12}
          onPress={onPressWorkspace}
          disabled={!canSwitch}
        >
          {currentWorkspace?.avatar_url ? (
            <Image source={{ uri: currentWorkspace.avatar_url }} style={s.avatar} />
          ) : (
            <View style={[s.avatar, s.avatarFallback, { backgroundColor: c.brand }]}>
              <Text style={s.workspaceInitial}>
                {(currentWorkspace?.name ?? "W").charAt(0).toUpperCase()}
              </Text>
            </View>
          )}
          <View style={s.rowMain}>
            <Text style={[s.rowTitle, { color: c.foreground }]} numberOfLines={1}>
              {currentWorkspace?.name ?? "Workspace"}
            </Text>
          </View>
          {canSwitch ? <Icon name="chevron-forward" size={12} color={c.mutedForeground} /> : null}
        </Pressable>

        <View style={[s.separator, { backgroundColor: c.border }]} />

        {MENU_ROWS.map((row) => (
          <Pressable
            key={row.path}
            accessibilityRole="button"
            accessibilityLabel={row.label}
            style={[s.row9, activePaths.has(row.path) && { backgroundColor: c.secondary }]}
            onPress={() => onPressPath(row.path)}
          >
            <Icon name={row.icon} size={18} color={c.foreground} />
            <Text style={[s.menuLabel, { color: c.foreground }]}>{row.label}</Text>
          </Pressable>
        ))}
      </View>
    </View>
  );
}

const styles = (c: ReturnType<typeof useThemeColors>) =>
  StyleSheet.create({
    backdrop: { flex: 1, backgroundColor: "transparent" },
    card: {
      position: "absolute",
      right: 8,
      width: 288,
      borderRadius: 10,
      padding: 8,
      shadowColor: "#000",
      shadowOpacity: 0.15,
      shadowRadius: 12,
      shadowOffset: { width: 0, height: 4 },
      elevation: 8,
    },
    row12: {
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      height: 48,
      borderRadius: 8,
      paddingHorizontal: 4,
    },
    row9: {
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      height: 36,
      borderRadius: 8,
      paddingHorizontal: 4,
    },
    rowMain: { flex: 1, minWidth: 0 },
    rowTitle: { fontSize: 14, fontWeight: "500" },
    rowSubtitle: { fontSize: 12 },
    menuLabel: { fontSize: 14 },
    avatar: { width: 32, height: 32, borderRadius: 16 },
    avatarFallback: { alignItems: "center", justifyContent: "center" },
    avatarInitial: { fontSize: 12, fontWeight: "500" },
    workspaceInitial: { fontSize: 12, fontWeight: "600", color: "#fff" },
    separator: { height: 1, marginVertical: 4 },
  });
