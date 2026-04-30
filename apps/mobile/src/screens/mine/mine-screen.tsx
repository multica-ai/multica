import { Image, Pressable, StyleSheet, Text, View } from "react-native";
import { useNavigation } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { useAuthStore } from "@multica/core/auth";
import { useInboxList } from "@multica/core/inbox";
import { Bot, Inbox, Server } from "lucide-react-native";
import { Button, Screen } from "../../components/ui/primitives";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";

type MineNavigation = NativeStackNavigationProp<RootStackParamList>;

const readOnlyEntries = [
  { label: "Runtimes", route: "Runtimes", icon: Server },
  { label: "Agents", route: "Agents", icon: Bot },
  { label: "Inbox", route: "Inbox", icon: Inbox },
] as const satisfies ReadonlyArray<{
  label: string;
  route: keyof Pick<RootStackParamList, "Runtimes" | "Agents" | "Inbox">;
  icon: typeof Server;
}>;

export function MineScreen() {
  const navigation = useNavigation<MineNavigation>();
  const { workspace } = useMobileWorkspace();
  const user = useAuthStore((state) => state.user);
  const logout = useAuthStore((state) => state.logout);
  const { data: inboxItems = [] } = useInboxList(workspace.id);
  const displayName = user?.name || user?.email || "User";
  const initial = displayName.trim().charAt(0).toUpperCase() || "U";
  const hasUnreadInbox = inboxItems.some((item) => !item.read && !item.archived);

  return (
    <Screen>
      <View style={styles.content}>
        <View style={[styles.card, styles.profileCard]}>
          {user?.avatar_url ? (
            <Image
              accessibilityIgnoresInvertColors
              source={{ uri: user.avatar_url }}
              style={styles.avatar}
            />
          ) : (
            <View style={styles.avatarFallback}>
              <Text style={styles.avatarInitial}>{initial}</Text>
            </View>
          )}
          <View style={styles.profileText}>
            <Text numberOfLines={1} style={styles.name}>
              {displayName}
            </Text>
            <Text numberOfLines={1} style={styles.email}>
              {user?.email}
            </Text>
          </View>
        </View>
        <View style={styles.card}>
          <Text style={styles.sectionTitle}>Read-only views</Text>
          <View style={styles.entryRow}>
            {readOnlyEntries.slice(0, 4).map((entry) => {
              const Icon = entry.icon;
              return (
                <Pressable
                  accessibilityRole="button"
                  key={entry.route}
                  onPress={() => navigation.navigate(entry.route)}
                  style={({ pressed }) => [
                    styles.entryItem,
                    pressed && styles.entryItemPressed,
                  ]}
                >
                  <View style={styles.entryIconWrap}>
                    <Icon color={colors.foreground} size={22} />
                    {entry.route === "Inbox" && hasUnreadInbox ? (
                      <View style={styles.unreadDot} />
                    ) : null}
                  </View>
                  <Text numberOfLines={1} style={styles.entryLabel}>
                    {entry.label}
                  </Text>
                </Pressable>
              );
            })}
          </View>
        </View>
      </View>
      <View style={styles.footer}>
        <Button onPress={logout} style={styles.logoutButton} variant="secondary">
          Log out
        </Button>
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  content: {
    flex: 1,
  },
  card: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.sm,
    marginBottom: spacing.md,
    padding: spacing.md,
  },
  profileCard: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.md,
  },
  avatar: {
    backgroundColor: colors.muted,
    borderRadius: 24,
    height: 48,
    width: 48,
  },
  avatarFallback: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: 24,
    height: 48,
    justifyContent: "center",
    width: 48,
  },
  avatarInitial: {
    color: colors.foreground,
    fontSize: 18,
    fontWeight: "600",
  },
  profileText: {
    flex: 1,
    minWidth: 0,
  },
  name: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "500",
  },
  email: {
    color: colors.mutedForeground,
    fontSize: 14,
  },
  sectionTitle: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  entryRow: {
    flexDirection: "row",
    gap: spacing.sm,
  },
  entryItem: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flex: 1,
    gap: spacing.xs,
    minWidth: 0,
    paddingHorizontal: spacing.xs,
    paddingVertical: spacing.md,
  },
  entryItemPressed: {
    opacity: 0.75,
  },
  entryIconWrap: {
    alignItems: "center",
    height: 24,
    justifyContent: "center",
    position: "relative",
    width: 24,
  },
  unreadDot: {
    backgroundColor: colors.destructive,
    borderColor: colors.muted,
    borderRadius: 5,
    borderWidth: 1,
    height: 10,
    position: "absolute",
    right: 0,
    top: 0,
    width: 10,
  },
  entryLabel: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  footer: {
    alignItems: "center",
    marginBottom: spacing.lg,
  },
  logoutButton: {
    width: "60%",
  },
});
