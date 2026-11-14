import { Image, KeyboardAvoidingView, Platform, StyleSheet, TextInput, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Button } from "../../components/Button";
import { FloatingButton } from "../../components/Floating";
import { Screen } from "../../components/Screen";
import { Txt } from "../../components/Text";
import { useStrings } from "../../strings";
import { radius, useTheme } from "../../theme";

const icon = require("../../../../assets/icon.png") as number;

export const CODE_LENGTH = 6;

interface SignInViewProps {
  step: "email" | "code";
  email: string;
  code: string;
  onEmailChange: (value: string) => void;
  onCodeChange: (value: string) => void;
  onSubmit: () => void;
  onBack: () => void;
  onResend: () => void;
  resendIn: number;
  busy: boolean;
  error: string | null;
  serverHost: string;
}

export function SignInView(props: SignInViewProps) {
  const { step, email, code, onEmailChange, onCodeChange, onSubmit, onBack, onResend, resendIn, busy, error, serverHost } = props;
  const strings = useStrings();
  const palette = useTheme();
  const insets = useSafeAreaInsets();
  const isEmail = step === "email";
  // No dot required: self-hosted and local servers sign in addresses like dev@localhost.
  const valid = isEmail ? /^[^\s@]+@[^\s@]+$/.test(email.trim()) : code.length === CODE_LENGTH;

  return (
    <Screen>
      <KeyboardAvoidingView behavior={Platform.OS === "ios" ? "padding" : undefined} style={[styles.flex, { paddingTop: insets.top, paddingBottom: insets.bottom + 12 }]}>
        <View style={styles.nav}>{isEmail ? null : <FloatingButton icon="ChevronLeft" label={strings.auth.differentEmail} onPress={onBack} />}</View>
        <View style={styles.content}>
          {isEmail ? <Image source={icon} style={styles.logo} accessibilityIgnoresInvertColors /> : null}
          <Txt variant="title" accessibilityRole="header">
            {isEmail ? strings.auth.welcome : strings.auth.codeTitle}
          </Txt>
          <Txt variant="callout" tone="secondary" style={styles.detail}>
            {isEmail ? strings.auth.welcomeDetail : strings.auth.codeDetail(email.trim())}
          </Txt>
          {isEmail ? (
            <TextInput
              value={email}
              onChangeText={onEmailChange}
              onSubmitEditing={() => valid && onSubmit()}
              placeholder={strings.auth.email}
              placeholderTextColor={palette.label3}
              keyboardType="email-address"
              textContentType="emailAddress"
              autoComplete="email"
              autoCapitalize="none"
              autoCorrect={false}
              autoFocus
              returnKeyType="next"
              style={[styles.field, { backgroundColor: palette.grouped, color: palette.label }]}
            />
          ) : (
            <TextInput
              value={code}
              onChangeText={(value) => onCodeChange(value.replace(/\D/g, "").slice(0, CODE_LENGTH))}
              placeholder={"0".repeat(CODE_LENGTH)}
              placeholderTextColor={palette.label3}
              keyboardType="number-pad"
              textContentType="oneTimeCode"
              autoComplete="one-time-code"
              autoFocus
              accessibilityLabel={strings.auth.code}
              style={[styles.field, styles.code, { backgroundColor: palette.grouped, color: palette.label }]}
            />
          )}
          {error ? (
            <Txt variant="meta" tone="destructive" style={styles.error}>
              {error}
            </Txt>
          ) : null}
        </View>
        <View style={styles.actions}>
          <Button label={isEmail ? strings.auth.continue : strings.auth.verify} onPress={onSubmit} disabled={!valid} busy={busy} />
          {isEmail ? (
            <Txt variant="caption" tone="tertiary" style={styles.server}>
              {strings.auth.server(serverHost)}
            </Txt>
          ) : (
            <Button kind="plain" label={resendIn > 0 ? strings.auth.resendIn(resendIn) : strings.auth.resend} onPress={onResend} disabled={resendIn > 0 || busy} />
          )}
        </View>
      </KeyboardAvoidingView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  nav: { height: 56, paddingHorizontal: 16, justifyContent: "center" },
  content: { flex: 1, paddingHorizontal: 24, paddingTop: 12 },
  logo: { width: 52, height: 52, borderRadius: 13, marginBottom: 24 },
  detail: { marginTop: 8, marginBottom: 24 },
  field: { height: 52, borderRadius: radius.card, paddingHorizontal: 16, fontSize: 17 },
  code: { fontSize: 24, letterSpacing: 10, fontWeight: "600", textAlign: "center" },
  error: { marginTop: 12 },
  actions: { paddingHorizontal: 24, gap: 8 },
  server: { textAlign: "center", marginTop: 8 },
});
