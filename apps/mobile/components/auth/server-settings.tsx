import { useState } from "react";
import { Alert, Keyboard, Platform, ScrollView, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { router } from "expo-router";
import { KeyboardAvoidingView } from "@/components/ui/keyboard-avoiding-view";
import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { useAuthStore } from "@/data/auth-store";
import { CLOUD_SERVER_CONFIG, useServerConfigStore } from "@/data/server-config-store";
import { changeServerConfig } from "@/data/server-session";
import { normalizeServerConfig, ServerUrlError } from "@/lib/server-url";
import { useT } from "@/lib/i18n";

export function ServerSettings({ recovery = false }: { recovery?: boolean }) {
  const config = useServerConfigStore((s) => s.config);
  const switching = useServerConfigStore((s) => s.switching);
  const user = useAuthStore((s) => s.user);
  const [apiUrl, setApiUrl] = useState(config.apiUrl);
  const [webUrl, setWebUrl] = useState(config.webUrl);
  const [error, setError] = useState<string | null>(null);
  const { t } = useT("settings");
  const { t: common } = useT();

  const save = async () => {
    setError(null);
    try {
      await changeServerConfig({ apiUrl, webUrl });
      Keyboard.dismiss();
      router.dismissAll();
      router.replace("/login");
    } catch {
      setError(t("server.save_failed"));
    }
  };

  const onSave = () => {
    try {
      const normalized = normalizeServerConfig({ apiUrl, webUrl });
      setApiUrl(normalized.apiUrl);
      setWebUrl(normalized.webUrl);
      if (!recovery && normalized.apiUrl === config.apiUrl && normalized.webUrl === config.webUrl) {
        router.back();
        return;
      }
      if (user && (normalized.apiUrl !== config.apiUrl || normalized.webUrl !== config.webUrl)) {
        Alert.alert(t("server.change_title"), t("server.change_message"), [
          { text: common("actions.cancel"), style: "cancel" },
          { text: t("server.save_sign_out"), onPress: () => { void save(); } },
        ]);
      } else {
        void save();
      }
    } catch (err) {
      setError(t(`server.${err instanceof ServerUrlError ? err.code : "invalid"}`));
    }
  };

  return (
    <SafeAreaView className="flex-1 bg-background">
      <KeyboardAvoidingView className="flex-1" behavior={Platform.OS === "ios" ? "padding" : undefined}>
        <ScrollView contentContainerClassName="grow px-6 py-4 gap-6" keyboardShouldPersistTaps="handled">
          <View className="flex-row items-center justify-between gap-3">
            <Text className="text-xl font-semibold flex-1">{t("server.title")}</Text>
            {!recovery ? (
              <Button variant="ghost" disabled={switching} onPress={() => router.back()}>
                <Text>{common("actions.cancel")}</Text>
              </Button>
            ) : null}
          </View>
          {recovery ? <Text className="text-sm text-destructive">{t("server.load_failed")}</Text> : null}
          <Text className="text-sm text-muted-foreground">{t("server.description")}</Text>
          <View className="gap-2">
            <Text className="font-medium">{t("server.api_url")}</Text>
            <TextField
              accessibilityLabel={t("server.api_url")}
              testID="server-api-url"
              value={apiUrl} onChangeText={setApiUrl}
              autoCapitalize="none" autoCorrect={false} keyboardType="url"
              editable={!switching} placeholder="https://multica.example.com"
            />
            <Text className="text-sm text-muted-foreground">{t("server.api_help")}</Text>
          </View>
          <View className="gap-2">
            <Text className="font-medium">{t("server.web_url")}</Text>
            <TextField
              accessibilityLabel={t("server.web_url")}
              testID="server-web-url"
              value={webUrl} onChangeText={setWebUrl}
              autoCapitalize="none" autoCorrect={false} keyboardType="url"
              editable={!switching} placeholder="https://multica.example.com"
            />
            <Text className="text-sm text-muted-foreground">{t("server.web_help")}</Text>
          </View>
          {error ? <Text accessibilityRole="alert" className="text-sm text-destructive">{error}</Text> : null}
          <Button testID="server-save" disabled={switching} onPress={onSave}>
            <Text>{switching ? t("server.saving") : t("server.save")}</Text>
          </Button>
          <Button
            variant="outline" disabled={switching}
            onPress={() => {
              setApiUrl(CLOUD_SERVER_CONFIG.apiUrl);
              setWebUrl(CLOUD_SERVER_CONFIG.webUrl);
              setError(null);
            }}
          >
            <Text>{t("server.restore_defaults")}</Text>
          </Button>
          <Text className="text-sm text-muted-foreground">{t("server.security")}</Text>
        </ScrollView>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}
