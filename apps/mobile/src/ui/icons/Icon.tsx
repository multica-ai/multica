import Svg, { Circle, Path } from "react-native-svg";
import type { GlyphNode } from "./glyph-node";
import { glyphs } from "./glyphs";

export type IconName = keyof typeof glyphs;

interface IconProps {
  name: IconName;
  size?: number;
  color: string;
  strokeWidth?: number;
}

function paint(value: string | number | undefined, color: string) {
  return value === "currentColor" ? color : value === undefined ? undefined : String(value);
}

export function Icon({ name, size = 20, color, strokeWidth = 2 }: IconProps) {
  const node: GlyphNode = glyphs[name];
  return (
    <Svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth={strokeWidth} strokeLinecap="round" strokeLinejoin="round">
      {node.map(([tag, attrs], i) => {
        const fill = paint(attrs.fill, color);
        const stroke = paint(attrs.stroke, color);
        if (tag === "circle") {
          return <Circle key={i} cx={attrs.cx} cy={attrs.cy} r={attrs.r} fill={fill} stroke={stroke} />;
        }
        return <Path key={i} d={String(attrs.d)} fill={fill} stroke={stroke} strokeOpacity={attrs.strokeOpacity} />;
      })}
    </Svg>
  );
}
