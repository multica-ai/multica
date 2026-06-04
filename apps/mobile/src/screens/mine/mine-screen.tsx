import { Image, Pressable, StyleSheet, Text, View } from "react-native";
import { useNavigation } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "@multica/core/auth";
import { useInboxList } from "@multica/core/inbox";
import { BookOpen, Bot, ChevronRight, Inbox, Server, Settings, Users, Zap } from "lucide-react-native";
import { Screen } from "../../components/ui/primitives";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";

type MineNavigation = NativeStackNavigationProp<RootStackParamList>;
type MineEntry = {
  labelKey: string;
  route: keyof Pick<RootStackParamList, "Runtimes" | "Agents" | "Squads" | "Inbox" | "Wiki" | "Autopilots" | "Setting">;
  icon: typeof Server;
};

const primaryEntries = [
  { labelKey: "mine.agents", route: "Agents", icon: Bot },
  { labelKey: "mine.squads", route: "Squads", icon: Users },
  { labelKey: "mine.inbox", route: "Inbox", icon: Inbox },
  { labelKey: "mine.autopilots", route: "Autopilots", icon: Zap },
] as const satisfies ReadonlyArray<MineEntry>;

const secondaryEntries = [
  { labelKey: "mine.runtimes", route: "Runtimes", icon: Server },
  { labelKey: "mine.wiki", route: "Wiki", icon: BookOpen },
  { labelKey: "mine.setting", route: "Setting", icon: Settings },
] as const satisfies ReadonlyArray<MineEntry>;

export function MineScreen() {
  const navigation = useNavigation<MineNavigation>();
  const { t } = useTranslation();
  const { workspace } = useMobileWorkspace();
  const user = useAuthStore((state) => state.user);
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
        <View style={styles.primaryGrid}>
          {primaryEntries.map((entry) => {
            const Icon = entry.icon;
            return (
              <Pressable
                accessibilityRole="button"
                key={entry.route}
                onPress={() => navigation.navigate(entry.route)}
                style={({ pressed }) => [
                  styles.primaryEntry,
                  pressed && styles.entryPressed,
                ]}
              >
                <View style={styles.primaryEntryIconWrap}>
                  <Icon color={colors.foreground} size={20} />
                  {entry.route === "Inbox" && hasUnreadInbox ? (
                    <View style={styles.unreadDot} />
                  ) : null}
                </View>
                <Text
                  adjustsFontSizeToFit
                  minimumFontScale={0.82}
                  numberOfLines={1}
                  style={styles.primaryEntryLabel}
                >
                  {t(entry.labelKey)}
                </Text>
              </Pressable>
            );
          })}
        </View>
        <View style={styles.moreSection}>
          <View style={styles.sectionTitleRow}>
            <View style={styles.sectionTitleAccent} />
            <Text style={styles.sectionTitle}>{t("mine.more")}</Text>
          </View>
          <View style={styles.secondaryList}>
            {secondaryEntries.map((entry) => {
              const Icon = entry.icon;
              return (
                <Pressable
                  accessibilityRole="button"
                  key={entry.route}
                  onPress={() => navigation.navigate(entry.route)}
                  style={({ pressed }) => [
                    styles.secondaryEntry,
                    pressed && styles.entryPressed,
                  ]}
                >
                  <View style={styles.secondaryEntryIconWrap}>
                    <Icon color={colors.foreground} size={18} />
                  </View>
                  <Text numberOfLines={1} style={styles.secondaryEntryLabel}>
                    {t(entry.labelKey)}
                  </Text>
                  <ChevronRight color={colors.mutedForeground} size={16} strokeWidth={2} />
                </Pressable>
              );
            })}
          </View>
        </View>
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
    fontSize: 15,
    fontWeight: "700",
    lineHeight: 20,
  },
  sectionTitleRow: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
    minHeight: 24,
  },
  sectionTitleAccent: {
    backgroundColor: colors.foreground,
    borderRadius: 2,
    height: 16,
    width: 3,
  },
  primaryGrid: {
    flexWrap: "wrap",
    flexDirection: "row",
    gap: spacing.sm,
    marginBottom: spacing.sm,
  },
  primaryEntry: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flexBasis: "47%",
    flexGrow: 1,
    gap: spacing.xs,
    justifyContent: "center",
    minHeight: 68,
    minWidth: 0,
    paddingHorizontal: spacing.md,
    paddingVertical: 10,
  },
  entryPressed: {
    opacity: 0.75,
  },
  primaryEntryIconWrap: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderRadius: radii.md,
    height: 30,
    justifyContent: "center",
    position: "relative",
    width: 30,
  },
  unreadDot: {
    backgroundColor: colors.destructive,
    borderColor: colors.card,
    borderRadius: 5,
    borderWidth: 1,
    height: 10,
    position: "absolute",
    right: 2,
    top: 2,
    width: 10,
  },
  primaryEntryLabel: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
    lineHeight: 18,
    textAlign: "center",
    width: "100%",
  },
  secondaryList: {
    gap: spacing.sm,
  },
  moreSection: {
    gap: spacing.sm,
    marginTop: 0,
  },
  secondaryEntry: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flexDirection: "row",
    gap: spacing.sm,
    minHeight: 50,
    paddingHorizontal: spacing.md,
  },
  secondaryEntryIconWrap: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderRadius: radii.md,
    height: 30,
    justifyContent: "center",
    width: 30,
  },
  secondaryEntryLabel: {
    color: colors.foreground,
    flex: 1,
    fontSize: 14,
    fontWeight: "500",
    lineHeight: 18,
  },
});
