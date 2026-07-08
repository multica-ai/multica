/**
 * Icon-only button — RNR `<Button variant="ghost" size="icon">` wrapping an
 * Ionicon. The icon color falls back to the active navigation theme's
 * foreground (via `useTheme()`), so dark mode flips automatically without
 * anyone passing a color prop.
 *
 * Use everywhere we'd otherwise hand-write
 *   <Pressable className="size-9 active:bg-secondary"><Ionicons color="#3f3f46" /></Pressable>
 * — that pattern hardcodes a light-mode hex and reinvents button chrome RNR
 * already ships.
 */
import { type ComponentProps } from "react";
import { Ionicons } from "@expo/vector-icons";
import { useTheme } from "@react-navigation/native";
import { Button, type ButtonProps } from "@/components/ui/button";

interface Props extends Omit<ButtonProps, "children" | "size"> {
  name: ComponentProps<typeof Ionicons>["name"];
  /** Glyph size in points. Default 20 matches iOS toolbar icons. */
  iconSize?: number;
  /** Override the icon color. Defaults to NAV_THEME[scheme].text. */
  color?: string;
}

export function IconButton({
  name,
  iconSize = 20,
  color,
  ...buttonProps
}: Props) {
  const { colors } = useTheme();
  return (
    <Button variant="ghost" size="icon" {...buttonProps}>
      <Ionicons name={name} size={iconSize} color={color ?? colors.text} />
    </Button>
  );
}
