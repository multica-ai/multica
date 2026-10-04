/**
 * Tintable Ionicons glyph rendered as font text — the react-native-vector-
 * icons approach. The iOS app draws the same glyphs through
 * @expo/vector-icons; here the Ionicons TTF is bundled as a rawfile and
 * registered for the "Ionicons" font family in Index.ets
 * (fontResourceByFontFamily). `name` matches the Ionicons icon name;
 * unknown names render nothing (visible as a gap during development).
 */
import React, { memo } from "react";
import { StyleSheet, Text } from "react-native";
import { IONICON_GLYPHS } from "@/components/ui/ionicon-glyphs";

export const ICON_FONT_FAMILY = "Ionicons";

export const Icon = memo(function Icon({
  name,
  size = 24,
  color,
}: {
  name: keyof typeof IONICON_GLYPHS | (string & {});
  size?: number;
  color: string;
}) {
  const codePoint = IONICON_GLYPHS[name];
  if (!codePoint) return null;
  return (
    <Text
      style={[
        styles.glyph,
        {
          color,
          fontSize: size,
          // Match the SVG-Icon box contract: fixed square, glyph centered.
          width: size,
          height: size,
          lineHeight: size,
          textAlign: "center",
        },
      ]}
      // The glyph codepoint is the only content; never let a screen-reader
      // announce a private-use codepoint.
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
    >
      {String.fromCodePoint(codePoint)}
    </Text>
  );
});

const styles = StyleSheet.create({
  glyph: {
    fontFamily: ICON_FONT_FAMILY,
    fontWeight: "400",
    textAlignVertical: "center",
  },
});
