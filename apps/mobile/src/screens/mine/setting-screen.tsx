import Constants from "expo-constants";
import { useState } from "react";
import { Image, Pressable, StyleSheet, Text, View } from "react-native";
import { useNavigation } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { useTranslation } from "react-i18next";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import type { SupportedLocale } from "@multica/core/i18n";
import { useMobileLogout } from "../../auth/use-mobile-logout";
import { Button, Screen } from "../../components/ui/primitives";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import { useMobileLocale } from "../../i18n/mobile-locale";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { colors, radii, spacing } from "../../theme/tokens";
import appIcon from "../../../assets/icon.png";

type SettingNavigation = NativeStackNavigationProp<RootStackParamList>;

const languageOptions: Array<{ labelKey: string; value: SupportedLocale }> = [
  { labelKey: "common.english", value: "en" },
  { labelKey: "common.chinese", value: "zh-Hans" },
];

export function SettingScreen() {
  const navigation = useNavigation<SettingNavigation>();
  const logout = useMobileLogout();
  const user = useAuthStore((state) => state.user);
  const { locale, setLocale } = useMobileLocale();
  const { t } = useTranslation();
  const version = Constants.expoConfig?.version ?? "unknown";
  const [syncError, setSyncError] = useState(false);

  async function handleLanguageChange(next: SupportedLocale) {
    if (next === locale) return;
    setSyncError(false);
    setLocale(next);
    let syncFailed = false;
    if (user) {
      try {
        await api.updateMe({ language: next });
      } catch {
        syncFailed = true;
      }
    }
    if (syncFailed) {
      setSyncError(true);
      return;
    }
  }

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar onBack={() => navigation.goBack()} title={t("settings.title")} />
      <View style={styles.content}>
        <View style={styles.appInfo}>
          <Image
            accessibilityIgnoresInvertColors
            source={appIcon}
            style={styles.icon}
          />
          <Text style={styles.version}>{t("settings.version", { version })}</Text>
        </View>
        <View style={styles.section}>
          <Text style={styles.sectionTitle}>{t("common.language")}</Text>
          <View style={styles.languageRow}>
            {languageOptions.map((option) => {
              const active = locale === option.value;
              return (
                <Pressable
                  accessibilityRole="button"
                  key={option.value}
                  onPress={() => void handleLanguageChange(option.value)}
                  style={({ pressed }) => [
                    styles.languageButton,
                    active && styles.languageButtonActive,
                    pressed && styles.buttonPressed,
                  ]}
                >
                  <Text
                    style={[
                      styles.languageButtonText,
                      active && styles.languageButtonTextActive,
                    ]}
                  >
                    {t(option.labelKey)}
                  </Text>
                </Pressable>
              );
            })}
          </View>
          {syncError ? (
            <Text style={styles.syncError}>{t("settings.sync_failed")}</Text>
          ) : null}
        </View>
        <View style={styles.footer}>
          <Button onPress={logout} style={styles.logoutButton} variant="secondary">
            {t("common.log_out")}
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
  buttonPressed: {
    opacity: 0.75,
  },
  icon: {
    borderRadius: radii.md,
    height: 96,
    width: 96,
  },
  languageButton: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flex: 1,
    minHeight: 44,
    justifyContent: "center",
    paddingHorizontal: spacing.md,
  },
  languageButtonActive: {
    backgroundColor: colors.foreground,
    borderColor: colors.foreground,
  },
  languageButtonText: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  languageButtonTextActive: {
    color: colors.background,
  },
  languageRow: {
    flexDirection: "row",
    gap: spacing.sm,
  },
  section: {
    gap: spacing.md,
    marginTop: spacing.xl,
  },
  sectionTitle: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "600",
  },
  syncError: {
    color: colors.destructive,
    fontSize: 13,
    lineHeight: 18,
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
