import type { JSX, SVGProps } from "react";
import { observer } from "mobx-react-lite";
export const GridTopLinedIcon = observer(GridTopLinedIconRender);

function GridTopLinedIconRender(props: SVGProps<SVGSVGElement>): JSX.Element {
  "use memo";
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden
      data-tutti-chrome-glyph="fill"
      data-nexight-chrome-glyph="fill"
      {...props}
    >
      <path d="M19 2C20.6569 2 22 3.34315 22 5V19C22 20.6569 20.6569 22 19 22H5C3.34315 22 2 20.6569 2 19V5C2 3.34315 3.34315 2 5 2H19ZM4 10V19C4 19.5523 4.44772 20 5 20H19C19.5523 20 20 19.5523 20 19V10H4ZM5 4C4.44772 4 4 4.44772 4 5V8H20V5C20 4.44772 19.5523 4 19 4H5Z" />
    </svg>
  );
}
