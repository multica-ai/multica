import Constants from "expo-constants";
import { useCallback, useState } from "react";
import { Alert, AppState, Image, Platform, Pressable, StyleSheet, Switch, Text, View } from "react-native";
import { useFocusEffect, useNavigation } from "@react-navigation/native";
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
import {
  getMobileNotificationPermissionStatus,
  openMobileNotificationSettings,
  requestMobileNotificationPermission,
  type MobileNotificationPermissionState,
} from "../../push/mobile-notification-permissions";
import { syncCurrentMobilePushRegistration } from "../../push/mobile-push-registration";
import { colors, radii, spacing } from "../../theme/tokens";
import appIcon from "../../../assets/icon-android.png";

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
  const [notificationPermission, setNotificationPermission] =
    useState<MobileNotificationPermissionState | null>(null);
  const [notificationPermissionUpdating, setNotificationPermissionUpdating] = useState(false);
  const notificationPermissionSupported = Platform.OS === "android" || Platform.OS === "ios";

  const refreshNotificationPermission = useCallback(async () => {
    if (!notificationPermissionSupported) return null;
    const next = await getMobileNotificationPermissionStatus();
    setNotificationPermission(next);
    return next;
  }, [notificationPermissionSupported]);

  useFocusEffect(
    useCallback(() => {
      if (!notificationPermissionSupported) return undefined;
      let mounted = true;

      const refresh = async () => {
        const next = await getMobileNotificationPermissionStatus();
        if (mounted) setNotificationPermission(next);
      };

      void refresh();
      const subscription = AppState.addEventListener("change", (state) => {
        if (state === "active") void refresh();
      });

      return () => {
        mounted = false;
        subscription.remove();
      };
    }, [notificationPermissionSupported]),
  );

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

  async function requestNotificationPermission() {
    setNotificationPermissionUpdating(true);
    try {
      const next = await requestMobileNotificationPermission();
      setNotificationPermission(next);
      if (next.granted) {
        await syncCurrentMobilePushRegistration();
      }
      return next;
    } catch {
      Alert.alert(
        t("settings.notification_permission_error_title"),
        t("settings.notification_permission_error_body"),
      );
      return refreshNotificationPermission();
    } finally {
      setNotificationPermissionUpdating(false);
    }
  }

  async function handleAndroidNotificationToggle(nextEnabled: boolean) {
    if (notificationPermissionUpdating) return;
    if (nextEnabled) {
      const next = await requestNotificationPermission();
      if (next && !next.granted && !next.canRequest) {
        Alert.alert(
          t("settings.notification_permission_settings_title"),
          t("settings.notification_permission_settings_body"),
          [
            { text: t("common.cancel"), style: "cancel" },
            {
              text: t("settings.open_system_settings"),
              onPress: () => void openMobileNotificationSettings(),
            },
          ],
        );
      }
      return;
    }

    Alert.alert(
      t("settings.notification_permission_settings_title"),
      t("settings.notification_permission_settings_body"),
      [
        { text: t("common.cancel"), style: "cancel" },
        {
          text: t("settings.open_system_settings"),
          onPress: () => void openMobileNotificationSettings(),
        },
      ],
    );
  }

  async function handleIosNotificationPress() {
    if (notificationPermissionUpdating) return;
    if (notificationPermission?.status === "undetermined" && notificationPermission.canRequest) {
      await requestNotificationPermission();
      return;
    }
    await openMobileNotificationSettings();
  }

  const notificationStatusLabel = getNotificationStatusLabel(notificationPermission);

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
        {Platform.OS === "android" ? (
          <View style={styles.section}>
            <Text style={styles.sectionTitle}>{t("settings.notifications")}</Text>
            <View style={styles.settingRow}>
              <View style={styles.settingRowText}>
                <Text style={styles.rowTitle}>{t("settings.notification_permission")}</Text>
                <Text style={styles.rowDetail}>
                  {t("settings.notification_permission_android_detail")}
                </Text>
              </View>
              <Switch
                accessibilityLabel={t("settings.notification_permission")}
                disabled={notificationPermissionUpdating || !notificationPermission}
                onValueChange={(value) => void handleAndroidNotificationToggle(value)}
                thumbColor={colors.card}
                trackColor={{ false: colors.border, true: colors.foreground }}
                value={notificationPermission?.granted === true}
              />
            </View>
          </View>
        ) : null}
        {Platform.OS === "ios" ? (
          <View style={styles.section}>
            <Text style={styles.sectionTitle}>{t("settings.notifications")}</Text>
            <Pressable
              accessibilityRole="button"
              disabled={notificationPermissionUpdating}
              onPress={() => void handleIosNotificationPress()}
              style={({ pressed }) => [
                styles.settingRow,
                pressed && styles.buttonPressed,
                notificationPermissionUpdating && styles.rowDisabled,
              ]}
            >
              <View style={styles.settingRowText}>
                <Text style={styles.rowTitle}>{t("settings.notification_permission")}</Text>
                <Text style={styles.rowDetail}>
                  {t("settings.notification_permission_ios_detail")}
                </Text>
              </View>
              <View style={styles.statusColumn}>
                <Text style={styles.permissionStatus}>{t(notificationStatusLabel)}</Text>
                <Text style={styles.openSettingsText}>{t("settings.open_system_settings")}</Text>
              </View>
            </Pressable>
          </View>
        ) : null}
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
  openSettingsText: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  permissionStatus: {
    color: colors.foreground,
    fontSize: 13,
    fontWeight: "600",
  },
  rowDetail: {
    color: colors.mutedForeground,
    fontSize: 13,
    lineHeight: 18,
  },
  rowDisabled: {
    opacity: 0.7,
  },
  rowTitle: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "500",
  },
  settingRow: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
    minHeight: 64,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  settingRowText: {
    flex: 1,
    gap: spacing.xs,
  },
  statusColumn: {
    alignItems: "flex-end",
    gap: spacing.xs,
  },
});

function getNotificationStatusLabel(permission: MobileNotificationPermissionState | null) {
  switch (permission?.status) {
    case "granted":
      return "settings.notification_permission_enabled";
    case "denied":
      return "settings.notification_permission_disabled";
    case "undetermined":
      return "settings.notification_permission_not_requested";
    default:
      return "settings.notification_permission_unknown";
  }
}
