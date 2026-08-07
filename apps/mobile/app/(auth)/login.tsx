import { useEffect, useState } from "react";
import { KeyboardAvoidingView, Platform, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { router, useLocalSearchParams } from "expo-router";
import * as Haptics from "expo-haptics";
import * as WebBrowser from "expo-web-browser";
import { useQueryClient } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { Button } from "@/components/ui/button";
import { MulticaLogo } from "@/components/brand/multica-logo";
import { useAuthStore } from "@/data/auth-store";
import { mapAuthError } from "@/lib/auth-error";
import { api } from "@/data/api";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  AUTH_CALLBACK_URL,
  buildMobileLoginUrl,
  getAuthHandoffToken,
} from "@/lib/auth-handoff";

export default function Login() {
  const sendCode = useAuthStore((s) => s.sendCode);
  const loginWithToken = useAuthStore((s) => s.loginWithToken);
  const queryClient = useQueryClient();
  const { authError } = useLocalSearchParams<{ authError?: string }>();
  const [email, setEmail] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [oidcProviderName, setOidcProviderName] = useState<string | null>(null);
  const [oidcSubmitting, setOidcSubmitting] = useState(false);
  const mobileLoginUrl = buildMobileLoginUrl(
    process.env.EXPO_PUBLIC_WEB_URL ?? "",
  );

  useEffect(() => {
    let active = true;
    void api.getPublicAuthConfig().then((config) => {
      if (active) setOidcProviderName(config.oidcProviderName ?? null);
    }).catch(() => {});
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (authError) setError(authError);
  }, [authError]);

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

  const onOIDCLogin = async () => {
    if (!mobileLoginUrl) return;
    void Haptics.selectionAsync();
    setOidcSubmitting(true);
    setError(null);
    try {
      const result = await WebBrowser.openAuthSessionAsync(
        mobileLoginUrl,
        AUTH_CALLBACK_URL,
      );
      if (result.type !== "success") return;
      const token = getAuthHandoffToken(result.url);
      if (!token) {
        setError("Couldn't complete single sign-on. Try again.");
        return;
      }
      await loginWithToken(token);
      await useWorkspaceStore.getState().clear();
      queryClient.clear();
      void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
      router.replace("/");
    } catch (err) {
      void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      setError(
        mapAuthError(err, "Couldn't complete single sign-on. Try again."),
      );
    } finally {
      setOidcSubmitting(false);
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

          {oidcProviderName && mobileLoginUrl ? (
            <View className="gap-4">
              <View className="flex-row items-center gap-3">
                <View className="h-px flex-1 bg-border" />
                <Text className="text-xs text-muted-foreground">or</Text>
                <View className="h-px flex-1 bg-border" />
              </View>
              <Button
                variant="outline"
                size="lg"
                disabled={submitting || oidcSubmitting}
                onPress={onOIDCLogin}
              >
                <Text>
                  {oidcSubmitting
                    ? "Opening..."
                    : `Continue with ${oidcProviderName}`}
                </Text>
              </Button>
            </View>
          ) : null}
        </View>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}
