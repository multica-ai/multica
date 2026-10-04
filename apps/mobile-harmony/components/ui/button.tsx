/**
 * HarmonyOS port of apps/mobile/components/ui/button.tsx. The iOS version
 * composes NativeWind class variants (cva); NativeWind className styling
 * regressed on RNOH 0.82, so variant/size are plain union types resolved
 * through a cached style table keyed on the theme colors.
 *
 * Visual values are the tailwind defaults the classes compiled to:
 * rounded-md=6, gap-2=8, h-10=40 px-4=16, h-9=36 px-3=12 gap-1.5=6,
 * h-11=44 px-6=24, icon 40x40; disabled opacity-50. Active (pressed)
 * states reproduce `active:bg-*` / `active:bg-<color>/nn` — translucent shades
 * go through withAlpha() because THEME tokens are opaque hsl strings.
 * Web-only concerns (focus rings, hover) and the shadow decoration are
 * dropped — native keeps flat surfaces.
 *
 * Label colors flow to a nested <Text> (the ported one) through
 * TextClassContext, mirroring the iOS buttonTextVariants wiring.
 */
import React from "react";
import {
  Pressable,
  StyleSheet,
  View,
  type PressableProps,
  type PressableStateCallbackType,
  type StyleProp,
  type TextStyle,
  type ViewStyle,
} from "react-native";
import { TextClassContext } from "@/components/ui/text";
import { THEME, withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

export type ButtonVariant =
  | "default"
  | "destructive"
  | "outline"
  | "secondary"
  | "ghost"
  | "link";

export type ButtonSize = "default" | "sm" | "lg" | "icon";

interface ButtonStyleTable {
  base: Record<ButtonVariant, ViewStyle>;
  size: Record<ButtonSize, ViewStyle>;
  pressed: Record<ButtonVariant, ViewStyle>;
  label: Record<ButtonVariant, TextStyle>;
  disabled: ViewStyle;
}

function buildButtonStyles(c: ReturnType<typeof useThemeColors>): ButtonStyleTable {
  // The iOS outline variant uses dark:bg-input/30 in dark mode; the colors
  // object is one of the two THEME constants, so the scheme is derivable.
  const isDark = c === THEME.dark;
  return {
    base: StyleSheet.create({
      default: {
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "center",
        gap: 8,
        borderRadius: 6,
        flexShrink: 0,
        backgroundColor: c.primary,
      },
      destructive: {
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "center",
        gap: 8,
        borderRadius: 6,
        flexShrink: 0,
        backgroundColor: c.destructive,
      },
      outline: {
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "center",
        gap: 8,
        borderRadius: 6,
        flexShrink: 0,
        backgroundColor: isDark ? withAlpha(c.input, 0.3) : c.background,
        borderWidth: 1,
        borderColor: c.border,
      },
      secondary: {
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "center",
        gap: 8,
        borderRadius: 6,
        flexShrink: 0,
        backgroundColor: c.secondary,
      },
      ghost: {
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "center",
        gap: 8,
        borderRadius: 6,
        flexShrink: 0,
        backgroundColor: "transparent",
      },
      link: {
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "center",
        gap: 8,
        borderRadius: 6,
        flexShrink: 0,
        backgroundColor: "transparent",
      },
    }),
    size: StyleSheet.create({
      default: { height: 40, paddingHorizontal: 16 },
      sm: { height: 36, paddingHorizontal: 12, gap: 6 },
      lg: { height: 44, paddingHorizontal: 24 },
      icon: { height: 40, width: 40 },
    }),
    pressed: StyleSheet.create({
      default: { backgroundColor: withAlpha(c.primary, 0.9) },
      destructive: { backgroundColor: withAlpha(c.destructive, 0.9) },
      outline: { backgroundColor: c.accent },
      secondary: { backgroundColor: withAlpha(c.secondary, 0.8) },
      ghost: { backgroundColor: c.accent },
      link: { backgroundColor: "transparent" },
    }),
    label: StyleSheet.create({
      default: { color: c.primaryForeground },
      destructive: { color: "#ffffff" },
      outline: { color: c.accentForeground },
      secondary: { color: c.secondaryForeground },
      ghost: { color: c.accentForeground },
      link: { color: c.primary },
    }),
    // Built once per theme and cached, so a plain object is fine here.
    disabled: { opacity: 0.5 },
  };
}

const styleCache = new WeakMap<object, ButtonStyleTable>();

function useButtonStyles(): ButtonStyleTable {
  const c = useThemeColors();
  let table = styleCache.get(c);
  if (!table) {
    table = buildButtonStyles(c);
    styleCache.set(c, table);
  }
  return table;
}

type ButtonProps = Omit<PressableProps, "children"> &
  React.RefAttributes<View> & {
    variant?: ButtonVariant;
    size?: ButtonSize;
    children?: React.ReactNode;
  };

function Button({
  variant = "default",
  size = "default",
  disabled,
  style,
  children,
  ...props
}: ButtonProps) {
  const s = useButtonStyles();
  // RNOH's Pressable drops function-form `style` (the pressed-state callback
  // never resolves — the button renders with NO background at all), so the
  // pressed state is tracked manually and applied as a static array.
  const [pressed, setPressed] = React.useState(false);
  const pressStyle: StyleProp<ViewStyle> = [
    s.base[variant],
    s.size[size],
    typeof style === "function" ? undefined : style,
    pressed && !disabled ? s.pressed[variant] : null,
    disabled ? s.disabled : null,
  ];
  return (
    <TextClassContext.Provider value={s.label[variant]}>
      <Pressable
        role="button"
        disabled={disabled}
        style={pressStyle}
        onPressIn={() => setPressed(true)}
        onPressOut={() => setPressed(false)}
        {...props}
      >
        {children}
      </Pressable>
    </TextClassContext.Provider>
  );
}

export { Button };
export type { ButtonProps };
