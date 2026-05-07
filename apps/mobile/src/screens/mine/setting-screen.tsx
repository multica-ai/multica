import Constants from "expo-constants";
import { Image, StyleSheet, Text, View } from "react-native";
import { useNavigation } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { useMobileLogout } from "../../auth/use-mobile-logout";
import { Button, Screen } from "../../components/ui/primitives";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { colors, radii, spacing } from "../../theme/tokens";
import appIcon from "../../../assets/icon.png";

type SettingNavigation = NativeStackNavigationProp<RootStackParamList>;

export function SettingScreen() {
  const navigation = useNavigation<SettingNavigation>();
  const logout = useMobileLogout();
  const version = Constants.expoConfig?.version ?? "unknown";

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar onBack={() => navigation.goBack()} title="Setting" />
      <View style={styles.content}>
        <View style={styles.appInfo}>
          <Image
            accessibilityIgnoresInvertColors
            source={appIcon}
            style={styles.icon}
          />
          <Text style={styles.version}>Version {version}</Text>
        </View>
        <View style={styles.footer}>
          <Button onPress={logout} style={styles.logoutButton} variant="secondary">
            Log out
          </Button>
        </View>
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  content: {
    flex: 1,
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.xl,
  },
  appInfo: {
    alignItems: "center",
    gap: spacing.md,
  },
  icon: {
    borderRadius: radii.md,
    height: 96,
    width: 96,
  },
  version: {
    color: colors.mutedForeground,
    fontSize: 14,
    fontWeight: "500",
  },
  footer: {
    alignItems: "center",
    marginBottom: spacing.lg,
    marginTop: "auto",
  },
  logoutButton: {
    width: "60%",
  },
});
