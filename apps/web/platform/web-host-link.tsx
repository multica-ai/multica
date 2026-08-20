import Link from "next/link";
import type { ComponentProps } from "react";
import { isDocumentHandoffPath } from "./web-host-path";

type Props = ComponentProps<typeof Link>;

/** Keep Next links inside Next; cross into the mounted Tag host via HTTP. */
export function WebHostLink({ href, ...props }: Props) {
  if (typeof href === "string" && isDocumentHandoffPath(href)) {
    return <a href={href} {...props} />;
  }
  return <Link href={href} {...props} />;
}
