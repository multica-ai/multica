/**
 * HarmonyOS port of apps/mobile/app/(auth)/verify.tsx — 6-digit code entry,
 * aligned to the iOS screen: MulticaLogo header, OtpInput visuals (48×56
 * slots, secondary/50 fill, 2px primary ring on the active slot, fake
 * caret), paste support, 60s resend cooldown, haptics on submit/success/
 * failure, mapAuthError copy.
 *
 * input-otp-native is not wired on this matrix, so the slots are plain
 * Views driven by a zero-size TextInput — the multi-char onChangeText path
 * covers paste. Props keep this slice's contract (email / onVerified /
 * onBack); app-shell.tsx owns navigation.
 */
import React, { useEffect, useRef, useState } from "react";
import { Pressable, ScrollView, StyleSheet, Text as RNText, TextInput, View } from "react-native";
import { useKeyboardHeight } from "@/lib/use-keyboard-height";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { MulticaLogo } from "@/components/brand/multica-logo";
import { SafeAreaView } from "@/lib/safe-area";
import { useAuthStore } from "@/data/auth-store";
import { mapAuthError } from "@/lib/auth-error";
import {
  impactAsync,
  ImpactFeedbackStyle,
  notificationAsync,
  NotificationFeedbackType,
} from "@/lib/haptics";
import { withAlpha } from "@/lib/theme";
import type { ThemeColors } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

const CODE_LENGTH = 6;
const RESEND_COOLDOWN_SECONDS = 60;

export function VerifyScreen({
  email,
  onVerified,
  onBack,
}: {
  email: string;
  onVerified: () => void;
  onBack: () => void;
}) {
  const c = useThemeColors();
  const s = styles(c);
  const keyboardHeight = useKeyboardHeight();
  const sendCode = useAuthStore((state) => state.sendCode);
  const verifyCode = useAuthStore((state) => state.verifyCode);
  const [code, setCode] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [cooldown, setCooldown] = useState(RESEND_COOLDOWN_SECONDS);
  const [resending, setResending] = useState(false);
  const [otpFocused, setOtpFocused] = useState(false);
  const inputRef = useRef<TextInput>(null);

  useEffect(() => {
    if (cooldown <= 0) return;
    const t = setInterval(() => {
      setCooldown((n) => (n <= 1 ? 0 : n - 1));
    }, 1000);
    return () => clearInterval(t);
  }, [cooldown]);

  const submit = async (value: string) => {
    if (!value || !email || submitting) return;
    void impactAsync(ImpactFeedbackStyle.Light);
    setSubmitting(true);
    setError(null);
    try {
      await verifyCode(email, value);
      void notificationAsync(NotificationFeedbackType.Success);
      onVerified();
    } catch (err) {
      void notificationAsync(NotificationFeedbackType.Error);
      setError(mapAuthError(err, "Couldn't verify the code. Try again."));
      setSubmitting(false);
      setCode("");
    }
  };

  const onResend = async () => {
    if (cooldown > 0 || resending || !email) return;
    void impactAsync(ImpactFeedbackStyle.Light);
    setResending(true);
    setError(null);
    try {
      await sendCode(email);
      setCooldown(RESEND_COOLDOWN_SECONDS);
      setCode("");
    } catch (err) {
      void notificationAsync(NotificationFeedbackType.Error);
      setError(mapAuthError(err, "Couldn't resend the code. Try again."));
    } finally {
      setResending(false);
    }
  };

  // Digits only. A multi-character insert (paste) fills the slots from the
  // start; filling the last slot auto-submits, like the iOS onComplete.
  const handleCodeChange = (raw: string) => {
    const digits = raw.replace(/\D/g, "").slice(0, CODE_LENGTH);
    setCode(digits);
    if (digits.length === CODE_LENGTH) {
      void submit(digits);
    }
  };

  return (
    <SafeAreaView style={s.screen}>
        <ScrollView
          style={s.flex}
          contentContainerStyle={[s.center, { paddingBottom: keyboardHeight }]}
          keyboardShouldPersistTaps="handled"
        >
          <View style={s.header}>
            <MulticaLogo size={32} />
            <View style={s.headerText}>
              <Text style={[s.title, { color: c.foreground }]}>
                Enter verification code
              </Text>
              <Text style={[s.subtitle, { color: c.mutedForeground }]}>
                We sent a 6-digit code to {email}
              </Text>
            </View>
          </View>

          <View style={s.otpSection}>
            {/* The boxes render from `code`; the zero-size input below owns
                focus and the keyboard (established harmony OTP pattern). */}
            <Pressable
              style={s.slotRow}
              onPress={() => inputRef.current?.focus()}
            >
              {Array.from({ length: CODE_LENGTH }).map((_, i) => {
                const active = otpFocused && i === code.length;
                const char = code[i] ?? "";
                return (
                  <View
                    key={i}
                    style={[
                      s.slot,
                      { backgroundColor: withAlpha(c.secondary, 0.5) },
                      // 2px border on every slot keeps geometry stable;
                      // only the active slot's border is visible — the iOS
                      // `border-2 border-primary` active treatment.
                      {
                        borderColor: active ? c.primary : "transparent",
                      },
                    ]}
                  >
                    {char ? (
                      <RNText style={[s.slotChar, { color: c.foreground }]}>
                        {char}
                      </RNText>
                    ) : active ? (
                      <View
                        style={[s.caret, { backgroundColor: c.foreground }]}
                      />
                    ) : null}
                  </View>
                );
              })}
            </Pressable>
            {/* Zero-size input drives the OTP boxes above. */}
            <TextInput
              ref={inputRef}
              style={s.hiddenInput}
              value={code}
              maxLength={CODE_LENGTH}
              autoFocus
              keyboardType="number-pad"
              autoCorrect={false}
              textContentType="oneTimeCode"
              onFocus={() => setOtpFocused(true)}
              onBlur={() => setOtpFocused(false)}
              onChangeText={handleCodeChange}
            />
            {error ? (
              <Text style={[s.error, { color: c.destructive }]}>{error}</Text>
            ) : null}
          </View>

          <View style={s.actions}>
            <Button
              size="lg"
              disabled={submitting || code.length < CODE_LENGTH}
              onPress={() => void submit(code)}
            >
              <Text>{submitting ? "Verifying..." : "Verify"}</Text>
            </Button>

            <Pressable
              onPress={() => void onResend()}
              disabled={cooldown > 0 || resending}
              style={s.resend}
            >
              <Text
                style={[
                  s.resendLabel,
                  {
                    color:
                      cooldown > 0 || resending
                        ? c.mutedForeground
                        : c.primary,
                  },
                ]}
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
              onPress={onBack}
            >
              <Text>Use a different email</Text>
            </Button>
          </View>
        </ScrollView>
    </SafeAreaView>
  );
}

const styles = (c: ThemeColors) =>
  StyleSheet.create({
    flex: { flex: 1 },
    screen: { flex: 1, backgroundColor: c.background },
    center: {
      flexGrow: 1,
      justifyContent: "center",
      paddingHorizontal: 24,
      paddingVertical: 24,
      gap: 24,
    },
    header: { alignItems: "center", gap: 12 },
    headerText: { gap: 4, alignItems: "center" },
    title: { fontSize: 24, fontWeight: "600" },
    subtitle: { fontSize: 14, textAlign: "center" },
    otpSection: { gap: 12, alignItems: "center" },
    slotRow: { flexDirection: "row", gap: 8, alignSelf: "stretch" },
    slot: {
      flex: 1,
      height: 56,
      borderRadius: 6,
      alignItems: "center",
      justifyContent: "center",
      borderWidth: 2,
    },
    slotChar: { fontSize: 22, fontWeight: "600", includeFontPadding: false },
    caret: { width: 2, height: 24 },
    hiddenInput: { height: 1, width: 1, opacity: 0 },
    error: { fontSize: 14, textAlign: "center" },
    actions: { gap: 12 },
    resend: { paddingVertical: 8, alignItems: "center" },
    resendLabel: { fontSize: 14 },
  });
