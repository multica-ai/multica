/**
 * Mobile ProjectIcon — RN port of
 * `packages/views/projects/components/project-icon.tsx`. Renders the
 * project's emoji icon with a 📁 fallback.
 *
 * Why a square View wrapper + explicit lineHeight:
 *   Emoji glyphs do not respect the latin-text baseline metrics — on iOS
 *   they render with a visual extent ~10-15% larger than `fontSize`,
 *   centered on the baseline. `<Text>` clips content to `lineHeight`, so a
 *   `lineHeight: font-size` (the previous `leading-none` form) trimmed the
 *   top and bottom of the emoji. Setting `lineHeight = fontSize * 1.2`
 *   gives the glyph enough vertical room; the surrounding View pins a
 *   stable square footprint so flex parents using `items-center` /
 *   `items-start` align siblings against a predictable box instead of the
 *   emoji's drifting baseline.
 */
import { View } from "react-native";
import { Text } from "@/components/ui/text";

export type ProjectIconSize = "sm" | "md" | "lg";

const SIZE: Record<ProjectIconSize, { box: number; font: number }> = {
  sm: { box: 18, font: 14 },
  md: { box: 22, font: 16 },
  lg: { box: 28, font: 22 },
};

interface Props {
  icon?: string | null;
  size?: ProjectIconSize;
}

export function ProjectIcon({ icon, size = "sm" }: Props) {
  const { box, font } = SIZE[size];
  return (
    <View
      style={{
        width: box,
        height: box,
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      <Text style={{ fontSize: font, lineHeight: Math.round(font * 1.2) }}>
        {icon || "📁"}
      </Text>
    </View>
  );
}
