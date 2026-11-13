import { createContext, useContext } from "react";
import { useColorScheme, type TextStyle } from "react-native";
import { dark, light, type Palette } from "./colors";

export type { Palette } from "./colors";

/** Lets previews pin a scheme; the app follows the system setting. */
export const SchemeOverride = createContext<"light" | "dark" | null>(null);

export function useTheme(): Palette {
  const override = useContext(SchemeOverride);
  const system = useColorScheme();
  return (override ?? system) === "dark" ? dark : light;
}

export const space = { screen: 20, gap: 12 } as const;

export const radius = { chip: 15, group: 18, card: 16, float: 22, sheet: 28 } as const;

/** Type ramp of the design language: compact titles, 16pt rows and body. */
export const type = {
  title: { fontSize: 24, lineHeight: 30, fontWeight: "700", letterSpacing: -0.36 },
  heading: { fontSize: 23, lineHeight: 29, fontWeight: "600", letterSpacing: -0.35 },
  navTitle: { fontSize: 16, lineHeight: 20, fontWeight: "600" },
  row: { fontSize: 16, lineHeight: 21, fontWeight: "500" },
  rowRegular: { fontSize: 16, lineHeight: 21, fontWeight: "400" },
  body: { fontSize: 16, lineHeight: 24, fontWeight: "400" },
  bodyStrong: { fontSize: 16, lineHeight: 20, fontWeight: "600" },
  callout: { fontSize: 15, lineHeight: 20, fontWeight: "400" },
  calloutStrong: { fontSize: 15, lineHeight: 20, fontWeight: "600" },
  meta: { fontSize: 14, lineHeight: 19, fontWeight: "400" },
  metaStrong: { fontSize: 14, lineHeight: 18, fontWeight: "600" },
  chip: { fontSize: 14, lineHeight: 18, fontWeight: "500" },
  caption: { fontSize: 12, lineHeight: 16, fontWeight: "500" },
  mono: { fontSize: 14, lineHeight: 20, fontFamily: "Menlo" },
} as const satisfies Record<string, TextStyle>;

export function floatShadow(palette: Palette) {
  return {
    shadowColor: palette.shadow,
    shadowOpacity: palette.shadowOpacity,
    shadowRadius: 12,
    shadowOffset: { width: 0, height: 6 },
    elevation: 6,
  } as const;
}
