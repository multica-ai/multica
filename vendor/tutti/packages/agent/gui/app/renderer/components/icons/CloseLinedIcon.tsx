import type { JSX, SVGProps } from "react";
import { observer } from "mobx-react-lite";
export const CloseLinedIcon = observer(CloseLinedIconRender);

/**
 * Lined dismiss (24x24 viewBox). Fill uses `currentColor` for panel / chrome close buttons.
 * Source asset: `assets/icons/close-lined.svg`.
 */
function CloseLinedIconRender(props: SVGProps<SVGSVGElement>): JSX.Element {
  "use memo";
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden
      {...props}
    >
      <path d="M18.2925 4.29252C18.6829 3.90215 19.316 3.90245 19.7066 4.29252C20.0971 4.68305 20.0971 5.31606 19.7066 5.70659L13.4136 11.9996L19.7066 18.2925C20.0971 18.683 20.0971 19.3161 19.7066 19.7066C19.3161 20.0971 18.6831 20.0971 18.2925 19.7066L11.9996 13.4136L5.70659 19.7066C5.31607 20.0971 4.68305 20.0971 4.29253 19.7066C3.90246 19.316 3.90216 18.6829 4.29253 18.2925L10.5855 11.9996L4.29253 5.70659C3.90246 5.31603 3.90216 4.6829 4.29253 4.29252C4.68291 3.90215 5.31603 3.90245 5.70659 4.29252L11.9996 10.5855L18.2925 4.29252Z" />
    </svg>
  );
}
