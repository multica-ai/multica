/**
 * HarmonyOS port of apps/mobile/components/ui/text.tsx. The iOS version
 * styles through NativeWind classes + class-variance-authority and swaps
 * the rendered node via @rn-primitives/slot (asChild); NativeWind className
 * styling regressed on RNOH 0.82 and the slot primitive is unavailable, so
 * variants are a plain union type resolved through a style lookup table and
 * asChild is dropped (callers compose their own wrappers).
 *
 * TextClassContext keeps its iOS name but now carries a TextStyle override
 * instead of a className string: ancestors like Button / DropdownMenuItem
 * provide it so a nested <Text> picks up the right label color without
 * explicit props, exactly like the class-based flow it replaces.
 *
 * Colors come from useThemeColors() so dark mode flips automatically; the
 * per-theme table is cached in a WeakMap keyed on the colors object (one of
 * the two THEME constants), keeping style references stable across renders.
 */
import React, { useContext } from "react";
import {
  StyleSheet,
  Text as RNText,
  type Role,
  type StyleProp,
  type TextStyle,
} from "react-native";
import { useThemeColors } from "@/lib/use-theme-colors";

export type TextVariant =
  | "default"
  | "h1"
  | "h2"
  | "h3"
  | "h4"
  | "p"
  | "blockquote"
  | "code"
  | "lead"
  | "large"
  | "small"
  | "muted";

// Tailwind scale translations of the iOS variant classes:
// text-4xl=36 text-3xl=30 text-2xl=24 text-xl=20 text-lg=18 text-base=16
// text-sm=14 text-xs=12; font-semibold=600 font-medium=500 font-extrabold=800;
// tracking-tight=-0.025em (converted per fontSize); mt-3=12 leading-7=28;
// mt-4=16 border-l-2 pl-3; rounded-xs=2 px-[0.3rem]=4.8 py-[0.2rem]=3.2.
function buildVariantStyles(c: ReturnType<typeof useThemeColors>) {
  return StyleSheet.create({
    default: { fontSize: 16, color: c.foreground },
    h1: {
      fontSize: 36,
      fontWeight: "800",
      letterSpacing: -0.9,
      textAlign: "center",
      color: c.foreground,
    },
    h2: {
      fontSize: 30,
      fontWeight: "600",
      letterSpacing: -0.75,
      borderBottomWidth: 1,
      borderBottomColor: c.border,
      paddingBottom: 8,
      color: c.foreground,
    },
    h3: {
      fontSize: 24,
      fontWeight: "600",
      letterSpacing: -0.6,
      color: c.foreground,
    },
    h4: {
      fontSize: 20,
      fontWeight: "600",
      letterSpacing: -0.5,
      color: c.foreground,
    },
    p: { marginTop: 12, lineHeight: 28, color: c.foreground },
    blockquote: {
      marginTop: 16,
      borderLeftWidth: 2,
      borderLeftColor: c.border,
      paddingLeft: 12,
      fontStyle: "italic",
      color: c.foreground,
    },
    code: {
      backgroundColor: c.muted,
      borderRadius: 2,
      paddingHorizontal: 4.8,
      paddingVertical: 3.2,
      // Android/RNOH map "monospace"; other platforms fall back to the system
      // font with a warning — acceptable degradation for a code variant.
      fontFamily: "monospace",
      fontSize: 14,
      fontWeight: "600",
      color: c.foreground,
    },
    lead: { fontSize: 20, color: c.mutedForeground },
    large: { fontSize: 18, fontWeight: "600", color: c.foreground },
    small: {
      fontSize: 14,
      fontWeight: "500",
      lineHeight: 14,
      color: c.foreground,
    },
    muted: { fontSize: 14, color: c.mutedForeground },
  });
}

type VariantStyles = ReturnType<typeof buildVariantStyles>;

const variantStyleCache = new WeakMap<object, VariantStyles>();

function useVariantStyles(): VariantStyles {
  const c = useThemeColors();
  let table = variantStyleCache.get(c);
  if (!table) {
    table = buildVariantStyles(c);
    variantStyleCache.set(c, table);
  }
  return table;
}

// Native heading roles mirror the iOS ROLE map (the blockquote/code roles
// are web-only there, and RN Text has no aria-level, so both stay out).
const ROLE: Partial<Record<TextVariant, Role>> = {
  h1: "heading",
  h2: "heading",
  h3: "heading",
  h4: "heading",
};

/**
 * TextStyle override provided by button/menu ancestors for their labels.
 * iOS carries a className string here; on this side it is a plain style.
 */
export const TextClassContext =
  React.createContext<StyleProp<TextStyle> | undefined>(undefined);

function Text({
  style,
  variant = "default",
  ref,
  ...props
}: Omit<React.ComponentProps<typeof RNText>, "ref"> & {
  ref?: React.Ref<RNText>;
  variant?: TextVariant;
}) {
  const styles = useVariantStyles();
  const textClass = useContext(TextClassContext);
  return (
    <RNText
      ref={ref}
      style={[styles[variant], textClass, style]}
      role={variant ? ROLE[variant] : undefined}
      {...props}
    />
  );
}

export { Text };
