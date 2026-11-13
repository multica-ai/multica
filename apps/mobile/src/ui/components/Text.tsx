import { Text, type TextProps } from "react-native";
import { type, useTheme, type Palette } from "../theme";

type Tone = "primary" | "secondary" | "tertiary" | "accent" | "add" | "del" | "warning" | "destructive" | "onAccent";

const TONES: Record<Tone, keyof Palette> = {
  primary: "label",
  secondary: "label2",
  tertiary: "label3",
  accent: "accent",
  add: "add",
  del: "del",
  warning: "warning",
  destructive: "destructive",
  onAccent: "onAccent",
};

interface TxtProps extends TextProps {
  variant?: keyof typeof type;
  tone?: Tone;
}

export function Txt({ variant = "body", tone = "primary", style, ...rest }: TxtProps) {
  const palette = useTheme();
  return <Text {...rest} style={[type[variant], { color: palette[TONES[tone]] as string }, style]} />;
}
