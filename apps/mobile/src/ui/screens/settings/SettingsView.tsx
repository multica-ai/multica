import { Pressable, ScrollView, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Avatar } from "../../components/Avatar";
import { Group, GroupRow } from "../../components/Group";
import { PushHeader } from "../../components/Header";
import { Screen } from "../../components/Screen";
import { Txt } from "../../components/Text";
import { useStrings } from "../../strings";
import { radius, useTheme } from "../../theme";

interface SettingsViewProps {
  user: { name: string; email: string; avatarUrl: string | null };
  workspaceName: string;
  serverHost: string;
  version: string;
  onBack: () => void;
  onNotifications: () => void;
  onSignOut: () => void;
}

function Detail({ label, value, last = false }: { label: string; value: string; last?: boolean }) {
  const palette = useTheme();
  return (
    <View style={[styles.detail, !last && { borderBottomColor: palette.separator, borderBottomWidth: StyleSheet.hairlineWidth }]}>
      <Txt variant="rowRegular">{label}</Txt>
      <Txt variant="rowRegular" tone="secondary" numberOfLines={1} style={styles.value}>
        {value}
      </Txt>
    </View>
  );
}

export function SettingsView({ user, workspaceName, serverHost, version, onBack, onNotifications, onSignOut }: SettingsViewProps) {
  const strings = useStrings();
  const palette = useTheme();
  const insets = useSafeAreaInsets();
  return (
    <Screen>
      <PushHeader title={strings.more.settings} onBack={onBack} backLabel={strings.common.back} />
      <ScrollView contentInsetAdjustmentBehavior="automatic" contentContainerStyle={[styles.content, { paddingBottom: insets.bottom + 24 }]}>
        <View style={[styles.group, styles.account, { backgroundColor: palette.grouped }]}>
          <Avatar actor={{ kind: "member", name: user.name, avatarUrl: user.avatarUrl }} size={48} />
          <View style={styles.flex}>
            <Txt variant="bodyStrong" numberOfLines={1}>
              {user.name}
            </Txt>
            <Txt variant="meta" tone="secondary" numberOfLines={1} style={styles.email}>
              {user.email}
            </Txt>
          </View>
        </View>
        <Group>
          <GroupRow icon="Bell" label={strings.notifications.row} chevron onPress={onNotifications} last />
        </Group>
        <View style={[styles.group, { backgroundColor: palette.grouped }]}>
          <Detail label={strings.more.workspace} value={workspaceName} />
          <Detail label={strings.more.server} value={serverHost} />
          <Detail label={strings.more.version} value={version} last />
        </View>
        <Pressable
          accessibilityRole="button"
          onPress={onSignOut}
          style={({ pressed }) => [styles.group, styles.signOut, { backgroundColor: palette.grouped, opacity: pressed ? 0.6 : 1 }]}
        >
          <Txt variant="row" tone="destructive">
            {strings.more.signOut}
          </Txt>
        </Pressable>
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  content: { paddingHorizontal: 16, paddingTop: 8, gap: 16 },
  group: { borderRadius: radius.group, paddingHorizontal: 16 },
  account: { flexDirection: "row", alignItems: "center", gap: 14, paddingVertical: 14 },
  flex: { flex: 1, minWidth: 0 },
  email: { marginTop: 2 },
  detail: { height: 50, flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 16 },
  value: { flexShrink: 1, textAlign: "right" },
  signOut: { height: 50, alignItems: "center", justifyContent: "center" },
});
