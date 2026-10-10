/**
 * HarmonyOS port of apps/mobile/app/(auth)/login.tsx — email + code sign-in,
 * aligned to the iOS screen: MulticaLogo header, TextField (invalid state on
 * error), mapAuthError copy, haptics on submit/failure, lg Send-code button.
 *
 * `onCodeSent` keeps this slice's props contract — app-shell.tsx pushes the
 * verify screen with the email.
 */
import React, { useState } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useKeyboardHeight } from "@/lib/use-keyboard-height";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
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
import type { ThemeColors } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

export function LoginScreen({
  onCodeSent,
}: {
  onCodeSent: (email: string) => void;
}) {
  const c = useThemeColors();
  const s = styles(c);
  const keyboardHeight = useKeyboardHeight();
  const sendCode = useAuthStore((state) => state.sendCode);
  const [email, setEmail] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const onSubmit = async () => {
    const trimmed = email.trim();
    if (!trimmed) return;
    void impactAsync(ImpactFeedbackStyle.Light);
    setSubmitting(true);
    setError(null);
    try {
      await sendCode(trimmed);
      onCodeSent(trimmed);
    } catch (err) {
      void notificationAsync(NotificationFeedbackType.Error);
      setError(mapAuthError(err, "Couldn't send the code. Try again."));
    } finally {
      setSubmitting(false);
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
                Sign in to Multica
              </Text>
              <Text style={[s.subtitle, { color: c.mutedForeground }]}>
                Enter your email and we'll send you a verification code.
              </Text>
            </View>
          </View>

          <View style={s.form}>
            <TextField
              autoCapitalize="none"
              autoComplete="email"
              autoFocus
              keyboardType="email-address"
              placeholder="you@example.com"
              placeholderTextColor={c.mutedForeground}
              value={email}
              onChangeText={setEmail}
              onSubmitEditing={() => void onSubmit()}
              returnKeyType="send"
              editable={!submitting}
              invalid={!!error}
            />
            {error ? (
              <Text style={[s.error, { color: c.destructive }]}>{error}</Text>
            ) : null}
          </View>

          <Button
            size="lg"
            disabled={submitting || !email.trim()}
            onPress={() => void onSubmit()}
          >
            <Text>{submitting ? "Sending..." : "Send code"}</Text>
          </Button>
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
    form: { gap: 12 },
    error: { fontSize: 14 },
  });
