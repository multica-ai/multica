/**
 * App-icon lockup for first-impression surfaces (auth screens).
 *
 * Renders the actual Multica app icon (assets/icon.png — the same asset wired
 * as `icon` in app.config.ts), not a recreation, so the login is anchored to
 * the exact mark users see on their home screen. The source icon is an opaque
 * white square with the squircle inset; we clip to a rounded frame and scale
 * the image up slightly to crop that white margin, so it reads correctly on
 * both light and dark backgrounds.
 */
import { View } from "react-native";
import { Image } from "expo-image";

const ICON = require("../../assets/icon.png");

export function BrandMark({ size = 72 }: { size?: number }) {
  // Crop the icon's white margin so its squircle fills our rounded frame.
  const inner = size * 1.16;
  const offset = -(inner - size) / 2;
  return (
    <View
      style={{
        shadowColor: "#000",
        shadowOpacity: 0.18,
        shadowRadius: 14,
        shadowOffset: { width: 0, height: 6 },
      }}
    >
      <View
        style={{
          width: size,
          height: size,
          // iOS continuous-corner ratio (~0.2237 of the side) for the squircle.
          borderRadius: size * 0.2237,
          overflow: "hidden",
        }}
      >
        <Image
          source={ICON}
          style={{
            width: inner,
            height: inner,
            marginLeft: offset,
            marginTop: offset,
          }}
          contentFit="cover"
        />
      </View>
    </View>
  );
}
