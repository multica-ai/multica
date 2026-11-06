/**
 * Multica wordmark. Vector copy of WORDMARK in packages/ui/lib/multica-brand.ts
 * (mobile does not depend on @multica/ui) — keep the two in sync.
 *
 * react-native-svg does not resolve CSS `currentColor`, so callers must pass
 * `color` explicitly. For theme-aware usage, pair with `useColorScheme` +
 * `THEME` token from `@/lib/theme`.
 */
import Svg, { Circle, G, Path } from "react-native-svg";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";

const VIEW_BOX = { x: 0, y: -30.5, w: 421.36, h: 106.5 };
const LETTERS = [
  "M8 76V31A23 23 0 0 1 54 31V76M54 31A23 23 0 0 1 100 31V76",
  "M128 0V45A23 23 0 0 0 174 45M174 0V76",
  "M202 -30V76",
  "M239.6 -19.8V76M219.6 8H259.6",
  "M277.8 0V76",
  "M344.103 14.737A23 23 0 0 0 304.84 31V45A23 23 0 0 0 344.103 61.263",
  "M367.36 31A23 23 0 0 1 413.36 31V45A23 23 0 0 1 367.36 45ZM413.36 0V76",
];

interface MulticaLogoProps {
  /** Rendered height in points; width follows the wordmark's aspect ratio. */
  size?: number;
  color?: string;
}

export function MulticaLogo({ size = 24, color }: MulticaLogoProps) {
  const { isDarkColorScheme } = useColorScheme();
  const resolvedColor =
    color ?? (isDarkColorScheme ? THEME.dark.foreground : THEME.light.foreground);

  return (
    <Svg
      width={(size * VIEW_BOX.w) / VIEW_BOX.h}
      height={size}
      viewBox={`${VIEW_BOX.x} ${VIEW_BOX.y} ${VIEW_BOX.w} ${VIEW_BOX.h}`}
      accessibilityLabel="Multica"
    >
      <G fill="none" stroke={resolvedColor} strokeWidth={16}>
        {LETTERS.map((d) => (
          <Path key={d} d={d} />
        ))}
      </G>
      <Circle cx={277.8} cy={-18.6} r={8.96} fill={resolvedColor} />
    </Svg>
  );
}
