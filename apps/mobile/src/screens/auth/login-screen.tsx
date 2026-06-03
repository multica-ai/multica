import "react-native-get-random-values";
import { useEffect, useState } from "react";
import { StyleSheet, Text, View } from "react-native";
import { useTranslation } from "react-i18next";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useCoreQueryClient } from "@multica/core/provider";
import { Button, Field, Heading, Screen } from "../../components/ui/primitives";
import { colors, spacing } from "../../theme/tokens";

type AuthConfig = {
  hideEmailLogin?: boolean;
};

export function LoginScreen() {
  const { t } = useTranslation();
  const queryClient = useCoreQueryClient();
  const sendCode = useAuthStore((state) => state.sendCode);
  const verifyCode = useAuthStore((state) => state.verifyCode);
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [sent, setSent] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [authConfig, setAuthConfig] = useState<AuthConfig>({});

  useEffect(() => {
    let alive = true;
    api
      .getConfig()
      .then((cfg) => {
        if (!alive) return;
        setAuthConfig({
          hideEmailLogin: cfg.hide_email_login,
        });
      })
      .catch(() => {
        // Email login remains available when runtime config cannot be loaded.
      });
    return () => {
      alive = false;
    };
  }, []);

  async function handleSendCode() {
    if (!email.trim()) return;
    setSubmitting(true);
    setError(null);
    try {
      await sendCode(email.trim());
      setSent(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("auth.unable_to_send_code"));
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
      setError(err instanceof Error ? err.message : t("auth.unable_to_verify_code"));
    } finally {
      setSubmitting(false);
    }
  }

  const showEmailLogin = !authConfig.hideEmailLogin;

  return (
    <Screen>
      <View style={styles.container}>
        <View style={styles.header}>
          <Heading>Multicam</Heading>
          <Text style={styles.subtitle}>{t("auth.subtitle")}</Text>
        </View>
        {showEmailLogin ? (
          <>
            <Field
              autoCapitalize="none"
              autoComplete="email"
              keyboardType="email-address"
              onChangeText={setEmail}
              placeholder={t("auth.email")}
              value={email}
            />
            {sent ? (
              <Field
                autoCapitalize="none"
                keyboardType="number-pad"
                onChangeText={setCode}
                placeholder={t("auth.verification_code")}
                value={code}
              />
            ) : null}
            <Button
              disabled={submitting}
              onPress={sent ? handleVerifyCode : handleSendCode}
            >
              {sent ? t("auth.verify_code") : t("auth.send_code")}
            </Button>
          </>
        ) : null}
        {!showEmailLogin ? (
          <Text style={styles.error}>{t("auth.no_login_methods")}</Text>
        ) : null}
        {error ? <Text style={styles.error}>{error}</Text> : null}
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
