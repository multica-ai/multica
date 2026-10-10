import type { ComponentProps } from "react";
import { Image } from "expo-image";
import { MaterialCommunityIcons } from "@expo/vector-icons";
import { Platform } from "react-native";

type IconName = ComponentProps<typeof MaterialCommunityIcons>["name"];

/** Keep the iOS SF Symbol while rendering a bundled vector glyph elsewhere. */
export function PlatformIcon({
  iosSymbol,
  fallback,
  color,
  size,
}: {
  iosSymbol: string;
  fallback: IconName;
  color: string;
  size: number;
}) {
  if (Platform.OS === "ios") {
    return (
      <Image
        source={`sf:${iosSymbol}`}
        tintColor={color}
        style={{ width: size, height: size }}
      />
    );
  }

  return <MaterialCommunityIcons name={fallback} color={color} size={size} />;
}
