import type { ComponentType, ReactNode } from "react";
import { View, Text, TouchableOpacity, StyleSheet } from "react-native";
import { SafeAreaView as RawSafeAreaView } from "react-native-safe-area-context";
import { useRouter } from "expo-router";
import { colors } from "@/lib/theme";

const SafeAreaView = RawSafeAreaView as ComponentType<any>;

export function ScreenHeader({
  title,
  subtitle,
  backLabel,
  fallback,
  preferFallback,
  right,
}: {
  title: string;
  subtitle?: string;
  backLabel?: string;
  fallback?: string;
  preferFallback?: boolean;
  right?: ReactNode;
}) {
  const router = useRouter();

  return (
    <SafeAreaView edges={["top"]} style={styles.safeArea}>
      <View style={styles.container}>
        {backLabel ? (
          <TouchableOpacity
            style={styles.backButton}
            onPress={() => {
              if (preferFallback && fallback) {
                router.replace(fallback as never);
              } else if (router.canGoBack()) {
                router.back();
              } else if (fallback) {
                router.replace(fallback as never);
              }
            }}
            activeOpacity={0.7}
          >
            <Text style={styles.backText}>‹ {backLabel}</Text>
          </TouchableOpacity>
        ) : null}
        <View style={styles.titleBlock}>
          <Text style={styles.title} numberOfLines={1}>
            {title}
          </Text>
          {subtitle ? (
            <Text style={styles.subtitle} numberOfLines={1}>
              {subtitle}
            </Text>
          ) : null}
        </View>
        {right ? <View style={styles.right}>{right}</View> : null}
      </View>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  safeArea: {
    backgroundColor: colors.background,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border,
  },
  container: {
    minHeight: 56,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingHorizontal: 16,
    paddingBottom: 10,
  },
  backButton: {
    paddingVertical: 8,
    paddingRight: 4,
  },
  backText: {
    fontSize: 16,
    color: colors.foreground,
    fontWeight: "500",
  },
  titleBlock: {
    flex: 1,
  },
  title: {
    fontSize: 18,
    color: colors.foreground,
    fontWeight: "700",
  },
  subtitle: {
    marginTop: 2,
    fontSize: 12,
    color: colors.mutedForeground,
  },
  right: {
    marginLeft: "auto",
  },
});
