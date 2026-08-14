import type { SVGProps } from "react";

export function QianwenMark(props: SVGProps<SVGSVGElement>) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      {...props}
    >
      <path d="M3.5 12c0-4.7 3.8-8.5 8.5-8.5s8.5 3.8 8.5 8.5-3.8 8.5-8.5 8.5S3.5 16.7 3.5 12Z" />
      <path d="m15.8 15.8 4 4" />
      <path d="M7.8 10.2c1.1-1.4 2.5-2.1 4.2-2.1s3.1.7 4.2 2.1" />
    </svg>
  );
}
