import "react-native-get-random-values";
import { useState } from "react";
import { StyleSheet, Text, View } from "react-native";
import { useAuthStore } from "@multica/core/auth";
import { useCoreQueryClient } from "@multica/core/provider";
import { Button, Field, Heading, Screen } from "../../components/ui/primitives";
import { colors, spacing } from "../../theme/tokens";

export function LoginScreen() {
  const queryClient = useCoreQueryClient();
  const sendCode = useAuthStore((state) => state.sendCode);
  const verifyCode = useAuthStore((state) => state.verifyCode);
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [sent, setSent] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSendCode() {
    if (!email.trim()) return;
    setSubmitting(true);
    setError(null);
    try {
      await sendCode(email.trim());
      setSent(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to send code");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleVerifyCode() {
    if (!email.trim() || !code.trim()) return;
    setSubmitting(true);
    setError(null);
    try {
      queryClient.clear();
      await verifyCode(email.trim(), code.trim());
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to verify code");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Screen>
      <View style={styles.container}>
        <View style={styles.header}>
          <Heading>Multicam</Heading>
          <Text style={styles.subtitle}>Sign in with your Multica email code.</Text>
        </View>
        <Field
          autoCapitalize="none"
          autoComplete="email"
          keyboardType="email-address"
          onChangeText={setEmail}
          placeholder="Email"
          value={email}
        />
        {sent ? (
          <Field
            autoCapitalize="none"
            keyboardType="number-pad"
            onChangeText={setCode}
            placeholder="Verification code"
            value={code}
          />
        ) : null}
        {error ? <Text style={styles.error}>{error}</Text> : null}
        <Button
          disabled={submitting}
          onPress={sent ? handleVerifyCode : handleSendCode}
        >
          {sent ? "Verify code" : "Send code"}
        </Button>
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  container: {
    alignSelf: "center",
    gap: spacing.md,
    maxWidth: 520,
    paddingTop: spacing.xl * 4,
    width: "100%",
  },
  header: {
    gap: spacing.sm,
    marginBottom: spacing.md,
  },
  subtitle: {
    color: colors.mutedForeground,
    fontSize: 14,
  },
  error: {
    color: colors.destructive,
    fontSize: 14,
  },
});
