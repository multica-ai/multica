import type { ComponentProps } from "react";
import { Platform, type ColorValue } from "react-native";
import { Image } from "expo-image";
import { Ionicons } from "@expo/vector-icons";

export interface PlatformIconProps {
  ios: string;
  android: ComponentProps<typeof Ionicons>["name"];
  size: number;
  color: ColorValue;
}

/** SF Symbols are available only on Apple platforms. */
export function PlatformIcon({ ios, android, size, color }: PlatformIconProps) {
  return Platform.OS === "ios" ? (
    <Image
      source={`sf:${ios}`}
      style={{ width: size, height: size, color }}
      accessible={false}
    />
  ) : (
    <Ionicons name={android} size={size} color={color} accessible={false} />
  );
}
