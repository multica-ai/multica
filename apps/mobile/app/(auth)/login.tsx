import { useEffect, useMemo, useState } from "react";
import { KeyboardAvoidingView, Platform, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { router } from "expo-router";
import * as Haptics from "expo-haptics";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { Button } from "@/components/ui/button";
import { MulticaLogo } from "@/components/brand/multica-logo";
import { useAuthStore } from "@/data/auth-store";
import { queryClient } from "@/data/query-client";
import {
  clearCustomApiUrl,
  getCustomApiUrl,
  getDefaultApiUrl,
  getEffectiveApiUrl,
  probeMulticaBackend,
  restoreServerConfig,
  ServerConfigError,
  setCustomApiUrl,
} from "@/data/server-config";
import { useWorkspaceStore } from "@/data/workspace-store";
import { mapAuthError } from "@/lib/auth-error";

export default function Login() {
  const sendCode = useAuthStore((s) => s.sendCode);
  const logout = useAuthStore((s) => s.logout);
  const [email, setEmail] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [serverUrl, setServerUrl] = useState("");
  const [savedServerUrl, setSavedServerUrl] = useState("");
  const [serverOpen, setServerOpen] = useState(false);
  const [savingServer, setSavingServer] = useState(false);
  const [serverError, setServerError] = useState<string | null>(null);
  const defaultUrl = getDefaultApiUrl();

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      await restoreServerConfig();
      if (cancelled) return;
      let effective = "";
      try {
        effective = getEffectiveApiUrl();
      } catch {
        effective = "";
      }
      setSavedServerUrl(effective);
      setServerUrl(effective);
      setServerOpen(!effective);
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const usingDefault = useMemo(
    () => !getCustomApiUrl() && !!defaultUrl && savedServerUrl === defaultUrl,
    [defaultUrl, savedServerUrl],
  );

  const clearLocalSession = async () => {
    await logout();
    await useWorkspaceStore.getState().clear();
    queryClient.clear();
  };

  const onSubmit = async () => {
    const trimmed = email.trim();
    if (!trimmed) return;
    void Haptics.selectionAsync();
    setSubmitting(true);
    setError(null);
    try {
      await sendCode(trimmed);
      router.push({ pathname: "/verify", params: { email: trimmed } });
    } catch (err) {
      void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      setError(mapAuthError(err, "Couldn't send the code. Try again."));
    } finally {
      setSubmitting(false);
    }
  };

  const onSaveServer = async () => {
    const trimmed = serverUrl.trim();
    void Haptics.selectionAsync();
    setSavingServer(true);
    setServerError(null);
    try {
      const probe = await probeMulticaBackend(trimmed);
      await setCustomApiUrl(probe.apiUrl, { webUrl: probe.webUrl });
      await clearLocalSession();
      setSavedServerUrl(probe.apiUrl);
      setServerUrl(probe.apiUrl);
      setServerOpen(false);
    } catch (err) {
      void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      setServerError(
        err instanceof ServerConfigError
          ? err.message
          : "Could not save that server.",
      );
    } finally {
      setSavingServer(false);
    }
  };

  const onResetServer = async () => {
    if (!defaultUrl) {
      setServerError("This build does not include a default backend URL.");
      return;
    }
    void Haptics.selectionAsync();
    setSavingServer(true);
    setServerError(null);
    try {
      await clearCustomApiUrl();
      await clearLocalSession();
      setSavedServerUrl(defaultUrl);
      setServerUrl(defaultUrl);
      setServerOpen(false);
    } catch {
      void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      setServerError("Could not reset the server.");
    } finally {
      setSavingServer(false);
    }
  };

  return (
    <SafeAreaView className="flex-1 bg-background">
      <KeyboardAvoidingView
        className="flex-1"
        behavior={Platform.OS === "ios" ? "padding" : undefined}
      >
        <View className="flex-1 justify-center px-6 gap-6">
          <View className="items-center gap-3">
            <MulticaLogo size={32} />
            <View className="gap-1 items-center">
              <Text className="text-2xl font-semibold text-foreground">
                Sign in to Multica
              </Text>
              <Text className="text-sm text-muted-foreground text-center">
                Enter your email and we&apos;ll send you a verification code.
              </Text>
            </View>
          </View>

          <View className="gap-3 rounded-md border border-border bg-secondary/30 p-3">
            <View className="gap-1">
              <Text className="text-sm font-medium text-foreground">
                Server
              </Text>
              <Text className="text-xs text-muted-foreground">
                {savedServerUrl || "No backend configured"}
              </Text>
            </View>

            {serverOpen ? (
              <View className="gap-3">
                <TextField
                  autoCapitalize="none"
                  autoCorrect={false}
                  keyboardType="url"
                  placeholder="https://api.example.com"
                  value={serverUrl}
                  onChangeText={setServerUrl}
                  editable={!savingServer}
                  invalid={!!serverError}
                />
                {serverError ? (
                  <Text className="text-sm text-destructive">
                    {serverError}
                  </Text>
                ) : (
                  <Text className="text-xs text-muted-foreground">
                    Use a Multica backend origin, for example https://... or
                    http://192.168.1.42:8080.
                  </Text>
                )}
                <View className="flex-row gap-2">
                  <Button
                    className="flex-1"
                    variant="outline"
                    disabled={savingServer}
                    onPress={() => {
                      setServerOpen(false);
                      setServerError(null);
                      setServerUrl(savedServerUrl);
                    }}
                  >
                    <Text>Cancel</Text>
                  </Button>
                  <Button
                    className="flex-1"
                    disabled={savingServer || !serverUrl.trim()}
                    onPress={onSaveServer}
                  >
                    <Text>{savingServer ? "Checking..." : "Save"}</Text>
                  </Button>
                </View>
              </View>
            ) : (
              <View className="flex-row gap-2">
                <Button
                  className="flex-1"
                  variant="outline"
                  disabled={savingServer}
                  onPress={() => {
                    setServerError(null);
                    setServerOpen(true);
                  }}
                >
                  <Text>Change server</Text>
                </Button>
                <Button
                  className="flex-1"
                  variant="ghost"
                  disabled={savingServer || usingDefault || !defaultUrl}
                  onPress={onResetServer}
                >
                  <Text>Use default</Text>
                </Button>
              </View>
            )}
          </View>

          <View className="gap-3">
            <TextField
              autoCapitalize="none"
              autoComplete="email"
              autoFocus
              keyboardType="email-address"
              placeholder="you@example.com"
              value={email}
              onChangeText={setEmail}
              onSubmitEditing={onSubmit}
              returnKeyType="send"
              editable={!submitting}
              invalid={!!error}
            />
            {error ? (
              <Text className="text-sm text-destructive">{error}</Text>
            ) : null}
          </View>

          <Button
            size="lg"
            disabled={submitting || !email.trim()}
            onPress={onSubmit}
          >
            <Text>{submitting ? "Sending..." : "Send code"}</Text>
          </Button>
        </View>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}
