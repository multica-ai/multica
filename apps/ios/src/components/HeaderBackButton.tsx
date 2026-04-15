import { Text, TouchableOpacity, StyleSheet } from "react-native";
import { useRouter } from "expo-router";
import { colors } from "@/lib/theme";

export function HeaderBackButton({
  label = "Back",
  fallback,
}: {
  label?: string;
  fallback?: string;
}) {
  const router = useRouter();

  return (
    <TouchableOpacity
      onPress={() => {
        if (router.canGoBack()) {
          router.back();
        } else if (fallback) {
          router.replace(fallback as never);
        }
      }}
      style={styles.button}
      activeOpacity={0.7}
    >
      <Text style={styles.text}>‹ {label}</Text>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  button: {
    paddingRight: 8,
  },
  text: {
    fontSize: 16,
    color: colors.foreground,
    fontWeight: "500",
  },
});
