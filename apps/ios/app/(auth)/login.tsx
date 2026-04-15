import { useState, useCallback } from "react";
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
import { useRouter } from "expo-router";
import { useAuthStore } from "@/lib/providers";
import { colors } from "@/lib/theme";

export default function LoginScreen() {
  const [email, setEmail] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const router = useRouter();

  const handleLogin = useCallback(async () => {
    const trimmed = email.trim().toLowerCase();
    if (!trimmed) return;

    setIsSubmitting(true);
    try {
      await useAuthStore.getState().sendCode(trimmed);
      router.push({
        pathname: "/(auth)/verify",
        params: { email: trimmed },
      });
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : "Failed to send code";
      Alert.alert("Error", message);
    } finally {
      setIsSubmitting(false);
    }
  }, [email, router]);

  return (
    <KeyboardAvoidingView
      style={styles.container}
      behavior={Platform.OS === "ios" ? "padding" : "height"}
    >
      <View style={styles.content}>
        <Text style={styles.logo}>Multica</Text>
        <Text style={styles.subtitle}>Sign in to your workspace</Text>

        <TextInput
          style={styles.input}
          placeholder="you@email.com"
          placeholderTextColor={colors.mutedForeground}
          value={email}
          onChangeText={setEmail}
          keyboardType="email-address"
          autoCapitalize="none"
          autoCorrect={false}
          autoComplete="email"
          editable={!isSubmitting}
          returnKeyType="next"
          onSubmitEditing={handleLogin}
        />

        <TouchableOpacity
          style={[styles.button, isSubmitting && styles.buttonDisabled]}
          onPress={handleLogin}
          disabled={isSubmitting || !email.trim()}
          activeOpacity={0.8}
        >
          <Text style={styles.buttonText}>
            {isSubmitting ? "Sending..." : "Continue with Email"}
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
  logo: {
    fontSize: 32,
    fontWeight: "700",
    color: colors.foreground,
    marginBottom: 8,
  },
  subtitle: {
    fontSize: 16,
    color: colors.mutedForeground,
    marginBottom: 32,
  },
  input: {
    height: 48,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 10,
    paddingHorizontal: 16,
    fontSize: 16,
    color: colors.foreground,
    backgroundColor: colors.background,
    marginBottom: 16,
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
});
