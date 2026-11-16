import {
  createElement,
  forwardRef,
  type ForwardRefExoticComponent,
  type RefAttributes,
  type SVGProps,
} from "react";

export interface IconProps extends Partial<SVGProps<SVGSVGElement>> {
  size?: string | number;
}

export type IconComponent = ForwardRefExoticComponent<
  Omit<IconProps, "ref"> & RefAttributes<SVGSVGElement>
>;

/** A glyph's children on the 24×24 grid: `[tag, attributes]` pairs. */
export type IconNode = ReadonlyArray<readonly [string, Record<string, string | number>]>;

/** Stroke on the 24px grid; 1.5px at the 18px nav size, 1.33px at 16px. */
export const ICON_STROKE_WIDTH = 2;

function iconClassName(name: string, className?: string) {
  const slug = name
    .replace(/Icon$/, "")
    .replace(/([a-z0-9])([A-Z])/g, "$1-$2")
    .replace(/([A-Z])([A-Z][a-z])/g, "$1-$2")
    .toLowerCase();
  return className ? `icon icon-${slug} ${className}` : `icon icon-${slug}`;
}

function isLabelled(props: Record<string, unknown>) {
  return Object.keys(props).some((key) => key.startsWith("aria-") || key === "role" || key === "title");
}

export function createIcon(name: string, node: IconNode): IconComponent {
  const children = node.map(([tag, attrs], i) => createElement(tag, { key: i, ...attrs }));
  const Icon = forwardRef<SVGSVGElement, Omit<IconProps, "ref">>(
    ({ size = 24, strokeWidth = ICON_STROKE_WIDTH, className, children: extra, ...rest }, ref) =>
      createElement(
        "svg",
        {
          ref,
          xmlns: "http://www.w3.org/2000/svg",
          width: size,
          height: size,
          viewBox: "0 0 24 24",
          fill: "none",
          stroke: "currentColor",
          strokeWidth,
          strokeLinecap: "round",
          strokeLinejoin: "round",
          "aria-hidden": isLabelled(rest) ? undefined : true,
          className: iconClassName(name, className),
          ...rest,
        },
        ...children,
        extra,
      ),
  );
  Icon.displayName = name;
  return Icon;
}
