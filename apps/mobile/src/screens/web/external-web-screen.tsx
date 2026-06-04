import { useMemo, useState } from "react";
import { ActivityIndicator, Linking, Pressable, StyleSheet, Text, View } from "react-native";
import type { NativeStackScreenProps } from "@react-navigation/native-stack";
import { useTranslation } from "react-i18next";
import { ExternalLink } from "lucide-react-native";
import { WebView } from "react-native-webview";
import { EmptyState, Screen } from "../../components/ui/primitives";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { colors, radii, spacing } from "../../theme/tokens";

type Props = NativeStackScreenProps<RootStackParamList, "ExternalWeb">;

export function ExternalWebScreen({ navigation, route }: Props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const url = route.params.url;
  const safeUrl = useMemo(() => normalizeExternalUrl(url), [url]);
  const title = route.params.title || t("external_web.title");

  const openInBrowser = () => {
    if (!safeUrl) return;
    void Linking.openURL(safeUrl);
  };

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar
        onBack={() => navigation.goBack()}
        right={
          <Pressable
            accessibilityLabel={t("external_web.open_browser")}
            accessibilityRole="button"
            disabled={!safeUrl}
            onPress={openInBrowser}
            style={({ pressed }) => [
              styles.iconButton,
              !safeUrl && styles.disabled,
              pressed && safeUrl && styles.pressed,
            ]}
          >
            <ExternalLink color={colors.foreground} size={19} />
          </Pressable>
        }
        title={title}
      />
      {!safeUrl ? (
        <EmptyState
          detail={t("external_web.invalid_detail")}
          title={t("external_web.invalid_title")}
        />
      ) : failed ? (
        <View style={styles.errorWrap}>
          <EmptyState
            detail={t("external_web.failed_detail")}
            title={t("external_web.failed_title")}
          />
          <Pressable
            accessibilityRole="button"
            onPress={openInBrowser}
            style={({ pressed }) => [styles.browserButton, pressed && styles.pressed]}
          >
            <ExternalLink color={colors.primaryForeground} size={16} />
            <Text style={styles.browserButtonText}>{t("external_web.open_browser")}</Text>
          </Pressable>
        </View>
      ) : (
        <View style={styles.webWrap}>
          {loading ? (
            <View style={styles.loadingOverlay}>
              <ActivityIndicator color={colors.foreground} />
            </View>
          ) : null}
          <WebView
            onError={() => setFailed(true)}
            onHttpError={() => setFailed(true)}
            onLoadEnd={() => setLoading(false)}
            onLoadStart={() => {
              setFailed(false);
              setLoading(true);
            }}
            source={{ uri: safeUrl }}
            style={styles.webView}
          />
        </View>
      )}
    </Screen>
  );
}

function normalizeExternalUrl(value: string): string | null {
  try {
    const parsed = new URL(value);
    if (parsed.protocol !== "https:" && parsed.protocol !== "http:") return null;
    return parsed.toString();
  } catch {
    return null;
  }
}

const styles = StyleSheet.create({
  iconButton: {
    alignItems: "center",
    borderRadius: radii.md,
    height: 40,
    justifyContent: "center",
    width: 40,
  },
  disabled: {
    opacity: 0.35,
  },
  pressed: {
    opacity: 0.72,
  },
  webWrap: {
    flex: 1,
    position: "relative",
  },
  webView: {
    backgroundColor: colors.background,
    flex: 1,
  },
  loadingOverlay: {
    alignItems: "center",
    backgroundColor: colors.background,
    bottom: 0,
    justifyContent: "center",
    left: 0,
    position: "absolute",
    right: 0,
    top: 0,
    zIndex: 1,
  },
  errorWrap: {
    flex: 1,
    gap: spacing.md,
    justifyContent: "center",
    padding: spacing.xl,
  },
  browserButton: {
    alignItems: "center",
    alignSelf: "center",
    backgroundColor: colors.primary,
    borderRadius: radii.md,
    flexDirection: "row",
    gap: spacing.xs,
    minHeight: 44,
    paddingHorizontal: spacing.lg,
  },
  browserButtonText: {
    color: colors.primaryForeground,
    fontSize: 14,
    fontWeight: "600",
  },
});
