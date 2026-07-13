/**
 * Multica wordmark / sigil. 1:1 vector copy of docs/assets/logo-light.svg —
 * keep this file and the SVG in sync.
 *
 * react-native-svg does not resolve CSS `currentColor`, so callers must pass
 * `color` explicitly. For theme-aware usage, pair with `useColorScheme` +
 * `THEME` token from `@/lib/theme`.
 */
import Svg, { Polygon } from "react-native-svg";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";

interface MulticaLogoProps {
  size?: number;
  color?: string;
}

export function MulticaLogo({ size = 48, color }: MulticaLogoProps) {
  const { isDarkColorScheme } = useColorScheme();
  const resolvedColor =
    color ?? (isDarkColorScheme ? THEME.dark.foreground : THEME.light.foreground);

  return (
    <Svg width={size} height={size} viewBox="0 0 80 80">
      <Polygon
        fill={resolvedColor}
        points="35,51.1 35,80 45,80 45,51.1 71.8,77.9 78.9,70.8 52.1,44 90,44 90,34 52.1,34 78.9,7.2 71.8,0.1 45,26.9 45,-11 35,-11 35,26.9 8.2,0.1 1.1,7.2 27.9,34 -10,34 -10,44 27.9,44 1.1,70.8 8.2,77.9"
        transform="translate(5, 5.5) scale(0.87)"
      />
    </Svg>
  );
}
