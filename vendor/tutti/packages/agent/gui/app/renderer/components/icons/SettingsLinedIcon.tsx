import type { JSX, SVGProps } from "react";
import { observer } from "mobx-react-lite";
export const SettingsLinedIcon = observer(SettingsLinedIconRender);

function SettingsLinedIconRender(props: SVGProps<SVGSVGElement>): JSX.Element {
  "use memo";
  return (
    <svg
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M12 8C9.79088 8 8.00002 9.79086 8.00002 12C8.00002 14.2091 9.79088 16 12 16C14.2092 16 16 14.2091 16 12C16 9.79086 14.2092 8 12 8ZM10 12C10 10.8954 10.8955 10 12 10C13.1046 10 14 10.8954 14 12C14 13.1046 13.1046 14 12 14C10.8955 14 10 13.1046 10 12Z"
        fill="currentColor"
      />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M12.5147 2.08484C12.1951 1.90505 11.8048 1.90505 11.4852 2.08484L3.48518 6.58484C3.15456 6.77082 2.94995 7.12066 2.94995 7.5V16.5C2.94995 16.8793 3.15456 17.2292 3.48518 17.4152L11.4852 21.9152C11.8048 22.0949 12.1951 22.0949 12.5147 21.9152L20.5147 17.4152C20.8453 17.2292 21.0499 16.8793 21.0499 16.5V7.5C21.0499 7.12066 20.8453 6.77082 20.5147 6.58484L12.5147 2.08484ZM5.04995 15.8859V8.11409L12 4.20471L18.95 8.11409V15.8859L12 19.7953L5.04995 15.8859Z"
        fill="currentColor"
      />
    </svg>
  );
}
