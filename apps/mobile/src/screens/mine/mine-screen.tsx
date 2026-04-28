import { StyleSheet, Text, View } from "react-native";
import { useAuthStore } from "@multica/core/auth";
import { Button, Screen } from "../../components/ui/primitives";
import { colors, radii, spacing } from "../../theme/tokens";

export function MineScreen() {
  const user = useAuthStore((state) => state.user);
  const logout = useAuthStore((state) => state.logout);

  return (
    <Screen>
      <View style={styles.card}>
        <Text style={styles.name}>{user?.name || user?.email}</Text>
        <Text style={styles.email}>{user?.email}</Text>
      </View>
      <View style={styles.card}>
        <Text style={styles.sectionTitle}>Read-only views</Text>
        <Text style={styles.item}>Runtimes</Text>
        <Text style={styles.item}>Agents</Text>
        <Text style={styles.item}>Inbox</Text>
      </View>
      <Button onPress={logout} variant="secondary">
        Log out
      </Button>
    </Screen>
  );
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.sm,
    marginBottom: spacing.md,
    padding: spacing.md,
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
  item: {
    color: colors.mutedForeground,
    fontSize: 14,
  },
});
