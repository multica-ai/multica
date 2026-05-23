import { useEffect, useRef, useState } from "react";
import { KeyboardAvoidingView, Platform, Pressable, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { router, useLocalSearchParams } from "expo-router";
import * as Haptics from "expo-haptics";
import { Text } from "@/components/ui/text";
import { OtpInput, type OtpInputRef } from "@/components/ui/otp-input";
import { Button } from "@/components/ui/button";
import { MulticaLogo } from "@/components/brand/multica-logo";
import { useAuthStore } from "@/data/auth-store";
import { mapAuthError } from "@/lib/auth-error";

const CODE_LENGTH = 6;
const RESEND_COOLDOWN_SECONDS = 60;

export default function Verify() {
  const sendCode = useAuthStore((s) => s.sendCode);
  const verifyCode = useAuthStore((s) => s.verifyCode);
  const { email = "" } = useLocalSearchParams<{ email?: string }>();
  const [code, setCode] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [cooldown, setCooldown] = useState(RESEND_COOLDOWN_SECONDS);
  const [resending, setResending] = useState(false);
  const otpRef = useRef<OtpInputRef>(null);

  useEffect(() => {
    if (cooldown <= 0) return;
    const t = setInterval(() => {
      setCooldown((c) => (c <= 1 ? 0 : c - 1));
    }, 1000);
    return () => clearInterval(t);
  }, [cooldown]);

  const submit = async (value: string) => {
    if (!value || !email || submitting) return;
    void Haptics.selectionAsync();
    setSubmitting(true);
    setError(null);
    try {
      await verifyCode(email, value);
      void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
      router.replace("/");
    } catch (err) {
      void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      setError(mapAuthError(err, "Couldn't verify the code. Try again."));
      setSubmitting(false);
      otpRef.current?.clear();
      setCode("");
    }
  };

  const onResend = async () => {
    if (cooldown > 0 || resending || !email) return;
    void Haptics.selectionAsync();
    setResending(true);
    setError(null);
    try {
      await sendCode(email);
      setCooldown(RESEND_COOLDOWN_SECONDS);
      otpRef.current?.clear();
      setCode("");
    } catch (err) {
      void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      setError(mapAuthError(err, "Couldn't resend the code. Try again."));
    } finally {
      setResending(false);
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
                Enter verification code
              </Text>
              <Text className="text-sm text-muted-foreground text-center">
                We sent a 6-digit code to {email}
              </Text>
            </View>
          </View>

          <View className="gap-3 items-center">
            <OtpInput
              ref={otpRef}
              numberOfDigits={CODE_LENGTH}
              value={code}
              onChange={setCode}
              onComplete={submit}
              autoFocus
              editable={!submitting}
            />
            {error ? (
              <Text className="text-sm text-destructive">{error}</Text>
            ) : null}
          </View>

          <View className="gap-3">
            <Button
              size="lg"
              disabled={submitting || code.length < CODE_LENGTH}
              onPress={() => submit(code)}
            >
              <Text>{submitting ? "Verifying..." : "Verify"}</Text>
            </Button>

            <Pressable
              onPress={onResend}
              disabled={cooldown > 0 || resending}
              className="py-2 items-center"
            >
              <Text
                className={
                  cooldown > 0 || resending
                    ? "text-sm text-muted-foreground"
                    : "text-sm text-primary"
                }
              >
                {resending
                  ? "Sending..."
                  : cooldown > 0
                    ? `Resend code in ${cooldown}s`
                    : "Resend code"}
              </Text>
            </Pressable>

            <Button
              variant="ghost"
              disabled={submitting}
              onPress={() => router.back()}
            >
              <Text>Use a different email</Text>
            </Button>
          </View>
        </View>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}
