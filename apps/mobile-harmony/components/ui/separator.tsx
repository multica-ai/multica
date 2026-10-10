/**
 * HarmonyOS port of apps/mobile/components/ui/separator.tsx. The iOS
 * version wraps @rn-primitives/separator with NativeWind classes; the
 * primitives are unavailable on RNOH, so this is a plain View with the
 * same API (orientation / decorative). Values: bg-border, 1px thick,
 * full-length in the cross axis.
 */
import { StyleSheet, View, type ViewProps } from "react-native";
import { useThemeColors } from "@/lib/use-theme-colors";

interface Props extends ViewProps {
  orientation?: "horizontal" | "vertical";
  /** Present for API parity with the primitive; a plain View carries no
   *  semantics either way. */
  decorative?: boolean;
}

function Separator({ orientation = "horizontal", style, ...props }: Props) {
  const c = useThemeColors();
  return (
    <View
      style={[
        styles.base,
        orientation === "horizontal" ? styles.horizontal : styles.vertical,
        { backgroundColor: c.border },
        style,
      ]}
      {...props}
    />
  );
}

const styles = StyleSheet.create({
  base: { flexShrink: 0, flexGrow: 0 },
  horizontal: { height: 1, width: "100%" },
  vertical: { height: "100%", width: 1 },
});

export { Separator };
