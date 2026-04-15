import { View, Text, Image, StyleSheet } from "react-native";
import { colors } from "@/lib/theme";

export function Avatar({
  name,
  avatarUrl,
  size = 24,
  isAgent = false,
}: {
  name?: string | null;
  avatarUrl?: string | null;
  size?: number;
  isAgent?: boolean;
}) {
  const initials = name
    ? name
        .split(" ")
        .map((w) => w[0])
        .join("")
        .toUpperCase()
        .slice(0, 2)
    : "?";

  if (avatarUrl) {
    return (
      <Image
        source={{ uri: avatarUrl }}
        style={[
          styles.image,
          {
            width: size,
            height: size,
            borderRadius: isAgent ? 6 : size / 2,
          },
        ]}
      />
    );
  }

  return (
    <View
      style={[
        styles.fallback,
        {
          width: size,
          height: size,
          borderRadius: isAgent ? 6 : size / 2,
          backgroundColor: isAgent ? "#7C3AED" : colors.muted,
        },
      ]}
    >
      <Text
        style={[
          styles.initials,
          { fontSize: size * 0.4, color: isAgent ? "#FFFFFF" : colors.mutedForeground },
        ]}
      >
        {isAgent ? "🤖" : initials}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  image: {
    resizeMode: "cover",
  },
  fallback: {
    alignItems: "center",
    justifyContent: "center",
  },
  initials: {
    fontWeight: "600",
  },
});
