import { useCallback } from "react";
import {
  View,
  Text,
  TouchableOpacity,
  ScrollView,
  Alert,
  StyleSheet,
} from "react-native";
import { useRouter } from "expo-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { useAuthStore, useWorkspaceStore } from "@/lib/providers";
import { Avatar } from "@/components/Avatar";
import { ScreenHeader } from "@/components/ScreenHeader";
import { colors } from "@/lib/theme";

export default function SettingsScreen() {
  const router = useRouter();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const switchWorkspace = useWorkspaceStore((s) => s.switchWorkspace);
  const { data: workspaces = [], isLoading: isLoadingWorkspaces } = useQuery(
    workspaceListOptions(),
  );

  const handleSwitchWorkspace = useCallback(
    async (nextWorkspace: Workspace) => {
      if (nextWorkspace.id === workspace?.id) return;

      switchWorkspace(nextWorkspace);
      await qc.invalidateQueries();
      router.replace("/(app)/(home)");
    },
    [workspace?.id, switchWorkspace, qc, router],
  );

  const handleLogout = useCallback(() => {
    Alert.alert("Log out", "Are you sure you want to log out?", [
      { text: "Cancel", style: "cancel" },
      {
        text: "Log out",
        style: "destructive",
        onPress: () => {
          useAuthStore.getState().logout();
          // Auth guard in root _layout will redirect to login
        },
      },
    ]);
  }, []);

  return (
    <View style={styles.container}>
      <ScreenHeader
        title="Settings"
        subtitle="Workspace and account"
        backLabel="More"
        fallback="/(app)/(more)"
      />
      <ScrollView contentContainerStyle={styles.content}>
        <View style={styles.section}>
          <Text style={styles.sectionTitle}>Profile</Text>
          <View style={styles.profileCard}>
            <Avatar name={user?.name} avatarUrl={user?.avatar_url} size={48} />
            <View style={styles.profileInfo}>
              <Text style={styles.profileName}>{user?.name ?? "—"}</Text>
              <Text style={styles.profileEmail}>{user?.email ?? "—"}</Text>
            </View>
          </View>
        </View>

        <View style={styles.section}>
          <Text style={styles.sectionTitle}>Workspace</Text>
          <View style={styles.card}>
            <View style={styles.row}>
              <Text style={styles.rowLabel}>Name</Text>
              <Text style={styles.rowValue}>{workspace?.name ?? "—"}</Text>
            </View>
            <View style={styles.divider} />
            <View style={styles.row}>
              <Text style={styles.rowLabel}>Slug</Text>
              <Text style={styles.rowValue}>{workspace?.slug ?? "—"}</Text>
            </View>
            <View style={styles.divider} />
            <View style={styles.row}>
              <Text style={styles.rowLabel}>Issue Prefix</Text>
              <Text style={styles.rowValue}>
                {workspace?.issue_prefix ?? "—"}
              </Text>
            </View>
          </View>
        </View>

        <View style={styles.section}>
          <Text style={styles.sectionTitle}>Switch Workspace</Text>
          <View style={styles.card}>
            {isLoadingWorkspaces ? (
              <View style={styles.row}>
                <Text style={styles.rowLabel}>Loading...</Text>
              </View>
            ) : workspaces.length === 0 ? (
              <View style={styles.row}>
                <Text style={styles.rowLabel}>No workspaces</Text>
              </View>
            ) : (
              workspaces.map((item, index) => {
                const isCurrent = item.id === workspace?.id;
                return (
                  <View key={item.id}>
                    <TouchableOpacity
                      style={styles.workspaceRow}
                      onPress={() => handleSwitchWorkspace(item)}
                      disabled={isCurrent}
                      activeOpacity={0.7}
                    >
                      <View style={styles.workspaceRowText}>
                        <Text style={styles.rowLabel}>{item.name}</Text>
                        <Text style={styles.workspaceSlug}>{item.slug}</Text>
                      </View>
                      <Text
                        style={[
                          styles.workspaceStatus,
                          isCurrent && styles.workspaceStatusActive,
                        ]}
                      >
                        {isCurrent ? "Current" : "Switch"}
                      </Text>
                    </TouchableOpacity>
                    {index < workspaces.length - 1 ? (
                      <View style={styles.divider} />
                    ) : null}
                  </View>
                );
              })
            )}
          </View>
        </View>

        <View style={styles.section}>
          <Text style={styles.sectionTitle}>About</Text>
          <View style={styles.card}>
            <View style={styles.row}>
              <Text style={styles.rowLabel}>App Version</Text>
              <Text style={styles.rowValue}>0.0.1 (MVP)</Text>
            </View>
          </View>
        </View>

        <View style={styles.section}>
          <TouchableOpacity
            style={styles.logoutButton}
            onPress={handleLogout}
            activeOpacity={0.7}
          >
            <Text style={styles.logoutText}>Log out</Text>
          </TouchableOpacity>
        </View>
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.background,
  },
  content: {
    paddingBottom: 40,
  },
  section: {
    paddingHorizontal: 16,
    paddingTop: 24,
  },
  sectionTitle: {
    fontSize: 13,
    fontWeight: "600",
    color: colors.mutedForeground,
    textTransform: "uppercase",
    letterSpacing: 0.5,
    marginBottom: 10,
  },
  profileCard: {
    flexDirection: "row",
    alignItems: "center",
    padding: 16,
    backgroundColor: colors.secondary,
    borderRadius: 8,
    gap: 14,
  },
  profileInfo: {
    flex: 1,
  },
  profileName: {
    fontSize: 17,
    fontWeight: "600",
    color: colors.foreground,
    marginBottom: 2,
  },
  profileEmail: {
    fontSize: 14,
    color: colors.mutedForeground,
  },
  card: {
    backgroundColor: colors.secondary,
    borderRadius: 8,
    overflow: "hidden",
  },
  row: {
    flexDirection: "row",
    justifyContent: "space-between",
    alignItems: "center",
    paddingHorizontal: 16,
    paddingVertical: 14,
  },
  rowLabel: {
    fontSize: 15,
    color: colors.foreground,
  },
  rowValue: {
    fontSize: 15,
    color: colors.mutedForeground,
    flexShrink: 1,
    textAlign: "right",
    marginLeft: 12,
  },
  divider: {
    height: StyleSheet.hairlineWidth,
    backgroundColor: colors.border,
    marginLeft: 16,
  },
  workspaceRow: {
    flexDirection: "row",
    justifyContent: "space-between",
    alignItems: "center",
    paddingHorizontal: 16,
    paddingVertical: 14,
    gap: 12,
  },
  workspaceRowText: {
    flex: 1,
  },
  workspaceSlug: {
    fontSize: 13,
    color: colors.mutedForeground,
    marginTop: 2,
  },
  workspaceStatus: {
    fontSize: 14,
    fontWeight: "500",
    color: colors.foreground,
  },
  workspaceStatusActive: {
    color: colors.success,
  },
  logoutButton: {
    backgroundColor: colors.secondary,
    borderRadius: 8,
    paddingVertical: 14,
    alignItems: "center",
  },
  logoutText: {
    fontSize: 16,
    fontWeight: "500",
    color: colors.destructive,
  },
});
