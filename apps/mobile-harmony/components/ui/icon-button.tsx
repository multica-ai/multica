/**
 * HarmonyOS port of apps/mobile/components/ui/icon-button.tsx — ghost
 * 40x40 `<Button size="icon">` wrapping a tintable icon. The iOS version
 * renders an Ionicon from @expo/vector-icons and falls back to the active
 * navigation theme's text color; here the glyph comes from the ported
 * `<Icon>` (inline Ionicons SVG paths) and the default color is the theme
 * `foreground`, so dark mode flips automatically without a color prop.
 *
 * Use everywhere we'd otherwise hand-write
 *   <Pressable style={{ width: 36, height: 36 }}><Icon ... /></Pressable>
 * — that pattern hardcodes light-mode hexes and reinvents button chrome.
 */
import { type ComponentProps } from "react";
import { Button, type ButtonProps } from "@/components/ui/button";
import { Icon } from "@/components/ui/icon";
import { useThemeColors } from "@/lib/use-theme-colors";

/** Ionicons glyph name, as accepted by <Icon>. */
type IconGlyph = ComponentProps<typeof Icon>["name"];

interface Props extends Omit<ButtonProps, "children" | "size"> {
  name: IconGlyph;
  /** Glyph size in points. Default 20 matches the iOS toolbar icons. */
  iconSize?: number;
  /** Override the icon color. Defaults to the theme foreground. */
  color?: string;
}

export function IconButton({
  name,
  iconSize = 20,
  color,
  ...buttonProps
}: Props) {
  const c = useThemeColors();
  return (
    <Button variant="ghost" size="icon" {...buttonProps}>
      <Icon name={name} size={iconSize} color={color ?? c.foreground} />
    </Button>
  );
}
