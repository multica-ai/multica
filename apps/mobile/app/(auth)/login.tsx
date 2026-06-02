import { useState } from "react";
import { KeyboardAvoidingView, Platform, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { router } from "expo-router";
import * as Haptics from "expo-haptics";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { Button } from "@/components/ui/button";
import { MulticaLogo } from "@/components/brand/multica-logo";
import { FeishuLogo } from "@/components/brand/feishu-logo";
import { useAuthStore } from "@/data/auth-store";
import { mapAuthError } from "@/lib/auth-error";
import { startFeishuLogin } from "@/lib/feishu-sso";

export default function Login() {
  const sendCode = useAuthStore((s) => s.sendCode);
  const loginWithFeishuCode = useAuthStore((s) => s.loginWithFeishuCode);
  const [email, setEmail] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [feishuPending, setFeishuPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

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

  const onFeishuLogin = async () => {
    if (feishuPending) return;
    void Haptics.selectionAsync();
    setFeishuPending(true);
    setError(null);
    try {
      const { code, redirectUri } = await startFeishuLogin();
      await loginWithFeishuCode(code, redirectUri);
      // Navigate to the entry route, exactly as verify.tsx does after
      // verifyCode. Setting `user` in the store does NOT auto-redirect
      // us off /login — app/index.tsx's <Redirect> only runs when we're
      // ON "/", so we have to go there ourselves. Without this the
      // /auth/feishu call succeeds (200, token stored) but the screen
      // stays on the login page.
      router.replace("/");
    } catch (err) {
      const code =
        err && typeof err === "object" && "code" in err
          ? String((err as { code: unknown }).code)
          : "";
      // User-cancelled the consent screen is not an error — quietly reset.
      if (code === "FEISHU_SSO_CANCELLED") {
        return;
      }
      void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      setError(mapAuthError(err, "Feishu sign-in failed. Try again."));
    } finally {
      setFeishuPending(false);
    }
  };

  // iOS-only — `lib/feishu-sso.ts` throws at import time on Android, so
  // we don't render the button on non-iOS platforms.
  const showFeishu = Platform.OS === "ios";

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

          {showFeishu ? (
            <View className="gap-3">
              <Button
                size="lg"
                // Feishu brand blue (#3370FF). Hardcoded hex rather than a
                // semantic token because this is a third-party brand
                // affordance — it must read as Feishu's color, not adapt to
                // Multica's theme. White logo + white label sit on top.
                className="bg-[#3370FF] active:bg-[#3370FF]/90"
                disabled={feishuPending || submitting}
                onPress={onFeishuLogin}
              >
                <FeishuLogo size={18} color="#ffffff" />
                <Text className="text-white">
                  {feishuPending ? "Opening Feishu..." : "Continue with Feishu"}
                </Text>
              </Button>
              <View className="flex-row items-center gap-3">
                <View className="flex-1 h-px bg-border" />
                <Text className="text-xs text-muted-foreground">or</Text>
                <View className="flex-1 h-px bg-border" />
              </View>
            </View>
          ) : null}

          <View className="gap-3">
            <TextField
              autoCapitalize="none"
              autoComplete="email"
              autoFocus={!showFeishu}
              keyboardType="email-address"
              placeholder="you@example.com"
              value={email}
              onChangeText={setEmail}
              onSubmitEditing={onSubmit}
              returnKeyType="send"
              editable={!submitting && !feishuPending}
              invalid={!!error}
            />
            {error ? (
              <Text className="text-sm text-destructive">{error}</Text>
            ) : null}
          </View>

          <Button
            size="lg"
            disabled={submitting || feishuPending || !email.trim()}
            onPress={onSubmit}
          >
            <Text>{submitting ? "Sending..." : "Send code"}</Text>
          </Button>
        </View>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}
