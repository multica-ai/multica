import { useState, useCallback, useRef } from "react";
import {
  View,
  Text,
  TextInput,
  TouchableOpacity,
  StyleSheet,
  KeyboardAvoidingView,
  Platform,
  Alert,
} from "react-native";
import { useRouter, useLocalSearchParams } from "expo-router";
import { useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@/lib/providers";
import { ensureWorkspaceForUser } from "@/lib/workspace-bootstrap";
import { colors } from "@/lib/theme";

const CODE_LENGTH = 6;

export default function VerifyScreen() {
  const { email } = useLocalSearchParams<{ email: string }>();
  const [code, setCode] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isResending, setIsResending] = useState(false);
  const inputRef = useRef<TextInput>(null);
  const router = useRouter();
  const qc = useQueryClient();

  const handleVerify = useCallback(async () => {
    if (code.length !== CODE_LENGTH || !email) return;

    setIsSubmitting(true);
    try {
      await useAuthStore.getState().verifyCode(email, code);
      await ensureWorkspaceForUser(email, qc);
      router.replace("/(app)/(home)");
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : "Invalid code";
      Alert.alert("Error", message);
      setCode("");
      inputRef.current?.focus();
    } finally {
      setIsSubmitting(false);
    }
  }, [code, email, qc, router]);

  const handleResend = useCallback(async () => {
    if (!email || isResending) return;
    setIsResending(true);
    try {
      await useAuthStore.getState().sendCode(email);
      Alert.alert("Code Sent", `A new code has been sent to ${email}`);
    } catch {
      Alert.alert("Error", "Failed to resend code");
    } finally {
      setIsResending(false);
    }
  }, [email, isResending]);

  const handleCodeChange = useCallback(
    (text: string) => {
      const cleaned = text.replace(/\D/g, "").slice(0, CODE_LENGTH);
      setCode(cleaned);
      if (cleaned.length === CODE_LENGTH && email) {
        // Auto-submit on complete
        setTimeout(async () => {
          setIsSubmitting(true);
          try {
            await useAuthStore.getState().verifyCode(email, cleaned);
            await ensureWorkspaceForUser(email, qc);
            router.replace("/(app)/(home)");
          } catch (err: unknown) {
            const message =
              err instanceof Error ? err.message : "Invalid code";
            Alert.alert("Error", message);
            setCode("");
            inputRef.current?.focus();
          } finally {
            setIsSubmitting(false);
          }
        }, 100);
      }
    },
    [email, qc, router],
  );

  return (
    <KeyboardAvoidingView
      style={styles.container}
      behavior={Platform.OS === "ios" ? "padding" : "height"}
    >
      <View style={styles.content}>
        <TouchableOpacity onPress={() => router.back()} style={styles.back}>
          <Text style={styles.backText}>← Back</Text>
        </TouchableOpacity>

        <Text style={styles.title}>Check your email</Text>
        <Text style={styles.subtitle}>
          Enter the 6-digit code sent to{"\n"}
          <Text style={styles.email}>{email}</Text>
        </Text>

        <TextInput
          ref={inputRef}
          style={styles.codeInput}
          value={code}
          onChangeText={handleCodeChange}
          keyboardType="number-pad"
          maxLength={CODE_LENGTH}
          autoFocus
          editable={!isSubmitting}
          placeholder="000000"
          placeholderTextColor={colors.border}
          textContentType="oneTimeCode"
        />

        <TouchableOpacity
          style={[styles.button, isSubmitting && styles.buttonDisabled]}
          onPress={handleVerify}
          disabled={isSubmitting || code.length !== CODE_LENGTH}
          activeOpacity={0.8}
        >
          <Text style={styles.buttonText}>
            {isSubmitting ? "Verifying..." : "Verify"}
          </Text>
        </TouchableOpacity>

        <TouchableOpacity
          onPress={handleResend}
          disabled={isResending}
          style={styles.resend}
        >
          <Text style={styles.resendText}>
            {isResending ? "Sending..." : "Resend code"}
          </Text>
        </TouchableOpacity>
      </View>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.background,
  },
  content: {
    flex: 1,
    justifyContent: "center",
    paddingHorizontal: 24,
  },
  back: {
    position: "absolute",
    top: 60,
    left: 24,
  },
  backText: {
    fontSize: 16,
    color: colors.foreground,
  },
  title: {
    fontSize: 28,
    fontWeight: "700",
    color: colors.foreground,
    marginBottom: 8,
  },
  subtitle: {
    fontSize: 16,
    color: colors.mutedForeground,
    marginBottom: 32,
    lineHeight: 24,
  },
  email: {
    fontWeight: "600",
    color: colors.foreground,
  },
  codeInput: {
    height: 56,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 10,
    paddingHorizontal: 16,
    fontSize: 28,
    fontWeight: "600",
    color: colors.foreground,
    backgroundColor: colors.background,
    marginBottom: 16,
    textAlign: "center",
    letterSpacing: 8,
  },
  button: {
    height: 48,
    borderRadius: 10,
    backgroundColor: colors.foreground,
    justifyContent: "center",
    alignItems: "center",
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  buttonText: {
    color: colors.background,
    fontSize: 16,
    fontWeight: "600",
  },
  resend: {
    marginTop: 16,
    alignItems: "center",
  },
  resendText: {
    fontSize: 14,
    color: colors.mutedForeground,
  },
});
