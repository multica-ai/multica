import "react-native-get-random-values";
import type { ReactNode } from "react";
import { useEffect, useState } from "react";
import { Linking, Platform, Pressable, StyleSheet, Text, View } from "react-native";
import Svg, { Circle, Path } from "react-native-svg";
import { GoogleSignin, statusCodes } from "@react-native-google-signin/google-signin";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useCoreQueryClient } from "@multica/core/provider";
import { Button, Field, Heading, Screen } from "../../components/ui/primitives";
import { MOBILE_ENV } from "../../runtime/env";
import { colors, radii, spacing } from "../../theme/tokens";

type AuthConfig = {
  googleClientId?: string;
  googleIosClientId?: string;
  dingtalkClientId?: string;
  hideEmailLogin?: boolean;
};

export function LoginScreen() {
  const queryClient = useCoreQueryClient();
  const sendCode = useAuthStore((state) => state.sendCode);
  const verifyCode = useAuthStore((state) => state.verifyCode);
  const loginWithGoogleMobile = useAuthStore((state) => state.loginWithGoogleMobile);
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
          googleClientId: cfg.google_client_id || undefined,
          googleIosClientId: cfg.google_ios_client_id || MOBILE_ENV.googleIosClientId || undefined,
          dingtalkClientId: cfg.dingtalk_client_id || undefined,
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

  useEffect(() => {
    if (!authConfig.googleClientId) return;

    GoogleSignin.configure({
      scopes: ["openid", "email", "profile"],
      webClientId: authConfig.googleClientId,
      iosClientId: authConfig.googleIosClientId,
    });
  }, [authConfig.googleClientId, authConfig.googleIosClientId]);

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

  async function handleOAuthLogin(provider: "dingtalk" | "google") {
    if (provider === "google") {
      await handleGoogleLogin();
      return;
    }

    setError(null);
    try {
      const url = new URL("/login", MOBILE_ENV.webBaseUrl);
      url.searchParams.set("platform", "mobile");
      url.searchParams.set("provider", provider);
      await Linking.openURL(url.toString());
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to open login");
    }
  }

  async function handleGoogleLogin() {
    if (!authConfig.googleClientId) return;

    setSubmitting(true);
    setError(null);
    try {
      if (Platform.OS === "android") {
        await GoogleSignin.hasPlayServices({ showPlayServicesUpdateDialog: true });
      }

      const result = await GoogleSignin.signIn();
      if (result.type === "cancelled") return;

      const idToken = result.data.idToken;
      if (!idToken) {
        throw new Error("Google did not return an ID token");
      }

      queryClient.clear();
      await loginWithGoogleMobile(idToken, Platform.OS);
    } catch (err) {
      if (
        isGoogleSignInCode(err, statusCodes.SIGN_IN_CANCELLED) ||
        isGoogleSignInCode(err, statusCodes.IN_PROGRESS)
      ) {
        return;
      }
      if (isGoogleSignInCode(err, statusCodes.PLAY_SERVICES_NOT_AVAILABLE)) {
        setError("Google Play services are not available on this device");
        return;
      }
      setError(err instanceof Error ? err.message : "Unable to sign in with Google");
    } finally {
      setSubmitting(false);
    }
  }

  const showEmailLogin = !authConfig.hideEmailLogin;
  const showDingTalk = Boolean(authConfig.dingtalkClientId);
  const showGoogle = Boolean(authConfig.googleClientId);

  return (
    <Screen>
      <View style={styles.container}>
        <View style={styles.header}>
          <Heading>Multicam</Heading>
          <Text style={styles.subtitle}>Sign in to your Multica workspace.</Text>
        </View>
        {showEmailLogin ? (
          <>
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
            <Button
              disabled={submitting}
              onPress={sent ? handleVerifyCode : handleSendCode}
            >
              {sent ? "Verify code" : "Send code"}
            </Button>
          </>
        ) : null}
        {showDingTalk || showGoogle ? (
          <View style={styles.oauthGroup}>
            {showDingTalk ? (
              <OAuthButton
                icon={<DingTalkIcon />}
                label="Continue with DingTalk"
                onPress={() => void handleOAuthLogin("dingtalk")}
              />
            ) : null}
            {showGoogle ? (
              <OAuthButton
                icon={<GoogleIcon />}
                label="Continue with Google"
                disabled={submitting}
                onPress={() => void handleOAuthLogin("google")}
              />
            ) : null}
          </View>
        ) : null}
        {!showEmailLogin && !showDingTalk && !showGoogle ? (
          <Text style={styles.error}>No login methods are configured.</Text>
        ) : null}
        {error ? <Text style={styles.error}>{error}</Text> : null}
      </View>
    </Screen>
  );
}

function OAuthButton({
  disabled,
  icon,
  label,
  onPress,
}: {
  disabled?: boolean;
  icon: ReactNode;
  label: string;
  onPress: () => void;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [
        styles.oauthButton,
        disabled && styles.oauthButtonDisabled,
        pressed && styles.oauthButtonPressed,
      ]}
    >
      <View style={styles.oauthIcon}>{icon}</View>
      <Text style={styles.oauthButtonText}>{label}</Text>
    </Pressable>
  );
}

function isGoogleSignInCode(err: unknown, code: string) {
  return (
    typeof err === "object" &&
    err !== null &&
    "code" in err &&
    (err as { code?: unknown }).code === code
  );
}

function DingTalkIcon() {
  return (
    <Svg height={22} viewBox="0 0 24 24" width={22}>
      <Circle cx={12} cy={12} fill="#1677ff" r={11} />
      <Path
        d="M7.1 6.9c3.7 1.1 6.9 1.2 9.8.4.3-.1.6.2.4.5l-1.1 2.1c-.1.2-.1.4.1.5l1.2.7c.3.2.3.6-.1.7l-2.1.8c-.2.1-.3.2-.4.4-1 2.4-2.9 4-5.7 4.7-.4.1-.7-.3-.4-.6l2.4-2.8c.2-.2.1-.5-.2-.5l-3.4-.4c-.4 0-.5-.5-.2-.7l2.5-1.7c.3-.2.2-.6-.1-.7L6.8 9.5c-.4-.1-.5-.6-.1-.8l1.8-.8-1.6-.4c-.4-.1-.3-.7.2-.6z"
        fill="#ffffff"
      />
    </Svg>
  );
}

function GoogleIcon() {
  return (
    <Svg height={22} viewBox="0 0 24 24" width={22}>
      <Path
        d="M21.6 12.2c0-.7-.1-1.4-.2-2H12v3.8h5.4c-.2 1.2-.9 2.2-1.9 2.9v2.4h3.1c1.9-1.7 3-4.2 3-7.1z"
        fill="#4285F4"
      />
      <Path
        d="M12 22c2.7 0 5-.9 6.6-2.6l-3.1-2.4c-.9.6-2 .9-3.5.9-2.6 0-4.8-1.7-5.6-4.1H3.2v2.5C4.8 19.7 8.1 22 12 22z"
        fill="#34A853"
      />
      <Path
        d="M6.4 13.8c-.2-.6-.3-1.2-.3-1.8s.1-1.2.3-1.8V7.7H3.2C2.4 9 2 10.5 2 12s.4 3 1.2 4.3l3.2-2.5z"
        fill="#FBBC05"
      />
      <Path
        d="M12 6.1c1.5 0 2.8.5 3.8 1.5l2.8-2.8C16.9 3.1 14.7 2 12 2 8.1 2 4.8 4.3 3.2 7.7l3.2 2.5C7.2 7.8 9.4 6.1 12 6.1z"
        fill="#EA4335"
      />
    </Svg>
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
  oauthButton: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    justifyContent: "center",
    minHeight: 48,
    paddingHorizontal: spacing.lg,
  },
  oauthButtonPressed: {
    opacity: 0.8,
  },
  oauthButtonDisabled: {
    opacity: 0.45,
  },
  oauthButtonText: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  oauthGroup: {
    gap: spacing.sm,
    marginTop: spacing.sm,
  },
  oauthIcon: {
    height: 22,
    width: 22,
  },
});
